package com.nw5w.graywolf.platformsvc

import android.bluetooth.BluetoothAdapter
import android.bluetooth.BluetoothDevice
import android.bluetooth.BluetoothSocket
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
        val socket = device.createRfcommSocketToServiceRecord(SPP_UUID)
        socket.connect() // blocking; caller MUST be on a worker thread
        return socket
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
