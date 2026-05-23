package com.nw5w.graywolf.usb

import android.hardware.usb.UsbDevice
import android.hardware.usb.UsbDeviceConnection
import com.hoho.android.usbserial.driver.UsbSerialPort
import org.junit.After
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.mockito.kotlin.any
import org.mockito.kotlin.argThat
import org.mockito.kotlin.eq
import org.mockito.kotlin.mock
import org.mockito.kotlin.verify
import org.mockito.kotlin.whenever

/**
 * Unit tests for UsbPttAdapter.pttSet dispatcher (spec §6.1).
 *
 * UsbPttAdapter is a Kotlin singleton with internal handle slots. Tests
 * inject mocked handles directly into those slots (no Robolectric needed).
 * android.util.Log is silenced by unitTests.isReturnDefaultValues = true.
 *
 * Teardown nulls all three slots after each test to prevent state bleed.
 */
class UsbPttAdapterTest {

    private fun mockDevice(): UsbDevice = mock<UsbDevice>().also {
        whenever(it.deviceName).thenReturn("/dev/bus/usb/001/001")
    }

    @After
    fun resetHandles() {
        UsbPttAdapter.cp2102n = null
        UsbPttAdapter.cm108 = null
        UsbPttAdapter.aioc = null
    }

    // -----------------------------------------------------------------------
    // §6.1: CP2102N_RTS — pttSet(1, true) delegates to setRts(true)
    // -----------------------------------------------------------------------
    @Test fun `pttSet CP2102N_RTS keyed delegates to setRts and returns true`() {
        val port = mock<UsbSerialPort>()
        UsbPttAdapter.cp2102n = UsbPttAdapter.Cp2102nHandle(mockDevice(), port, mock<UsbDeviceConnection>())

        val result = UsbPttAdapter.pttSet(PttMethodConsts.PTT_METHOD_CP2102N_RTS, true)

        assertTrue(result)
        verify(port).rts = true
    }

    @Test fun `pttSet CP2102N_RTS unkeyed delegates to setRts and returns true`() {
        val port = mock<UsbSerialPort>()
        UsbPttAdapter.cp2102n = UsbPttAdapter.Cp2102nHandle(mockDevice(), port, mock<UsbDeviceConnection>())

        val result = UsbPttAdapter.pttSet(PttMethodConsts.PTT_METHOD_CP2102N_RTS, false)

        assertTrue(result)
        verify(port).rts = false
    }

    @Test fun `pttSet CP2102N_RTS returns false when transport not open`() {
        UsbPttAdapter.cp2102n = null

        val result = UsbPttAdapter.pttSet(PttMethodConsts.PTT_METHOD_CP2102N_RTS, true)

        assertFalse(result)
    }

    // -----------------------------------------------------------------------
    // §6.1: AIOC_CDC_DTR — pttSet(3, true) delegates to setAiocRts(true)
    // Per feedback_aioc_ptt_cdc_acm_dtr: AIOC firmware ≥1.2.0 requires
    // DTR=1 AND RTS=0 to assert PTT. setAiocRts holds RTS=0 in both states.
    // -----------------------------------------------------------------------
    @Test fun `pttSet AIOC_CDC_DTR keyed drives DTR=true and RTS=false`() {
        val port = mock<UsbSerialPort>()
        UsbPttAdapter.aioc = UsbPttAdapter.AiocHandle(mockDevice(), port, mock<UsbDeviceConnection>())

        val result = UsbPttAdapter.pttSet(PttMethodConsts.PTT_METHOD_AIOC_CDC_DTR, true)

        assertTrue(result)
        // Order per setAiocRts: rts=false first, then dtr=state
        verify(port).rts = false
        verify(port).dtr = true
    }

    @Test fun `pttSet AIOC_CDC_DTR unkeyed drives DTR=false and RTS=false`() {
        val port = mock<UsbSerialPort>()
        UsbPttAdapter.aioc = UsbPttAdapter.AiocHandle(mockDevice(), port, mock<UsbDeviceConnection>())

        val result = UsbPttAdapter.pttSet(PttMethodConsts.PTT_METHOD_AIOC_CDC_DTR, false)

        assertTrue(result)
        verify(port).rts = false
        verify(port).dtr = false
    }

