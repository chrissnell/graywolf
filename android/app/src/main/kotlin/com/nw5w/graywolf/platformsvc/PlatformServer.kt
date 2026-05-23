package com.nw5w.graywolf.platformsvc

import android.net.LocalServerSocket
import android.net.LocalSocket
import android.util.Log
import com.nw5w.graywolf.platformproto.GnssStatusUpdate
import com.nw5w.graywolf.platformproto.GpsFix
import com.nw5w.graywolf.platformproto.PlatformMessage
import com.nw5w.graywolf.platformproto.SerialKind
import com.nw5w.graywolf.platformproto.UsbClass
import com.nw5w.graywolf.platformproto.UsbDevice as ProtoUsbDevice
import com.nw5w.graywolf.platformproto.UsbDeviceListResponse
import java.io.Closeable
import java.io.File
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import java.util.concurrent.CopyOnWriteArrayList
import kotlin.concurrent.thread

/**
 * UDS server consumed by the Go `pkg/platformsvc` client. One client at a
 * time. Phase 2 ships only Hello + GpsFix handlers; every other message
 * type returns Error{NOT_IMPLEMENTED}.
 *
 * Lifecycle: created in GraywolfService.onCreate after JNI loadLibrary
 * and before exec'ing the Go child. Stopped in onDestroy.
 *
 * Uses android.net.LocalServerSocket (API 1) with NAMESPACE_FILESYSTEM
 * so the Go side dials the path with net.Dialer{} "unix" unchanged.
 */
/**
 * Thrown by PlatformServer.start() when the abstract-namespace address is
 * still held by a previous instance after the bounded wait. The caller
 * (GraywolfService.onCreate) treats this as "another instance owns the
 * station" and stops itself cleanly rather than crashing the process.
 */
class BindContendedException(name: String, cause: Throwable) :
    java.io.IOException("platformsvc address still in use after wait: $name", cause)

