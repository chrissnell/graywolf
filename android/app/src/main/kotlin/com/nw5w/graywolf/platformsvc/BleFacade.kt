package com.nw5w.graywolf.platformsvc

import android.bluetooth.BluetoothAdapter
import android.bluetooth.BluetoothDevice
import android.bluetooth.BluetoothGatt
import android.bluetooth.BluetoothGattCallback
import android.bluetooth.BluetoothGattCharacteristic
import android.bluetooth.BluetoothGattDescriptor
import android.bluetooth.le.ScanCallback
import android.bluetooth.le.ScanFilter
import android.bluetooth.le.ScanResult
import android.bluetooth.le.ScanSettings
import android.content.Context
import android.os.Build
import android.os.ParcelUuid
import android.util.Log
import java.util.UUID

/** A single BLE KISS TNC device found during a scan. */
data class BleFoundDevice(val mac: String, val name: String, val rssi: Int)

/** Narrow interface around the Android BLE API to allow unit testing. */
interface BleFacade {
    /** Start scanning; calls onFound for each matching device, onError if the scan cannot start. Idempotent if already scanning. */
    fun startScan(onFound: (BleFoundDevice) -> Unit, onError: ((String) -> Unit)? = null)
    /** Stop an in-progress scan. Idempotent. */
    fun stopScan()
    /**
     * Open a GATT connection to [mac], discover services, enable TX notifications,
     * and return a [BleGattSession] for data relay. Throws on failure.
     * MUST be called from a worker thread (blocks until connection completes or fails).
     */
    fun openGatt(context: Context, mac: String): BleGattSession
    /** Remove the Android bond for [mac] so the next connect triggers fresh pairing. */
    fun removeBond(mac: String): Boolean
}

/** GATT session returned by [BleFacade.openGatt]. Caller owns close(). */
interface BleGattSession {
    /** Write [bytes] to the TNC RX characteristic (write-without-response). */
    fun write(bytes: ByteArray)
    /** Register a callback invoked on each inbound notification from the TNC TX characteristic. */
    fun onData(cb: (ByteArray) -> Unit)
    /** Register a one-shot callback invoked when the GATT link drops after successful init. */
    fun onDisconnect(cb: () -> Unit)
    /** Tear down the GATT connection. */
    fun close()
}

