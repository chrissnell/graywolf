package com.nw5w.graywolf.platformsvc

import android.util.Log
import com.google.protobuf.ByteString
import com.nw5w.graywolf.platformproto.AvailableUsbSerialDevicesResponse
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
 * UsbSerialAdapter relays USB serial bytes for the platform service, mirroring
 * BtSerialAdapter. All UsbManager / port calls run on the worker dispatcher
 * (never the main thread — memory feedback_android_usb_open_worker_thread).
 *
 * sendMessage is the PlatformServer callback that pushes frames back to the Go
 * client. handleSerialData / handleSerialClose are no-ops for handles this
 * adapter does not own, so PlatformServer can forward those frames to both the
 * Bluetooth and USB adapters and each ignores the other's handles.
 */
class UsbSerialAdapter(
    private val facade: UsbSerialFacade,
    private val workerDispatcher: CoroutineDispatcher = Dispatchers.IO,
    private val sendMessage: (PlatformMessage) -> Unit,
) {
    private val tag = "UsbSerialAdapter"
    private val scope = CoroutineScope(SupervisorJob() + workerDispatcher)
    private val handles = ConcurrentHashMap<UInt, HandleState>()

    private data class HandleState(
        val vidPid: String,
        val deviceName: String,
        val session: UsbSerialSession,
        val readJob: Job,
        val mutex: Mutex = Mutex(),
    )

    fun handleAvailableRequest() {
        scope.launch {
            val devices = try {
                facade.available()
            } catch (t: Throwable) {
                Log.w(tag, "available() threw; returning empty", t)
                emptyList()
            }
            val resp = AvailableUsbSerialDevicesResponse.newBuilder().apply {
                devices.forEach {
                    addDevices(
                        AvailableUsbSerialDevicesResponse.Device.newBuilder()
                            .setVidPid(it.vidPid)
                            .setProduct(it.product)
                            .setManufacturer(it.manufacturer)
                            .setHasPermission(it.hasPermission)
                            .build()
                    )
                }
            }.build()
            sendMessage(
                PlatformMessage.newBuilder()
                    .setAvailableUsbSerialDevicesResponse(resp)
                    .build()
            )
        }
    }

    fun handleSerialOpen(req: SerialOpen) {
        val handle = req.handle.toUInt()
        if (req.kind != SerialKind.SERIAL_KIND_USB) {
            // Defensive: PlatformServer routes by kind, so this shouldn't fire.
            sendAck(handle, ok = false, err = "unsupported_kind: ${req.kind}")
            return
        }
        val vidPid = req.address
        val baud = req.baud.toInt()
        scope.launch {
            val session = try {
                facade.open(vidPid, baud)
            } catch (e: UsbSerialOpenException) {
                sendAck(handle, ok = false, err = e.code)
                return@launch
            } catch (e: Throwable) {
                sendAck(handle, ok = false, err = "io_error")
                return@launch
            }
            val readJob = scope.launch { readPump(handle, session) }
            handles[handle] = HandleState(vidPid, session.deviceName, session, readJob)
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
                    state.session.write(bytes, WRITE_TIMEOUT_MS)
                } catch (e: Throwable) {
                    sendError(handle, "io_error", e.message ?: "")
                    closeQuietly(handle, "write failed")
                }
            }
        }
    }

    fun handleSerialClose(req: SerialClose) {
        val handle = req.handle.toUInt()
        val state = handles.remove(handle) ?: return
        scope.launch {
            // Close the port first: the read pump may be blocked in a native
            // read that coroutine cancellation can't interrupt; closing
            // unblocks it. session.close() also releases the arbiter claim.
            try { state.session.close() } catch (_: Throwable) {}
            try { state.readJob.cancelAndJoin() } catch (_: Throwable) {}
        }
    }

    /**
     * Called by GraywolfService on ACTION_USB_DEVICE_DETACHED. Recoverable:
     * emit SerialError(usb_detached) + close the handle so SerialSupervisor
     * backs off and auto-reconnects on re-attach. Then refresh the picker.
     */
    fun onUsbDetached(deviceName: String) {
        handles.entries
            .filter { it.value.deviceName == deviceName }
            .forEach { (handle, _) ->
                sendError(handle, "usb_detached", "device $deviceName unplugged")
                closeQuietly(handle, "usb_detached")
            }
        handleAvailableRequest()
    }

    fun shutdown() {
        handles.keys.toList().forEach { closeQuietly(it, "shutdown") }
        runBlocking { scope.coroutineContext[Job]?.cancelAndJoin() }
    }

    private suspend fun readPump(handle: UInt, session: UsbSerialSession) {
        val buf = ByteArray(4096)
        try {
            while (true) {
                val n = session.read(buf, READ_TIMEOUT_MS)
                if (n < 0) {
                    sendError(handle, "io_error", "EOF")
                    closeQuietly(handle, "read EOF")
                    return
                }
                if (n == 0) continue // read timeout with no data; poll again
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
        } catch (e: Throwable) {
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
        try { state.session.close() } catch (_: Throwable) {}
        state.readJob.cancel()
        sendMessage(PlatformMessage.newBuilder().setSerialClose(
            SerialClose.newBuilder()
                .setHandle(handle.toInt())
                .setReason(reason)
                .build()
        ).build())
    }

    companion object {
        // Positive read timeout so the pump wakes periodically (n==0) and can
        // observe close()/cancel rather than parking forever in native read.
        private const val READ_TIMEOUT_MS = 200
        private const val WRITE_TIMEOUT_MS = 2000
    }
}