    @Test fun `pttSet AIOC_CDC_DTR returns false when transport not open`() {
        UsbPttAdapter.aioc = null

        val result = UsbPttAdapter.pttSet(PttMethodConsts.PTT_METHOD_AIOC_CDC_DTR, true)

        assertFalse(result)
    }

    // -----------------------------------------------------------------------
    // §6.1: CM108_HID — pttSet(2, true) delegates to setHidGpio(true)
    // Report layout: [0x00, value, mask, 0x00] where mask = 1 shl (pin-1),
    // default pin=3 → mask=0x04. state=true → value=mask; state=false → 0x00.
    // -----------------------------------------------------------------------
    @Test fun `pttSet CM108_HID keyed sends correct HID report and returns true`() {
        val conn = mock<UsbDeviceConnection>()
        // Default cm108GpioBit=3 → mask=0x04, keyed value=0x04
        // controlTransfer returns report.size (4) to signal success.
        whenever(conn.controlTransfer(any(), any(), any(), any(), any(), any(), any()))
            .thenReturn(4)
        UsbPttAdapter.cm108 = UsbPttAdapter.Cm108Handle(mockDevice(), conn, hidIface = 3)

        val result = UsbPttAdapter.pttSet(PttMethodConsts.PTT_METHOD_CM108_HID, true)

        assertTrue(result)
        // Verify the report byte layout: [0x00, value=0x04, mask=0x04, 0x00]
        // ByteArray.equals() is reference-based; use argThat for content check.
        verify(conn).controlTransfer(
            eq(0x21), eq(0x09), eq(0x0200), eq(3),
            argThat { contentEquals(byteArrayOf(0x00, 0x04, 0x04, 0x00)) },
            eq(4), eq(200)
        )
    }

    @Test fun `pttSet CM108_HID unkeyed sends value=0x00 in report`() {
        val conn = mock<UsbDeviceConnection>()
        // Unkeyed: value byte is 0x00, mask stays 0x04
        whenever(conn.controlTransfer(any(), any(), any(), any(), any(), any(), any()))
            .thenReturn(4)
        UsbPttAdapter.cm108 = UsbPttAdapter.Cm108Handle(mockDevice(), conn, hidIface = 3)

        val result = UsbPttAdapter.pttSet(PttMethodConsts.PTT_METHOD_CM108_HID, false)

        assertTrue(result)
        verify(conn).controlTransfer(
            eq(0x21), eq(0x09), eq(0x0200), eq(3),
            argThat { contentEquals(byteArrayOf(0x00, 0x00, 0x04, 0x00)) },
            eq(4), eq(200)
        )
    }

    @Test fun `pttSet CM108_HID returns false when transport not open`() {
        UsbPttAdapter.cm108 = null

        val result = UsbPttAdapter.pttSet(PttMethodConsts.PTT_METHOD_CM108_HID, true)

        assertFalse(result)
    }

    // -----------------------------------------------------------------------
    // §6.1: VOX — no PTT wire; audio path drives PTT; always returns true
    // -----------------------------------------------------------------------
    @Test fun `pttSet VOX always returns true regardless of keyed state`() {
        // No handles needed — VOX is a no-op transport
        assertTrue(UsbPttAdapter.pttSet(PttMethodConsts.PTT_METHOD_VOX, true))
        assertTrue(UsbPttAdapter.pttSet(PttMethodConsts.PTT_METHOD_VOX, false))
    }

    // -----------------------------------------------------------------------
    // §6.1: Unknown method — dispatcher logs WARN and returns false
    // -----------------------------------------------------------------------
    @Test fun `pttSet unknown method int returns false`() {
        val result = UsbPttAdapter.pttSet(99, true)

        assertFalse(result)
    }

    // -----------------------------------------------------------------------
    // §6.1: PTT_METHOD_UNKNOWN (0) is also treated as unknown → false
    // -----------------------------------------------------------------------
    @Test fun `pttSet PTT_METHOD_UNKNOWN returns false`() {
        val result = UsbPttAdapter.pttSet(PttMethodConsts.PTT_METHOD_UNKNOWN, true)

        assertFalse(result)
    }
}
