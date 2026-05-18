package com.nw5w.graywolf.platformsvc

import android.net.LocalServerSocket
import android.net.LocalSocket
import android.util.Log
import com.nw5w.graywolf.platformproto.GnssStatusUpdate
import com.nw5w.graywolf.platformproto.GpsFix
import com.nw5w.graywolf.platformproto.PlatformMessage
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
     * Production startup. Binds an android.net.LocalServerSocket at
     * NAMESPACE_FILESYSTEM. Available on every Android API level the app
     * supports.
     */
    fun start() {
        // Lesson: feedback_uds_unlink_before_bind. The Service can be
        // killed/restarted by the OS; the prior socket file lingers.
        File(socketPath).delete()

        prodListener = LocalServerSocket(socketPath)
        running = true
        acceptThread = thread(start = true, isDaemon = true, name = "platformsvc-accept") {
            prodAcceptLoop()
        }
        Log.i(TAG, "PlatformServer bound at $socketPath")
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
