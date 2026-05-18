package com.nw5w.graywolf.webview

import android.webkit.WebView
import com.nw5w.graywolf.usb.UsbPttAdapter
import org.json.JSONArray
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.mockito.kotlin.any
import org.mockito.kotlin.argumentCaptor
import org.mockito.kotlin.doAnswer
import org.mockito.kotlin.mock
import org.mockito.kotlin.never
import org.mockito.kotlin.verify
import org.mockito.kotlin.whenever

/**
 * Unit tests for WebAppInterface (spec §3.6).
 *
 * WebView is mocked; UsbPttAdapter is mocked via the constructor default
 * override. android.util.Log is silenced by unitTests.isReturnDefaultValues.
 */
class WebAppInterfaceTest {

    private fun mockWebView(): WebView = mock<WebView>().also { wv ->
        // Make WebView.post() execute the Runnable immediately so callbacks
        // fire synchronously in tests.
        whenever(wv.post(any())).doAnswer { inv ->
            (inv.arguments[0] as Runnable).run()
            true
        }
    }

    private fun iface(
        webView: WebView = mockWebView(),
        adapter: UsbPttAdapter = UsbPttAdapter,
        token: String = "tok",
    ) = WebAppInterface(tokenProvider = { token }, webView = webView, adapter = adapter)

    // -----------------------------------------------------------------------
    // getBearerToken — unchanged from phase 3
    // -----------------------------------------------------------------------
    @Test
    fun `getBearerToken returns provided value`() {
        assertEquals("abc-123", iface(token = "abc-123").getBearerToken())
    }

    // -----------------------------------------------------------------------
    // listUsbDevices — delegates to adapter.enumerateForJs()
    // -----------------------------------------------------------------------
    @Test
    fun `listUsbDevices returns JSON array with expected keys from adapter`() {
        val fakeArray = JSONArray().apply {
            put(
                org.json.JSONObject().apply {
                    put("vid", "0x10C4")
                    put("pid", "0xEA60")
                    put("name", "Digirig CP2102N")
                    put("role", "CP2102N")
                    put("permission_granted", true)
                }
            )
            put(
                org.json.JSONObject().apply {
                    put("vid", "0x1209")
                    put("pid", "0x7388")
                    put("name", "AIOC")
                    put("role", "AIOC")
                    put("permission_granted", false)
                }
            )
        }
        val adapter = mock<UsbPttAdapter>().also {
            whenever(it.enumerateForJs()).thenReturn(fakeArray)
        }

        val result = JSONArray(iface(adapter = adapter).listUsbDevices())

        assertEquals(2, result.length())

        val first = result.getJSONObject(0)
        assertTrue(first.has("vid"))
        assertTrue(first.has("pid"))
        assertTrue(first.has("name"))
        assertTrue(first.has("role"))
        assertTrue(first.has("permission_granted"))
        assertEquals("0x10C4", first.getString("vid"))
        assertEquals("CP2102N", first.getString("role"))
        assertTrue(first.getBoolean("permission_granted"))

        val second = result.getJSONObject(1)
        assertEquals("0x1209", second.getString("vid"))
        assertEquals("AIOC", second.getString("role"))
        assertEquals(false, second.getBoolean("permission_granted"))
    }

    // -----------------------------------------------------------------------
    // requestUsbPermission — no matching device fires JS callback with false
    // -----------------------------------------------------------------------
    @Test
    fun `requestUsbPermission with no matching device posts callback with false`() {
        val wv = mockWebView()
        val adapter = mock<UsbPttAdapter>().also { ad ->
            // Simulate adapter immediately calling onResult(false)
            whenever(ad.requestPermissionFor(any(), any(), any())).doAnswer { inv ->
                @Suppress("UNCHECKED_CAST")
                (inv.arguments[2] as (Boolean) -> Unit)(false)
                Unit
            }
        }

        iface(webView = wv, adapter = adapter).requestUsbPermission(
            vid = 0x10C4,
            pid = 0xEA60,
            callbackId = "cb-id",
        )

        val scriptCaptor = argumentCaptor<String>()
        verify(wv).evaluateJavascript(scriptCaptor.capture(), any())
        val script = scriptCaptor.firstValue
        assertTrue("script must contain callbackId", script.contains("cb-id"))
        assertTrue("script must contain false", script.contains("false"))
    }

    @Test
    fun `requestUsbPermission with granted=true posts callback with true`() {
        val wv = mockWebView()
        val adapter = mock<UsbPttAdapter>().also { ad ->
            whenever(ad.requestPermissionFor(any(), any(), any())).doAnswer { inv ->
                @Suppress("UNCHECKED_CAST")
                (inv.arguments[2] as (Boolean) -> Unit)(true)
                Unit
            }
        }

        iface(webView = wv, adapter = adapter).requestUsbPermission(
            vid = 0x10C4,
            pid = 0xEA60,
            callbackId = "cb-id",
        )

        val scriptCaptor = argumentCaptor<String>()
        verify(wv).evaluateJavascript(scriptCaptor.capture(), any())
        val script = scriptCaptor.firstValue
        assertTrue("script must contain callbackId", script.contains("cb-id"))
        assertTrue("script must contain true", script.contains("true"))
    }

    @Test
    fun `requestUsbPermission with malformed callbackId rejects and does not call adapter`() {
        val wv = mockWebView()
        val adapter = mock<UsbPttAdapter>()

        iface(webView = wv, adapter = adapter).requestUsbPermission(
            vid = 0x10C4,
            pid = 0xEA60,
            callbackId = "foo'); alert(1)",
        )

        verify(adapter, never()).requestPermissionFor(any(), any(), any())
        verify(wv, never()).evaluateJavascript(any(), any())
    }

    // -----------------------------------------------------------------------
    // Phase-3 regression: no raw PTT methods on the public surface
    // -----------------------------------------------------------------------
    @Test
    fun `pttMethods are not exposed`() {
        val wv = mockWebView()
        val adapter = mock<UsbPttAdapter>()
        val methods = iface(webView = wv, adapter = adapter)::class.java
            .declaredMethods
            .map { it.name }
            .toSet()
        val forbidden = setOf(
            "fireTxTest",
            "pttStatusJson",
            "keyCp2102nRts", "unkeyCp2102nRts",
            "keyCm108Hid", "unkeyCm108Hid",
            "setCm108Bit",
            "keyAiocCdcRts", "unkeyAiocCdcRts",
        )
        assertEquals(emptySet<String>(), methods.intersect(forbidden))
    }
}
