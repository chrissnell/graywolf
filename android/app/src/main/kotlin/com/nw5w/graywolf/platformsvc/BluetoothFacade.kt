package com.nw5w.graywolf.platformsvc

import android.bluetooth.BluetoothAdapter
import android.bluetooth.BluetoothDevice
import android.bluetooth.BluetoothSocket
import android.util.Log
import java.io.IOException
import java.lang.reflect.InvocationTargetException
import java.util.UUID

/** Simple bonded-device record passed up to Go. */
data class BondedDevice(val mac: String, val name: String)

/** Thin interface around BluetoothAdapter to allow unit testing. */
interface BluetoothFacade {
    suspend fun bondedDevices(): List<BondedDevice>
    /** Throws on connect failure. MUST be called from a worker thread. */
    suspend fun connectRfcomm(mac: String): BluetoothSocket
    fun isBonded(mac: String): Boolean
}

/** Production implementation backed by the system BluetoothAdapter. */
class SystemBluetoothFacade(
    private val adapter: BluetoothAdapter?,
) : BluetoothFacade {
    companion object {
        private const val TAG = "SystemBluetoothFacade"
        val SPP_UUID: UUID = UUID.fromString("00001101-0000-1000-8000-00805F9B34FB")
    }

    override suspend fun bondedDevices(): List<BondedDevice> {
        val a = adapter ?: return emptyList()
        return a.bondedDevices.orEmpty().map {
            BondedDevice(mac = it.address, name = it.name ?: it.address)
        }
    }

    override fun isBonded(mac: String): Boolean {
        val a = adapter ?: return false
        return a.bondedDevices.orEmpty().any { it.address == mac }
    }

    override suspend fun connectRfcomm(mac: String): BluetoothSocket {
        val a = adapter ?: error("Bluetooth not available on this device")
        val device: BluetoothDevice = a.getRemoteDevice(mac)
        // An inquiry scan in progress slows or breaks RFCOMM connects.
        // cancelDiscovery needs BLUETOOTH_SCAN on API 31+, which we don't
        // request, so this is best-effort.
        try { a.cancelDiscovery() } catch (_: SecurityException) {}
        // Radios differ in how they expose a KISS TNC over SPP. Try the
        // standard secure SDP lookup first, then the insecure (no MITM)
        // variant, then a direct connect to RFCOMM channel 1 that skips SDP
        // entirely. Every failure is collected so the SerialOpenAck error
        // (which lands in the Go log) shows what each attempt hit.
        val attempts = listOf<Pair<String, () -> BluetoothSocket>>(
            "secure-spp" to { device.createRfcommSocketToServiceRecord(SPP_UUID) },
            "insecure-spp" to { device.createInsecureRfcommSocketToServiceRecord(SPP_UUID) },
            "channel-1" to {
                device.javaClass
                    .getMethod("createRfcommSocket", Int::class.javaPrimitiveType)
                    .invoke(device, 1) as BluetoothSocket
            },
        )
        val failures = mutableListOf<String>()
        for ((name, create) in attempts) {
            var socket: BluetoothSocket? = null
            try {
                socket = create()
                socket.connect() // blocking; caller MUST be on a worker thread
                Log.i(TAG, "RFCOMM connected to $mac via $name")
                return socket
            } catch (sec: SecurityException) {
                try { socket?.close() } catch (_: IOException) {}
                throw sec
            } catch (e: Exception) {
                try { socket?.close() } catch (_: IOException) {}
                val cause = (e as? InvocationTargetException)?.targetException ?: e
                Log.w(TAG, "RFCOMM connect to $mac via $name failed", cause)
                failures += "$name: ${cause.javaClass.simpleName}: ${cause.message}"
            }
        }
        throw IOException(failures.joinToString("; "))
    }
}

/** Test double. */
class FakeBluetoothFacade(
    private val bonded: List<BondedDevice>,
) : BluetoothFacade {
    override suspend fun bondedDevices(): List<BondedDevice> = bonded
    override fun isBonded(mac: String): Boolean = bonded.any { it.mac == mac }
    override suspend fun connectRfcomm(mac: String): BluetoothSocket =
        error("FakeBluetoothFacade does not support real RFCOMM; override for integration tests")
}
