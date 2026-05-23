package com.nw5w.graywolf.usb

import java.util.concurrent.ConcurrentHashMap

/**
 * Tracks USB devices (by Android deviceName, e.g. "/dev/bus/usb/001/004")
 * claimed by a non-PTT subsystem — currently the KISS USB-serial adapter.
 *
 * UsbPttAdapter consults [isClaimed] before opening any device, so a serial
 * KISS TNC the operator configured never gets grabbed by the PTT auto-open
 * path. The claim is process-global because both the PTT adapter and the
 * serial adapter are singletons over the same USB bus; deviceName is stable
 * for the lifetime of an attachment.
 */
object UsbDeviceArbiter {
    private val claimed = ConcurrentHashMap.newKeySet<String>()

    fun claim(deviceName: String) {
        claimed.add(deviceName)
    }

    fun release(deviceName: String) {
        claimed.remove(deviceName)
    }

    fun isClaimed(deviceName: String): Boolean = deviceName in claimed
}
