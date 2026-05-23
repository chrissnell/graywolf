package com.nw5w.graywolf.usb

import android.content.Context
import android.hardware.usb.UsbDevice
import android.hardware.usb.UsbManager
import org.json.JSONArray
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.mockito.kotlin.any
import org.mockito.kotlin.mock
import org.mockito.kotlin.never
import org.mockito.kotlin.verify
import org.mockito.kotlin.whenever

/**
 * Unit tests for UsbPttAdapter.enumerateForJs() and
 * UsbPttAdapter.requestPermissionFor() (spec §3.6).
 *
 * UsbPttAdapter is a Kotlin singleton. We use reflection to inject a mock
 * UsbManager and appContext so that enumeration can be exercised without a
 * running Android runtime. android.util.Log is silenced by
 * unitTests.isReturnDefaultValues = true.
 *
 * Kept separate from UsbPttAdapterTest (T7) so PTT-dispatch and JS-bridge
 * concerns don't collide.
 */
class UsbPttAdapterEnumerateForJsTest {

    private lateinit var mockUsbManager: UsbManager
    private lateinit var mockContext: Context

    /** Inject a field into the UsbPttAdapter singleton by name via reflection. */
    private fun inject(fieldName: String, value: Any?) {
        val field = UsbPttAdapter::class.java.getDeclaredField(fieldName)
        field.isAccessible = true
        field.set(UsbPttAdapter, value)
    }

    @Before
    fun setUp() {
        mockUsbManager = mock()
        mockContext = mock<Context>().also {
            whenever(it.applicationContext).thenReturn(it)
            whenever(it.packageName).thenReturn("com.nw5w.graywolf")
        }
        inject("appContext", mockContext)
        inject("usbManager", mockUsbManager)
    }

    @After
    fun tearDown() {
        // Clear the singleton back so other test classes don't inherit state.
        inject("appContext", null)
        inject("usbManager", null)
        // Null the lateinit by resetting to uninitialised state. Kotlin
        // lateinit stores null internally when uninitialised; assign null
        // back via reflection to allow re-initialisation in other suites.
    }

    // -----------------------------------------------------------------------
    // enumerateForJs — empty device list
    // -----------------------------------------------------------------------
    @Test
    fun `enumerateForJs returns empty array when no devices attached`() {
        whenever(mockUsbManager.deviceList).thenReturn(emptyMap())

        val result = JSONArray(UsbPttAdapter.enumerateForJs().toString())

        assertEquals(0, result.length())
    }

    // -----------------------------------------------------------------------
    // enumerateForJs — one entry per attached device with correct keys
    // -----------------------------------------------------------------------
    @Test
    fun `enumerateForJs returns one entry per device with all required keys`() {
        val dev1 = mockDevice(
            vendorId = 0x10C4,
            productId = 0xEA60,
            name = "Digirig Mobile",
            hasPermission = true,
        )
        val dev2 = mockDevice(
            vendorId = 0x1209,
            productId = 0x7388,
            name = "AIOC",
            hasPermission = false,
        )
        whenever(mockUsbManager.deviceList).thenReturn(
            mapOf("/dev/bus/usb/001/001" to dev1, "/dev/bus/usb/001/002" to dev2)
        )
        whenever(mockUsbManager.hasPermission(dev1)).thenReturn(true)
        whenever(mockUsbManager.hasPermission(dev2)).thenReturn(false)

        val result = JSONArray(UsbPttAdapter.enumerateForJs().toString())

        assertEquals(2, result.length())
        // Find entries by decimal vid int (order not guaranteed by HashMap).
        val entries = (0 until result.length()).map { result.getJSONObject(it) }
        val cp = entries.first { it.getInt("vid") == 0x10C4 }
        val ai = entries.first { it.getInt("vid") == 0x1209 }

        // vid/pid are decimal Ints for the JS bridge call.
        assertEquals(0xEA60, cp.getInt("pid"))
        // vid_hex/pid_hex are hex strings for display.
        assertEquals("0x10C4", cp.getString("vid_hex"))
        assertEquals("0xEA60", cp.getString("pid_hex"))
        assertEquals("Digirig Mobile", cp.getString("name"))
        assertEquals("CP2102N", cp.getString("role"))
        assertTrue(cp.getBoolean("permission_granted"))

        assertEquals(0x7388, ai.getInt("pid"))
        assertEquals("0x1209", ai.getString("vid_hex"))
        assertEquals("0x7388", ai.getString("pid_hex"))
        assertEquals("AIOC", ai.getString("role"))
        assertFalse(ai.getBoolean("permission_granted"))
    }

    // -----------------------------------------------------------------------
    // requestPermissionFor — no matching device fires cb(false) synchronously
    // -----------------------------------------------------------------------
    @Test
    fun `requestPermissionFor with no matching device fires callback false synchronously`() {
        whenever(mockUsbManager.deviceList).thenReturn(emptyMap())

        var callbackResult: Boolean? = null
        UsbPttAdapter.requestPermissionFor(vid = 0x10C4, pid = 0xEA60) { granted ->
            callbackResult = granted
        }

        assertFalse("callback must fire with false", callbackResult!!)
    }

    @Test
    fun `requestPermissionFor with no matching device does not call requestPermission`() {
        whenever(mockUsbManager.deviceList).thenReturn(emptyMap())

        UsbPttAdapter.requestPermissionFor(vid = 0x10C4, pid = 0xEA60) {}

        // requestPermission() calls usbManager.requestPermission(device, pi).
        // With no device found it should never touch usbManager.requestPermission.
        verify(mockUsbManager, never()).requestPermission(any(), any())
    }

    // -----------------------------------------------------------------------
    // requestPermissionFor — already-granted device fires cb(true) and opens
    // -----------------------------------------------------------------------
    @Test
    fun `requestPermissionFor with already-granted device fires callback true synchronously`() {
        val dev = mockDevice(
            vendorId = 0x10C4,
            productId = 0xEA60,
            name = "Digirig Mobile",
            hasPermission = true,
        )
        whenever(mockUsbManager.deviceList).thenReturn(
            mapOf("/dev/bus/usb/001/001" to dev)
        )
        whenever(mockUsbManager.hasPermission(dev)).thenReturn(true)
        // openDevice can return null; tryOpen handles the null case gracefully.
        whenever(mockUsbManager.openDevice(dev)).thenReturn(null)

        var callbackResult: Boolean? = null
        UsbPttAdapter.requestPermissionFor(vid = 0x10C4, pid = 0xEA60) { granted ->
            callbackResult = granted
        }

        assertTrue("callback must fire with true when already granted", callbackResult!!)
    }

    // -----------------------------------------------------------------------
    // Helpers
    // -----------------------------------------------------------------------
    private fun mockDevice(
        vendorId: Int,
        productId: Int,
        name: String,
        hasPermission: Boolean,
    ): UsbDevice = mock<UsbDevice>().also { d ->
        whenever(d.vendorId).thenReturn(vendorId)
        whenever(d.productId).thenReturn(productId)
        whenever(d.productName).thenReturn(name)
        whenever(d.deviceName).thenReturn("/dev/bus/usb/001/$vendorId")
        // interfaceCount needed by classify(); return 0 → role=UNKNOWN
        // unless we specify vid/pid that match a known device.
        whenever(d.interfaceCount).thenReturn(0)
    }
}