/** Production implementation backed by android.bluetooth. */
class SystemBleFacade(
    private val adapter: BluetoothAdapter?,
    private val appContext: Context,
) : BleFacade {

    companion object {
        // Mobilinkd TNC3/TNC4 proprietary GATT service / characteristics.
        val MOBILINKD_SVC: UUID = UUID.fromString("00000001-ba2a-46c9-ae49-01b0961f68bb")
        val MOBILINKD_TX:  UUID = UUID.fromString("00000003-ba2a-46c9-ae49-01b0961f68bb")
        val MOBILINKD_RX:  UUID = UUID.fromString("00000002-ba2a-46c9-ae49-01b0961f68bb")

        // Nordic UART Service (NUS) — BTECH UV-PRO, VERO VR-N76, Radioddity GA-5WB, etc.
        val NUS_SVC: UUID = UUID.fromString("6E400001-B5A3-F393-E0A9-E50E24DCCA9E")
        val NUS_TX:  UUID = UUID.fromString("6E400003-B5A3-F393-E0A9-E50E24DCCA9E")
        val NUS_RX:  UUID = UUID.fromString("6E400002-B5A3-F393-E0A9-E50E24DCCA9E")

        // CCCD descriptor UUID for enabling notifications.
        val CCCD: UUID = UUID.fromString("00002902-0000-1000-8000-00805f9b34fb")

        private val SCAN_SERVICE_UUIDS = listOf(MOBILINKD_SVC, NUS_SVC)
    }

    @Volatile private var activeScanCallback: ScanCallback? = null

    override fun startScan(onFound: (BleFoundDevice) -> Unit, onError: ((String) -> Unit)?) {
        val scanner = adapter?.bluetoothLeScanner ?: run {
            val msg = "bluetoothLeScanner unavailable (Bluetooth off or adapter missing)"
            Log.e("SystemBleFacade", msg)
            onError?.invoke(msg)
            return
        }
        if (activeScanCallback != null) return

        val filters = SCAN_SERVICE_UUIDS.map { uuid ->
            ScanFilter.Builder().setServiceUuid(ParcelUuid(uuid)).build()
        }
        val settings = ScanSettings.Builder()
            .setScanMode(ScanSettings.SCAN_MODE_LOW_LATENCY)
            .build()

        val seen = mutableSetOf<String>()
        val cb = object : ScanCallback() {
            override fun onScanResult(callbackType: Int, result: ScanResult) {
                val mac = result.device.address ?: return
                if (!seen.add(mac)) return
                // Prefer the advertised name from the scan record — it is available
                // without BLUETOOTH_CONNECT. Fall back to device.name only as a last resort.
                val name = result.scanRecord?.deviceName
                    ?: try { result.device.name } catch (_: SecurityException) { null }
                onFound(BleFoundDevice(mac = mac, name = name ?: mac, rssi = result.rssi))
            }

            override fun onScanFailed(errorCode: Int) {
                activeScanCallback = null
                val msg = "scan_failed: " + when (errorCode) {
                    SCAN_FAILED_ALREADY_STARTED            -> "already_started"
                    SCAN_FAILED_APPLICATION_REGISTRATION_FAILED -> "registration_failed"
                    SCAN_FAILED_INTERNAL_ERROR             -> "internal_error"
                    SCAN_FAILED_FEATURE_UNSUPPORTED        -> "feature_unsupported"
                    5 /* OUT_OF_HARDWARE_RESOURCES */       -> "out_of_hardware_resources"
                    6 /* SCANNING_TOO_FREQUENTLY */         -> "scanning_too_frequently"
                    else                                   -> "unknown_code_$errorCode"
                }
                Log.e("SystemBleFacade", msg)
                onError?.invoke(msg)
            }
        }
        activeScanCallback = cb
        try {
            scanner.startScan(filters, settings, cb)
        } catch (e: SecurityException) {
            activeScanCallback = null
            val msg = "permission_denied: BLUETOOTH_SCAN not granted (${e.message})"
            Log.e("SystemBleFacade", msg)
            onError?.invoke(msg)
        }
    }

    override fun stopScan() {
        val cb = activeScanCallback ?: return
        activeScanCallback = null
        try {
            adapter?.bluetoothLeScanner?.stopScan(cb)
        } catch (_: SecurityException) { /* ignore */ }
    }

    override fun openGatt(context: Context, mac: String): BleGattSession {
        val a = adapter ?: error("Bluetooth adapter not available")
        val device: BluetoothDevice = try {
            a.getRemoteDevice(mac)
        } catch (_: IllegalArgumentException) {
            error("BLE: invalid MAC address: $mac")
        }
        return SystemBleGattSession(device, context)
    }

    override fun removeBond(mac: String): Boolean {
        val a = adapter ?: return false
        val device = try { a.getRemoteDevice(mac) } catch (_: Exception) { return false }
        // Already unbonded — goal state is already reached.
        try { if (device.bondState != BluetoothDevice.BOND_BONDED) return true } catch (_: SecurityException) { /* proceed */ }
        return try {
            device.javaClass.getMethod("removeBond").invoke(device) as Boolean
        } catch (_: Exception) { false }
    }
}

/**
 * Blocking GATT session for one BLE KISS TNC. Constructor returns after
 * the connection is established, services are discovered, and TX
 * notifications are enabled — or throws if any step fails.
 * MUST be constructed on a worker thread (not the main thread).
 */
