package com.nw5w.graywolf.platformsvc

import android.content.Context
import android.util.Log
import com.google.protobuf.ByteString
import com.nw5w.graywolf.platformproto.BleScanError
import com.nw5w.graywolf.platformproto.BleScanResult
import com.nw5w.graywolf.platformproto.BleRepairAck
import com.nw5w.graywolf.platformproto.BleRepairRequest
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
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import java.util.concurrent.ConcurrentHashMap

/**
 * BleAdapter handles BLE KISS scanning and GATT byte-relay for the platform
 * service. Scanning runs on the IO dispatcher (the Android BLE scanner posts
 * results on the main thread, but we push them from a worker). GATT connects
 * also run on the IO dispatcher since they block until the connection is ready.
 *
 * sendMessage is the callback wired by PlatformServer to push frames back to
 * the connected Go client. Mirrors the BtSerialAdapter lifecycle contract.
 */
class BleAdapter(
    private val facade: BleFacade,
    private val appContext: Context,
    private val workerDispatcher: CoroutineDispatcher = Dispatchers.IO,
    private val sendMessage: (PlatformMessage) -> Unit,
) {
    private val tag = "BleAdapter"
    private val scope = CoroutineScope(SupervisorJob() + workerDispatcher)
    private val handles = ConcurrentHashMap<UInt, HandleState>()

    @Volatile private var scanActive = false

    private data class HandleState(
        val mac: String,
        val session: BleGattSession,
        val readJob: Job,
    )

    // -----------------------------------------------------------------------
    // BLE scan
    // -----------------------------------------------------------------------

    /** Called by PlatformServer on BleScanRequest. */
    fun handleScanRequest() {
        if (scanActive) return
        scanActive = true
        facade.startScan(
            onFound = { device ->
                sendMessage(
                    PlatformMessage.newBuilder().setBleScanResult(
                        BleScanResult.newBuilder()
                            .setAddr(device.mac)
                            .setName(device.name)
                            .setRssi(device.rssi)
                            .build()
                    ).build()
                )
            },
            onError = { msg ->
                scanActive = false
                sendMessage(
                    PlatformMessage.newBuilder().setBleScanError(
                        BleScanError.newBuilder()
                            .setCode(msg.substringBefore(':'))
                            .setDetail(msg)
                            .build()
                    ).build()
                )
            },
        )
        Log.d(tag, "BLE scan started")
    }

    /** Called by PlatformServer on BleScanStop. */
    fun handleScanStop() {
        if (!scanActive) return
        scanActive = false
        facade.stopScan()
        Log.d(tag, "BLE scan stopped")
    }

    /** Called by PlatformServer on BleRepairRequest: remove the bond so the next
     * connectGatt triggers fresh pairing with the TNC. */
    fun handleRepairRequest(req: BleRepairRequest) {
        scope.launch {
            // Tear down any live GATT session to this device first; removeBond
            // while a GATT connection is open causes undefined behaviour on many chipsets.
            handles.entries
                .filter { it.value.mac.equals(req.mac, ignoreCase = true) }
                .toList()
                .forEach { (handle, _) -> closeQuietly(handle, "repair") }
            delay(300)
            val ok = facade.removeBond(req.mac)
            Log.i(tag, "BLE repair bond removal mac=${req.mac} ok=$ok")
            sendMessage(PlatformMessage.newBuilder().setBleRepairAck(
                BleRepairAck.newBuilder().setOk(ok).build()
            ).build())
        }
    }

    // -----------------------------------------------------------------------
    // BLE serial open / data / close
    // -----------------------------------------------------------------------

    fun handleSerialOpen(req: SerialOpen) {
        val handle = req.handle.toUInt()
        val mac = req.address
        if (req.kind != SerialKind.SERIAL_KIND_BLE) {
            sendAck(handle, ok = false, err = "unsupported_kind: ${req.kind}")
            return
        }
        scope.launch {
            val session = try {
                facade.openGatt(appContext, mac)
            } catch (e: Exception) {
                Log.w(tag, "BLE openGatt failed for $mac: ${e.message}")
                sendAck(handle, ok = false, err = e.message ?: "gatt_error")
                return@launch
            }

            val readJob = scope.launch { readPump(handle, session) }
            handles[handle] = HandleState(mac, session, readJob)
            sendAck(handle, ok = true, err = "")
        }
    }

    fun handleSerialData(req: SerialData) {
        val handle = req.handle.toUInt()
        val state = handles[handle] ?: return
        val bytes = req.data.toByteArray()
        scope.launch {
            try {
                state.session.write(bytes)
            } catch (e: Exception) {
                sendError(handle, "io_error", e.message ?: "")
                closeQuietly(handle, "write failed")
            }
        }
    }

    fun handleSerialClose(req: SerialClose) {
        val handle = req.handle.toUInt()
        closeQuietly(handle, req.reason.ifEmpty { "client_close" })
    }

    fun shutdown() {
        handleScanStop()
        handles.keys.toList().forEach { closeQuietly(it, "shutdown") }
        runBlocking { scope.coroutineContext[Job]?.cancelAndJoin() }
    }

    // -----------------------------------------------------------------------
    // Internal
    // -----------------------------------------------------------------------

    private suspend fun readPump(handle: UInt, session: BleGattSession) {
        val ch = kotlinx.coroutines.channels.Channel<ByteArray>(64)
        session.onData { bytes -> ch.trySend(bytes) }
        // Closing ch causes the for-loop below to exit → closeQuietly sends SerialClose
        // to Go → Read() returns io.EOF → SerialSupervisor reconnects automatically.
        session.onDisconnect { ch.close() }
        try {
            for (bytes in ch) {
                sendMessage(
                    PlatformMessage.newBuilder().setSerialData(
                        SerialData.newBuilder()
                            .setHandle(handle.toInt())
                            .setData(ByteString.copyFrom(bytes))
                            .build()
                    ).build()
                )
            }
        } finally {
            closeQuietly(handle, "ble_link_lost")
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
}
