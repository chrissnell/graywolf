package com.nw5w.graywolf.usb

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class UsbDeviceArbiterTest {
    @Test fun claimAndRelease_togglesIsClaimed() {
        val name = "/dev/bus/usb/001/004"
        UsbDeviceArbiter.release(name) // start clean
        assertFalse(UsbDeviceArbiter.isClaimed(name))
        UsbDeviceArbiter.claim(name)
        assertTrue(UsbDeviceArbiter.isClaimed(name))
        UsbDeviceArbiter.release(name)
        assertFalse(UsbDeviceArbiter.isClaimed(name))
    }

    @Test fun claimIsIdempotent() {
        val name = "/dev/bus/usb/001/005"
        UsbDeviceArbiter.claim(name)
        UsbDeviceArbiter.claim(name)
        assertTrue(UsbDeviceArbiter.isClaimed(name))
        UsbDeviceArbiter.release(name)
        assertFalse(UsbDeviceArbiter.isClaimed(name))
    }
}