class SystemBleGattSession(
    private val device: BluetoothDevice,
    private val context: Context,
) : BleGattSession {
    private val lock = java.util.concurrent.locks.ReentrantLock()
    private val cond = lock.newCondition()

    private enum class State { CONNECTING, CONNECTED, SERVICES_DISCOVERED, READY, FAILED, CLOSED }
    @Volatile private var state = State.CONNECTING

    private var gatt: BluetoothGatt? = null
    private var rxChar: BluetoothGattCharacteristic? = null
    @Volatile private var dataCallback: ((ByteArray) -> Unit)? = null
    @Volatile private var disconnectCallback: (() -> Unit)? = null

    // Write serialization: BLE does not allow concurrent characteristic writes.
    private val writeLock = java.util.concurrent.locks.ReentrantLock()
    private val writeReady = writeLock.newCondition()
    @Volatile private var writePending = false

    private val gattCallback = object : BluetoothGattCallback() {
        override fun onConnectionStateChange(g: BluetoothGatt, status: Int, newState: Int) {
            when (newState) {
                BluetoothGatt.STATE_CONNECTED -> {
                    setState(State.CONNECTED)
                    // Clear stale GATT service cache, then negotiate MTU before service
                    // discovery: correct BLE order is connect → MTU → discover → CCCD.
                    // Small delay lets the cache refresh settle before the next command.
                    g.refreshGattCache()
                    android.os.Handler(android.os.Looper.getMainLooper()).postDelayed({
                        try {
                            // onMtuChanged triggers discoverServices on success; fall through
                            // to discoverServices directly only if requestMtu can't be queued.
                            if (!g.requestMtu(517)) g.discoverServices()
                        } catch (_: Throwable) {
                            g.discoverServices()
                        }
                    }, 300)
                }
                else -> {
                    val wasReady = state == State.READY
                    if (state != State.CLOSED) setState(State.FAILED)
                    if (wasReady) disconnectCallback?.invoke()
                    signal()
                    // Release the GATT client slot immediately so future connect
                    // attempts are not blocked by a lingering registration.
                    try { g.close() } catch (_: Throwable) {}
                }
            }
        }

        override fun onServicesDiscovered(g: BluetoothGatt, status: Int) {
            if (status != BluetoothGatt.GATT_SUCCESS) { setState(State.FAILED); signal(); return }
            setState(State.SERVICES_DISCOVERED)
            signal()
        }

        override fun onCharacteristicChanged(
            g: BluetoothGatt,
            characteristic: BluetoothGattCharacteristic,
            value: ByteArray,
        ) {
            dataCallback?.invoke(value.copyOf())
        }

        @Deprecated("Deprecated in Java")
        @Suppress("DEPRECATION")
        override fun onCharacteristicChanged(
            g: BluetoothGatt,
            characteristic: BluetoothGattCharacteristic,
        ) {
            onCharacteristicChanged(g, characteristic, characteristic.value ?: return)
        }

        @Suppress("DEPRECATION")
        override fun onDescriptorWrite(g: BluetoothGatt, descriptor: BluetoothGattDescriptor, status: Int) {
            setState(State.READY)
            signal()
        }

        override fun onCharacteristicWrite(
            g: BluetoothGatt,
            characteristic: BluetoothGattCharacteristic,
            status: Int,
        ) {
            writeLock.lock()
            try {
                writePending = false
                writeReady.signalAll()
            } finally {
                writeLock.unlock()
            }
        }

        override fun onMtuChanged(g: BluetoothGatt, mtu: Int, status: Int) {
            // MTU response received; proceed with service discovery.
            g.discoverServices()
        }
    }

    init {
        val g = try {
            device.connectGatt(context, false, gattCallback, BluetoothDevice.TRANSPORT_LE)
        } catch (e: SecurityException) {
            error("BLE: BLUETOOTH_CONNECT permission denied: ${e.message}")
        } ?: error("BLE: connectGatt returned null for ${device.address}")
        gatt = g

        var initOk = false
        try {
            // Wait for services: connect → requestMtu (postDelayed 300ms) →
            // onMtuChanged → discoverServices → onServicesDiscovered.
            awaitState(State.SERVICES_DISCOVERED, timeoutMs = 20_000L)

            // Find the first supported GATT profile (Mobilinkd, then NUS).
            val profile = findProfile(g)
                ?: run { g.disconnect(); error("BLE: no recognised KISS service on ${device.address}") }

            rxChar = profile.rx

            // Enable notifications on the TX characteristic.
            try { g.setCharacteristicNotification(profile.tx, true) } catch (e: SecurityException) {
                g.disconnect(); error("BLE: BLUETOOTH_CONNECT permission denied enabling notify")
            }
            val cccdDescriptor = profile.tx.getDescriptor(SystemBleFacade.CCCD)
                ?: run { g.disconnect(); error("BLE: no CCCD on TX characteristic") }

            @Suppress("DEPRECATION")
            cccdDescriptor.value = BluetoothGattDescriptor.ENABLE_NOTIFICATION_VALUE
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                try { g.writeDescriptor(cccdDescriptor, BluetoothGattDescriptor.ENABLE_NOTIFICATION_VALUE) } catch (e: SecurityException) {
                    g.disconnect(); error("BLE: BLUETOOTH_CONNECT permission denied writing CCCD")
                }
            } else {
                @Suppress("DEPRECATION")
                try { g.writeDescriptor(cccdDescriptor) } catch (e: SecurityException) {
                    g.disconnect(); error("BLE: BLUETOOTH_CONNECT permission denied writing CCCD")
                }
            }

            // Wait for CCCD write to complete (→ onDescriptorWrite → READY).
            awaitState(State.READY, timeoutMs = 10_000L)

            // Shorter connection intervals → supervision timeout ~3-5s vs ~30s default.
            try { g.requestConnectionPriority(BluetoothGatt.CONNECTION_PRIORITY_HIGH) } catch (_: SecurityException) {}

            initOk = true
        } finally {
            if (!initOk) {
                // Always release the GATT client slot on any construction failure;
                // not doing so exhausts the BLE stack's client table (~8-16 slots)
                // and prevents reconnection until the Bluetooth stack is restarted.
                try { g.disconnect() } catch (_: Throwable) {}
                try { g.close() } catch (_: Throwable) {}
                gatt = null
            }
        }
    }

    private data class GattProfile(
        val tx: BluetoothGattCharacteristic,
        val rx: BluetoothGattCharacteristic,
    )

    private fun findProfile(g: BluetoothGatt): GattProfile? {
        // Try Mobilinkd first, then NUS.
        for ((svcUuid, txUuid, rxUuid) in listOf(
            Triple(SystemBleFacade.MOBILINKD_SVC, SystemBleFacade.MOBILINKD_TX, SystemBleFacade.MOBILINKD_RX),
            Triple(SystemBleFacade.NUS_SVC,       SystemBleFacade.NUS_TX,       SystemBleFacade.NUS_RX),
        )) {
            val svc = g.getService(svcUuid) ?: continue
            val tx  = svc.getCharacteristic(txUuid) ?: continue
            val rx  = svc.getCharacteristic(rxUuid) ?: continue
            return GattProfile(tx, rx)
        }
        return null
    }

    private fun setState(s: State) { state = s }

    private fun signal() {
        lock.lock(); try { cond.signalAll() } finally { lock.unlock() }
    }

    private fun awaitState(target: State, timeoutMs: Long) {
        val deadline = System.currentTimeMillis() + timeoutMs
        lock.lock()
        try {
            while (state != target) {
                if (state == State.FAILED || state == State.CLOSED)
                    error("BLE: connection to ${device.address} failed (state=$state)")
                val remaining = deadline - System.currentTimeMillis()
                if (remaining <= 0)
                    error("BLE: timed out waiting for $target from ${device.address}")
                cond.await(remaining, java.util.concurrent.TimeUnit.MILLISECONDS)
            }
        } finally {
            lock.unlock()
        }
    }

    override fun onData(cb: (ByteArray) -> Unit) { dataCallback = cb }

    override fun onDisconnect(cb: () -> Unit) { disconnectCallback = cb }

    override fun write(bytes: ByteArray) {
        val rc = rxChar ?: return
        val g  = gatt  ?: return
        // Prefer write-without-response when the characteristic supports it: no
        // per-chunk ATT round-trip and onCharacteristicWrite does NOT fire for WWR
        // on API < 33, so writePending would never clear and every subsequent chunk
        // would stall for 5 s then be dropped.
        val useWwr = rc.properties and BluetoothGattCharacteristic.PROPERTY_WRITE_NO_RESPONSE != 0
        val mtu = 244
        var offset = 0
        while (offset < bytes.size) {
            val end = minOf(offset + mtu, bytes.size)
            val chunk = bytes.copyOfRange(offset, end)

            if (!useWwr) {
                // Serialize write-with-response: wait for the previous ATT ACK.
                writeLock.lock()
                try {
                    val deadline = System.currentTimeMillis() + 5_000L
                    while (writePending) {
                        val remaining = deadline - System.currentTimeMillis()
                        if (remaining <= 0) return
                        writeReady.await(remaining, java.util.concurrent.TimeUnit.MILLISECONDS)
                    }
                    writePending = true
                } finally {
                    writeLock.unlock()
                }
            }

            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                val writeType = if (useWwr) BluetoothGattCharacteristic.WRITE_TYPE_NO_RESPONSE
                                else BluetoothGattCharacteristic.WRITE_TYPE_DEFAULT
                try { g.writeCharacteristic(rc, chunk, writeType) }
                catch (_: SecurityException) {
                    if (!useWwr) { writeLock.lock(); try { writePending = false; writeReady.signalAll() } finally { writeLock.unlock() } }
                    return
                }
            } else {
                @Suppress("DEPRECATION")
                rc.writeType = if (useWwr) BluetoothGattCharacteristic.WRITE_TYPE_NO_RESPONSE
                               else BluetoothGattCharacteristic.WRITE_TYPE_DEFAULT
                @Suppress("DEPRECATION")
                rc.value = chunk
                @Suppress("DEPRECATION")
                try { g.writeCharacteristic(rc) }
                catch (_: SecurityException) {
                    if (!useWwr) { writeLock.lock(); try { writePending = false; writeReady.signalAll() } finally { writeLock.unlock() } }
                    return
                }
            }
            offset = end
        }
    }

    override fun close() {
        setState(State.CLOSED)
        signal()
        dataCallback = null
        disconnectCallback = null
        // disconnect() before close() per Android docs; close() alone leaves the
        // remote device connected until supervision timeout (~30 s by default).
        try { gatt?.disconnect() } catch (_: Throwable) {}
        try { gatt?.close() } catch (_: Throwable) {}
        gatt = null
    }

    // Clears the Android GATT service cache via the internal refresh() method.
    // Prevents stale cache after a previous app connection changed services.
    private fun BluetoothGatt.refreshGattCache(): Boolean = try {
        javaClass.getMethod("refresh").invoke(this) as Boolean
    } catch (_: Exception) { false }

    // Removes the Android bond via the internal removeBond() method.
    private fun BluetoothDevice.removeBond(): Boolean = try {
        javaClass.getMethod("removeBond").invoke(this) as Boolean
    } catch (_: Exception) { false }
}
