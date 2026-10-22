package com.nw5w.graywolf.platformsvc

import android.bluetooth.BluetoothSocket
import android.util.Log
import com.google.protobuf.ByteString
import com.nw5w.graywolf.platformproto.BondedBtDevicesResponse
import com.nw5w.graywolf.platformproto.PlatformMessage
import com.nw5w.graywolf.platformproto.SerialClose
import com.nw5w.graywolf.platformproto.SerialData
import com.nw5w.graywolf.platformproto.SerialError
import com.nw5w.graywolf.platformproto.SerialKind
import com.nw5w.graywolf.platformproto.SerialOpen
import com.nw5w.graywolf.platformproto.SerialOpenAck
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import java.io.IOException
import java.util.concurrent.ConcurrentHashMap

/**
 * BtSerialAdapter handles Bluetooth-classic SPP/RFCOMM byte relay for the
 * platform service. All BluetoothAdapter / RFCOMM calls run on the
 * worker dispatcher; never the main thread.
 *
 * sendMessage is the callback the PlatformServer wires up to push frames
 * back to the connected Go client.
 */
class BtSerialAdapter(
    private val facade: BluetoothFacade,
    private val workerDispatcher: CoroutineDispatcher = Dispatchers.IO,
    private val sendMessage: (PlatformMessage) -> Unit,
) {
    private val tag = "BtSerialAdapter"
    private val scope = CoroutineScope(SupervisorJob() + workerDispatcher)
    private val handles = ConcurrentHashMap<UInt, HandleState>()

    // Handles the Go side closed while their RFCOMM connect was still in
    // flight (its open timed out). The connect coroutine checks this set
    // when connect() returns and closes the socket instead of registering
    // it, so an abandoned open can't keep holding the radio's SPP link.
    private val abandoned = ConcurrentHashMap.newKeySet<UInt>()

    private data class HandleState(
        val mac: String,
        val socket: BluetoothSocket,
        val readJob: Job,
        val mutex: Mutex = Mutex(),
    )

    fun handleBondedRequest() {
        scope.launch {
            // Every request MUST produce exactly one reply. The Go client
            // blocks (holding its single-request lock) until this response
            // arrives, so any uncaught failure here spins the KISS
            // bonded-device picker on "Loading" forever and wedges the
            // platform request channel. Catch everything — a denied
            // permission surfaces as SecurityException, but a resetting
            // Bluetooth stack can throw DeadObjectException / IllegalState —
            // and always send a (possibly empty) list.
            val devices = try {
                facade.bondedDevices()
            } catch (sec: SecurityException) {
                Log.w(tag, "BLUETOOTH_CONNECT permission missing", sec)
                emptyList()
            } catch (cancel: kotlinx.coroutines.CancellationException) {
                // Never swallow cooperative cancellation (e.g. shutdown()) —
                // rethrow so the coroutine unwinds instead of sending a stray reply.
                throw cancel
            } catch (t: Throwable) {
                Log.w(tag, "bonded-device enumeration failed; replying empty", t)
                emptyList()
            }
            Log.i(tag, "bonded-device request: ${devices.size} device(s): " +
                devices.joinToString { "${it.name} (${it.mac})" })
            val resp = BondedBtDevicesResponse.newBuilder().apply {
                devices.forEach {
                    addDevices(
                        BondedBtDevicesResponse.Device.newBuilder()
                            .setMac(it.mac)
                            .setName(it.name)
                            .build()
                    )
                }
            }.build()
            sendMessage(
                PlatformMessage.newBuilder()
                    .setBondedBtDevicesResponse(resp)
                    .build()
            )
        }
    }

    fun handleSerialOpen(req: SerialOpen) {
        val handle = req.handle.toUInt()
        val mac = req.address
        if (req.kind != SerialKind.SERIAL_KIND_BLUETOOTH) {
            sendAck(handle, ok = false, err = "unsupported_kind: ${req.kind}")
            return
        }
        // isBonded reads BluetoothAdapter.bondedDevices, which throws
        // SecurityException without BLUETOOTH_CONNECT. This runs on the
        // platformsvc connection thread, so an escaped exception would kill
        // that thread rather than produce an ack.
        val bonded = try {
            facade.isBonded(mac)
        } catch (sec: SecurityException) {
            Log.w(tag, "BLUETOOTH_CONNECT permission missing", sec)
            sendAck(handle, ok = false, err = "permission_denied: grant Bluetooth permission to Graywolf")
            return
        }
        if (!bonded) {
            sendAck(handle, ok = false, err = "not_bonded: $mac")
            return
        }
        Log.i(tag, "serial open handle=$handle mac=$mac: connecting")
        scope.launch {
            val socket = try {
                facade.connectRfcomm(mac)
            } catch (sec: SecurityException) {
                sendAck(handle, ok = false, err = "permission_denied: grant Bluetooth permission to Graywolf")
                return@launch
            } catch (cancel: kotlinx.coroutines.CancellationException) {
                throw cancel
            } catch (e: Throwable) {
                // Every open MUST produce exactly one ack; the Go side waits
                // for it. Anything connectRfcomm throws (IOException with the
                // per-attempt detail, IllegalArgumentException for a bad MAC,
                // a resetting BT stack) becomes a failed ack.
                Log.w(tag, "serial open handle=$handle mac=$mac failed", e)
                abandoned.remove(handle)
                sendAck(handle, ok = false, err = "connect failed: ${e.message ?: e.javaClass.simpleName}")
                return@launch
            }
            if (abandoned.remove(handle)) {
                Log.w(tag, "serial open handle=$handle mac=$mac: connected after Go gave up; closing")
                try { socket.close() } catch (_: Throwable) {}
                return@launch
            }
            val readJob = scope.launch { readPump(handle, socket) }
            handles[handle] = HandleState(mac, socket, readJob)
            sendAck(handle, ok = true, err = "")
        }
    }

    fun handleSerialData(req: SerialData) {
        val handle = req.handle.toUInt()
        val state = handles[handle] ?: return
        val bytes = req.data.toByteArray()
        scope.launch {
            state.mutex.withLock {
                try {
                    state.socket.outputStream.write(bytes)
                    state.socket.outputStream.flush()
                } catch (e: IOException) {
                    sendError(handle, "io_error", e.message ?: "")
                    closeQuietly(handle, "write failed")
                }
            }
        }
    }

    fun handleSerialClose(req: SerialClose) {
        val handle = req.handle.toUInt()
        val state = handles.remove(handle)
        if (state == null) {
            // No live socket yet: the open may still be connecting.
            abandoned.add(handle)
            return
        }
        scope.launch {
            // socket.close() first: the read pump is blocked in a native JNI
            // inputStream.read() that coroutine cancellation cannot interrupt;
            // closing the socket unblocks it via IOException.
            try { state.socket.close() } catch (_: Throwable) {}
            try { state.readJob.cancelAndJoin() } catch (_: Throwable) {}
        }
    }

    /** Called by GraywolfService on ACTION_BOND_STATE_CHANGED transitioning to BOND_NONE. */
    fun onBondLost(mac: String) {
        handles.entries
            .filter { it.value.mac.equals(mac, ignoreCase = true) }
            .forEach { (handle, _) ->
                sendError(handle, "bond_lost", "device $mac unpaired")
                closeQuietly(handle, "bond_lost")
            }
        // Then push the refreshed bonded list:
        handleBondedRequest()
    }

    fun shutdown() {
        handles.keys.toList().forEach { closeQuietly(it, "shutdown") }
        runBlocking { scope.coroutineContext[Job]?.cancelAndJoin() }
    }

    private suspend fun readPump(handle: UInt, socket: BluetoothSocket) {
        val buf = ByteArray(4096)
        var firstRead = true
        try {
            while (true) {
                val n = socket.inputStream.read(buf)
                if (firstRead && n > 0) {
                    Log.i(tag, "serial handle=$handle: first $n byte(s) from radio")
                    firstRead = false
                }
                if (n < 0) {
                    sendError(handle, "rfcomm_closed", "EOF")
                    closeQuietly(handle, "rfcomm EOF")
                    return
                }
                if (n == 0) continue
                sendMessage(
                    PlatformMessage.newBuilder().setSerialData(
                        SerialData.newBuilder()
                            .setHandle(handle.toInt())
                            .setData(ByteString.copyFrom(buf, 0, n))
                            .build()
                    ).build()
                )
            }
        } catch (e: IOException) {
            sendError(handle, "io_error", e.message ?: "")
            closeQuietly(handle, "read failed")
        }
    }

    private fun sendAck(handle: UInt, ok: Boolean, err: String) {
        sendMessage(PlatformMessage.newBuilder().setSerialOpenAck(
            SerialOpenAck.newBuilder()
                .setHandle(handle.toInt())
                .setOk(ok)
                .setError(err)
                .build()
        ).build())
    }

    private fun sendError(handle: UInt, code: String, detail: String) {
        sendMessage(PlatformMessage.newBuilder().setSerialError(
            SerialError.newBuilder()
                .setHandle(handle.toInt())
                .setCode(code)
                .setDetail(detail)
                .build()
        ).build())
    }

    private fun closeQuietly(handle: UInt, reason: String) {
        val state = handles.remove(handle) ?: return
        try { state.socket.close() } catch (_: Throwable) {}
        state.readJob.cancel()
        sendMessage(PlatformMessage.newBuilder().setSerialClose(
            SerialClose.newBuilder()
                .setHandle(handle.toInt())
                .setReason(reason)
                .build()
        ).build())
    }
}
