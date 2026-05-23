package com.nw5w.graywolf.platformsvc

import android.hardware.usb.UsbDevice
import android.hardware.usb.UsbDeviceConnection
import android.hardware.usb.UsbManager
import android.util.Log
import com.hoho.android.usbserial.driver.UsbSerialPort
import com.hoho.android.usbserial.driver.UsbSerialProber
import com.nw5w.graywolf.usb.UsbDeviceArbiter
import com.nw5w.graywolf.usb.UsbPttAdapter

/** Attached serial-capable USB device, surfaced to the device picker. */
data class UsbSerialDeviceInfo(
    val vidPid: String,        // lowercase hex "vid:pid", e.g. "2341:0043"
    val product: String,
    val manufacturer: String,
    val hasPermission: Boolean,
)

/**
 * Reasons handleSerialOpen can reject an open, carried back to the Go side as
 * the SerialOpenAck.error / SerialError.code string. Values match the design's
 * error table: not_found | permission_denied | unsupported | io_error.
 */
class UsbSerialOpenException(val code: String, message: String? = null) :
    Exception(message ?: code)

/** An open USB serial port. All calls run on a worker thread. */
interface UsbSerialSession {
    /** Android deviceName ("/dev/bus/usb/001/004") for detach matching. */
    val deviceName: String
    /** Blocking read; returns bytes read (>=0) or <0 on EOF/closed. */
    fun read(buf: ByteArray, timeoutMs: Int): Int
    fun write(bytes: ByteArray, timeoutMs: Int)
    fun close()
}

/** Thin interface around UsbManager + usb-serial-for-android for testing. */
interface UsbSerialFacade {
    /** Serial-capable attached devices (probeable by the default prober). */
    fun available(): List<UsbSerialDeviceInfo>
    /**
     * Open the attached device matching vidPid at baud. Throws
     * UsbSerialOpenException(code) on not_found / permission_denied /
     * unsupported / io_error. MUST be called from a worker thread.
     */
    fun open(vidPid: String, baud: Int): UsbSerialSession
}

private const val TAG = "UsbSerialFacade"

/** Parse "vid:pid" hex into a (vendorId, productId) decimal pair, or null. */
internal fun parseVidPid(s: String): Pair<Int, Int>? {
    val parts = s.split(":")
    if (parts.size != 2) return null
    val vid = parts[0].toIntOrNull(16) ?: return null
    val pid = parts[1].toIntOrNull(16) ?: return null
    return vid to pid
}

/** Format vendorId/productId as lowercase "vid:pid" hex. */
internal fun formatVidPid(vid: Int, pid: Int): String =
    "%04x:%04x".format(vid, pid)

/** Production facade backed by the system UsbManager + mik3y prober. */
class SystemUsbSerialFacade(
    private val usbManager: UsbManager?,
) : UsbSerialFacade {

    override fun available(): List<UsbSerialDeviceInfo> {
        val mgr = usbManager ?: return emptyList()
        val prober = UsbSerialProber.getDefaultProber()
        return mgr.deviceList.values.mapNotNull { dev ->
            if (prober.probeDevice(dev) == null) return@mapNotNull null
            UsbSerialDeviceInfo(
                vidPid = formatVidPid(dev.vendorId, dev.productId),
                product = dev.productName ?: "",
                manufacturer = dev.manufacturerName ?: "",
                hasPermission = mgr.hasPermission(dev),
            )
        }
    }

    override fun open(vidPid: String, baud: Int): UsbSerialSession {
        val mgr = usbManager ?: throw UsbSerialOpenException("io_error", "no UsbManager")
        val (vid, pid) = parseVidPid(vidPid)
            ?: throw UsbSerialOpenException("not_found", "bad vid:pid $vidPid")
        val dev: UsbDevice = mgr.deviceList.values
            .firstOrNull { it.vendorId == vid && it.productId == pid }
            ?: throw UsbSerialOpenException("not_found", vidPid)

        if (!mgr.hasPermission(dev)) {
            throw UsbSerialOpenException("permission_denied", dev.deviceName)
        }
        val driver = UsbSerialProber.getDefaultProber().probeDevice(dev)
            ?: throw UsbSerialOpenException("unsupported", dev.deviceName)

        // Take ownership: claim so PTT auto-open skips this device, and evict
        // any PTT handle already holding it (e.g. a CP210x grabbed on hotplug).
        UsbDeviceArbiter.claim(dev.deviceName)
        UsbPttAdapter.evictDevice(dev)

        val conn: UsbDeviceConnection = mgr.openDevice(dev)
            ?: run {
                UsbDeviceArbiter.release(dev.deviceName)
                throw UsbSerialOpenException("io_error", "openDevice returned null")
            }
        val port = driver.ports.firstOrNull()
            ?: run {
                conn.close()
                UsbDeviceArbiter.release(dev.deviceName)
                throw UsbSerialOpenException("unsupported", "driver returned 0 ports")
            }
        try {
            port.open(conn)
            // 8N1, no flow control (mik3y defaults flow control off).
            port.setParameters(baud, 8, UsbSerialPort.STOPBITS_1, UsbSerialPort.PARITY_NONE)
        } catch (t: Throwable) {
            try { port.close() } catch (_: Throwable) {}
            conn.close()
            UsbDeviceArbiter.release(dev.deviceName)
            throw UsbSerialOpenException("io_error", t.message)
        }
        Log.i(TAG, "opened USB serial ${dev.deviceName} ($vidPid) @ $baud")
        return SystemUsbSerialSession(dev.deviceName, port, conn)
    }
}

private class SystemUsbSerialSession(
    override val deviceName: String,
    private val port: UsbSerialPort,
    private val conn: UsbDeviceConnection,
) : UsbSerialSession {
    override fun read(buf: ByteArray, timeoutMs: Int): Int = port.read(buf, timeoutMs)
    override fun write(bytes: ByteArray, timeoutMs: Int) = port.write(bytes, timeoutMs)
    override fun close() {
        try { port.close() } catch (_: Throwable) {}
        try { conn.close() } catch (_: Throwable) {}
        UsbDeviceArbiter.release(deviceName)
    }
}

/** Test double — no real USB. */
class FakeUsbSerialFacade(
    private val devices: List<UsbSerialDeviceInfo>,
    private val openResult: (vidPid: String, baud: Int) -> UsbSerialSession,
) : UsbSerialFacade {
    override fun available(): List<UsbSerialDeviceInfo> = devices
    override fun open(vidPid: String, baud: Int): UsbSerialSession = openResult(vidPid, baud)
}