class PlatformServer(
    private val socketPath: String,
    private val serverVersion: String,
    private val schemaVersion: Int,
) {
    private val gpsFixSubs = CopyOnWriteArrayList<(GpsFix) -> Unit>()
    private var prodListener: LocalServerSocket? = null
    private var testListener: java.nio.channels.ServerSocketChannel? = null
    private var acceptThread: Thread? = null
    @Volatile private var running = false

    @Volatile private var activeOutputs: List<OutputStream> = emptyList()
    private val outputsLock = Object()

    @Volatile private var btSerialAdapter: BtSerialAdapter? = null

    /**
     * Attach the BtSerialAdapter after construction. Wired this way because
     * BtSerialAdapter's sendMessage callback references broadcastBt on this
     * server, so the server must exist before the adapter is built. Safe
     * to call at most once during onCreate; serveClient consults the field
     * with a nullable read.
     */
    fun attachBtAdapter(adapter: BtSerialAdapter) {
        btSerialAdapter = adapter
    }

    @Volatile private var usbSerialAdapter: UsbSerialAdapter? = null

    /**
     * Attach the UsbSerialAdapter after construction (same lifecycle reason as
     * attachBtAdapter — its sendMessage callback closes over broadcastBt).
     * Safe to call at most once during onCreate.
     */
    fun attachUsbSerialAdapter(adapter: UsbSerialAdapter) {
        usbSerialAdapter = adapter
    }

    @Volatile private var usbDeviceLister: ((UsbClass) -> List<ProtoUsbDevice>)? = null

    /**
     * Attach the USB enumeration provider. The lambda receives the
     * class_filter from each UsbDeviceListRequest and must return the
     * matching list of ProtoUsbDevice. Wired this way (vs. holding an
     * Android Context here) so PlatformServer stays Android-injection-free
     * and unit-testable. Safe to call at most once during onCreate.
     */
    fun attachUsbDeviceLister(lister: (UsbClass) -> List<ProtoUsbDevice>) {
        usbDeviceLister = lister
    }

    fun subscribeGpsFix(cb: (GpsFix) -> Unit) {
        gpsFixSubs.add(cb)
    }

    /**
     * Push a server-produced PlatformMessage to every connected client.
     * Synchronizes per-stream: serveClient's response writes also wrap
     * WireCodec.writeFrame in synchronized(out) so a concurrent
     * broadcast can't interleave bytes with a response frame.
     */
    private fun broadcast(msg: PlatformMessage) {
        val outs = activeOutputs  // snapshot — CoW List, safe to iterate
        for (os in outs) {
            try {
                synchronized(os) { WireCodec.writeFrame(os, msg) }
            } catch (_: IOException) {
                // serveClient will remove the dead stream on its next read failure.
            }
        }
    }

    fun broadcastGpsFix(fix: GpsFix) =
        broadcast(PlatformMessage.newBuilder().setGpsFix(fix).build())

    fun broadcastGnssStatus(status: GnssStatusUpdate) =
        broadcast(PlatformMessage.newBuilder().setGnssStatus(status).build())

    /**
     * Typed wrapper used by BtSerialAdapter to push asynchronous frames
     * (SerialOpenAck, SerialData, SerialError, SerialClose,
     * BondedBtDevicesResponse) to the connected Go client. The adapter
     * already builds a fully-formed PlatformMessage, so this just forwards
     * to broadcast. Kept distinct from the raw private broadcast() to
     * preserve typed-intent at every call site.
     */
    fun broadcastBt(msg: PlatformMessage) = broadcast(msg)

    /**
     * Production startup. Binds an android.net.LocalServerSocket at
     * NAMESPACE_FILESYSTEM. Available on every Android API level the app
     * supports.
     */
    fun start() {
        // The abstract-namespace address frees the instant the holding
        // process dies, so "Address already in use" means a previous
        // instance is still tearing down. Wait for it briefly rather than
        // crashing -- a single bind attempt here is what produced the
        // relaunch->crash->relaunch loop that churned the USB bus.
        prodListener = bindWithRetry()
        running = true
        acceptThread = thread(start = true, isDaemon = true, name = "platformsvc-accept") {
            prodAcceptLoop()
        }
        Log.i(TAG, "PlatformServer bound at $socketPath")
    }

    // Bind, retrying on "Address already in use" until the predecessor frees
    // the abstract address or BIND_WAIT_MS elapses. Throws BindContendedException
    // on timeout so the caller can stopSelf() cleanly instead of crashing.
    private fun bindWithRetry(): LocalServerSocket {
        val deadline = System.currentTimeMillis() + BIND_WAIT_MS
        var attempt = 0
        while (true) {
            try {
                return LocalServerSocket(socketPath)
            } catch (e: IOException) {
                if (System.currentTimeMillis() >= deadline) {
                    throw BindContendedException(socketPath, e)
                }
                if (attempt == 0) {
                    Log.w(TAG, "platformsvc address busy; waiting for previous instance to exit")
                }
                attempt++
                try {
                    Thread.sleep(BIND_RETRY_STEP_MS)
                } catch (_: InterruptedException) {
                    Thread.currentThread().interrupt()
                    throw BindContendedException(socketPath, e)
                }
            }
        }
    }

    /**
     * Host-side unit-test startup. Uses java.net UDS APIs (JDK 16+) so
     * the JUnit harness on JDK 17 can drive a real socket without an
     * Android emulator. NEVER call from production code.
     *
     * Reflection is used so this file compiles against android.jar (API 28
     * stubs), which lacks java.net.UnixDomainSocketAddress and the
     * ServerSocketChannel.open(ProtocolFamily) overload. At runtime under
     * the unit-test JVM (JDK 17) the real classes are available.
     */
    fun startForTest() {
        File(socketPath).delete()

        val protocolFamilyClass = Class.forName("java.net.StandardProtocolFamily")
        val unixFamily = protocolFamilyClass
            .getField("UNIX")
            .get(null)
        val openMethod = java.nio.channels.ServerSocketChannel::class.java
            .getMethod("open", Class.forName("java.net.ProtocolFamily"))
        val ssc = openMethod.invoke(null, unixFamily) as java.nio.channels.ServerSocketChannel

        val udsAddrClass = Class.forName("java.net.UnixDomainSocketAddress")
        val ofMethod = udsAddrClass.getMethod("of", String::class.java)
        val addr = ofMethod.invoke(null, socketPath) as java.net.SocketAddress
        ssc.bind(addr)

        testListener = ssc
        running = true
        acceptThread = thread(start = true, isDaemon = true, name = "platformsvc-accept-test") {
            testAcceptLoop()
        }
    }

    fun stop() {
        running = false
        try { prodListener?.close() } catch (_: IOException) {}
        try { testListener?.close() } catch (_: IOException) {}
        acceptThread?.interrupt()
        File(socketPath).delete()
    }

    private fun prodAcceptLoop() {
        while (running) {
            val client: LocalSocket = try {
                prodListener!!.accept()
            } catch (e: IOException) {
                if (running) Log.w(TAG, "accept failed: $e")
                return
            }
            thread(start = true, isDaemon = true, name = "platformsvc-conn") {
                serveClient(LocalClientStream(client))
            }
        }
    }

    private fun testAcceptLoop() {
        while (running) {
            val ch = try {
                testListener!!.accept()
            } catch (e: IOException) {
                if (running) Log.w(TAG, "accept(test) failed: $e")
                return
            }
            thread(start = true, isDaemon = true, name = "platformsvc-conn-test") {
                serveClient(NioClientStream(ch))
            }
        }
    }

    private fun serveClient(stream: ClientStream) {
        val out = stream.outputStream()
        val input = stream.inputStream()
        var registered = false
        try {
            while (running) {
                val req = WireCodec.readFrame(input)
                // Bluetooth-serial notifications are fire-and-forget: replies
                // are produced asynchronously by BtSerialAdapter via the
                // broadcastBt path. Dispatch directly and skip the synchronous
                // response branch. Treat a null adapter as a no-op so unit
                // tests (which never attach one) still work.
                when (req.bodyCase) {
                    PlatformMessage.BodyCase.SERIAL_OPEN -> {
                        val req2 = req.serialOpen
                        if (req2.kind == SerialKind.SERIAL_KIND_USB) {
                            val adapter = usbSerialAdapter
                            if (adapter == null) Log.d(TAG, "SERIAL_OPEN(USB) with no UsbSerialAdapter; dropping")
                            else adapter.handleSerialOpen(req2)
                        } else {
                            val adapter = btSerialAdapter
                            if (adapter == null) Log.d(TAG, "SERIAL_OPEN(BT) with no BtSerialAdapter; dropping")
                            else adapter.handleSerialOpen(req2)
                        }
                        continue
                    }
                    PlatformMessage.BodyCase.SERIAL_DATA -> {
                        // Handles are globally unique across transports; each
                        // adapter no-ops a handle it doesn't own, so forward to
                        // both rather than tracking handle->kind here.
                        btSerialAdapter?.handleSerialData(req.serialData)
                        usbSerialAdapter?.handleSerialData(req.serialData)
                        continue
                    }
                    PlatformMessage.BodyCase.SERIAL_CLOSE -> {
                        btSerialAdapter?.handleSerialClose(req.serialClose)
                        usbSerialAdapter?.handleSerialClose(req.serialClose)
                        continue
                    }
                    PlatformMessage.BodyCase.AVAILABLE_USB_SERIAL_DEVICES_REQUEST -> {
                        val adapter = usbSerialAdapter
                        if (adapter == null) Log.d(TAG, "AVAILABLE_USB_SERIAL_DEVICES_REQUEST with no UsbSerialAdapter; dropping")
                        else adapter.handleAvailableRequest()
                        continue
                    }
                    PlatformMessage.BodyCase.BONDED_BT_DEVICES_REQUEST -> {
                        val adapter = btSerialAdapter
                        if (adapter == null) {
                            Log.d(TAG, "BONDED_BT_DEVICES_REQUEST with no BtSerialAdapter attached; dropping")
                        } else {
                            adapter.handleBondedRequest()
                        }
                        continue
                    }
                    PlatformMessage.BodyCase.USB_LIST_REQ -> {
                        val lister = usbDeviceLister
                        val devices = if (lister != null) {
                            try { lister(req.usbListReq.classFilter) } catch (e: Throwable) {
                                Log.w(TAG, "usbDeviceLister threw; replying empty", e)
                                emptyList()
                            }
                        } else {
                            Log.d(TAG, "USB_LIST_REQ with no usbDeviceLister attached; replying empty")
                            emptyList()
                        }
                        val resp = PlatformMessage.newBuilder()
                            .setUsbListResp(
                                UsbDeviceListResponse.newBuilder()
                                    .addAllDevices(devices)
                                    .build()
                            )
                            .build()
                        try {
                            synchronized(out) { WireCodec.writeFrame(out, resp) }
                        } catch (_: IOException) {
                            // serveClient will tear down on next read failure
                        }
                        continue
                    }
                    else -> { /* fall through to synchronous-handler path */ }
                }
                val handler: MessageHandler = when (req.bodyCase) {
                    PlatformMessage.BodyCase.HELLO ->
                        HelloHandler(serverVersion, schemaVersion)
                    PlatformMessage.BodyCase.GPS_FIX ->
                        GpsFixHandler(onFix = { fix -> gpsFixSubs.forEach { it(fix) } })
                    else -> {
                        Log.d(TAG, "dropping unhandled notification ${req.bodyCase.name}")
                        continue
                    }
                }
                val resp = handler.handle(req)
                if (resp != null) {
                    synchronized(out) { WireCodec.writeFrame(out, resp) }
                    // Register into activeOutputs only after a successful Hello
                    // round-trip — otherwise a broadcast that fires during the
                    // pre-Hello window would write into an unframed stream the
                    // client isn't yet ready to read, and the first fix could
                    // be silently lost. Hello is the only handshake message,
                    // so register exactly once on its success.
                    if (!registered &&
                        req.bodyCase == PlatformMessage.BodyCase.HELLO &&
                        resp.bodyCase != PlatformMessage.BodyCase.ERROR) {
                        synchronized(outputsLock) { activeOutputs = activeOutputs + out }
                        registered = true
                    }
                    if (req.bodyCase == PlatformMessage.BodyCase.HELLO &&
                        resp.bodyCase == PlatformMessage.BodyCase.ERROR) {
                        return
                    }
                }
            }
        } catch (e: IOException) {
            Log.i(TAG, "client disconnected: $e")
        } finally {
            if (registered) {
                synchronized(outputsLock) { activeOutputs = activeOutputs - out }
            }
            try { stream.close() } catch (_: IOException) {}
        }
    }

    companion object {
        private const val TAG = "PlatformServer"

        // Total time start() waits for a predecessor to free the abstract
        // address before giving up. Kept short because the only main-thread
        // caller is a START_STICKY restart's onCreate (ANR budget ~5s); the
        // common case binds on the first attempt. The UI-gated launch path
        // (MainActivity) already waits off the main thread, so this is only
        // a backstop.
        private const val BIND_WAIT_MS = 3_000L
        private const val BIND_RETRY_STEP_MS = 100L
    }
}

private interface ClientStream : Closeable {
    fun inputStream(): InputStream
    fun outputStream(): OutputStream
}

private class LocalClientStream(private val sock: LocalSocket) : ClientStream {
    override fun inputStream(): InputStream = sock.inputStream
    override fun outputStream(): OutputStream = sock.outputStream
    override fun close() { sock.close() }
}

private class NioClientStream(
    private val ch: java.nio.channels.SocketChannel,
) : ClientStream {
    override fun inputStream(): InputStream = java.nio.channels.Channels.newInputStream(ch)
    override fun outputStream(): OutputStream = java.nio.channels.Channels.newOutputStream(ch)
    override fun close() { ch.close() }
}
