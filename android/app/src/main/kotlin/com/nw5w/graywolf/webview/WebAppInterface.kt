package com.nw5w.graywolf.webview

import android.util.Log
import android.webkit.JavascriptInterface
import android.webkit.WebView
import com.nw5w.graywolf.usb.UsbPttAdapter

/**
 * The single JS bridge exposed to the production Svelte SPA.
 *
 * Phase 4b surface (spec §3.6):
 *   getBearerToken()            — per-launch auth token
 *   listUsbDevices()            — JSON array of attached USB devices + permission state
 *   requestUsbPermission(...)   — async system permission dialog; result via window.__usbResult
 *
 * POC-C TX-test and non-USB PTT trigger methods remain absent; phase 5 rewires
 * PTT through the proto path.
 */
class WebAppInterface(
    private val tokenProvider: () -> String,
    private val webView: WebView,
    private val adapter: UsbPttAdapter = UsbPttAdapter,
) {
    @JavascriptInterface
    fun getBearerToken(): String = tokenProvider()

    /**
     * Snapshot of attached USB devices for the SPA channel-config status row.
     * Returns a JSON array string:
     *   [{"vid":"0x10C4","pid":"0xEA60","name":"Digirig CP2102N",
     *     "role":"CP2102N","permission_granted":true}, ...]
     *
     * `role` is one of "CP2102N", "CM108", "AIOC", or "UNKNOWN".
     * `permission_granted` reflects UsbManager.hasPermission, not whether
     * the device handle is open yet.
     */
    @JavascriptInterface
    fun listUsbDevices(): String = adapter.enumerateForJs().toString()

    /**
     * Request user permission to access the device matching (vid, pid).
     * vid/pid arrive as decimal ints from the JS side.
     *
     * If no matching device is attached the JS callback fires immediately
     * with granted=false. Otherwise the call is async; the callback fires
     * when the user taps Allow/Deny, or never if they dismiss the dialog.
     *
     * Result is posted back into the WebView via:
     *   window.__usbResult(callbackId, granted: boolean)
     *
     * callbackId is validated to match ^[A-Za-z0-9_-]+$ before JS interpolation.
     * Invalid callbackId values are rejected to prevent string-escape attacks.
     */
    @JavascriptInterface
    fun requestUsbPermission(vid: Int, pid: Int, callbackId: String) {
        if (!CALLBACK_ID_RE.matches(callbackId)) {
            Log.w(TAG, "rejected invalid callbackId: $callbackId")
            return
        }
        adapter.requestPermissionFor(vid, pid) { granted ->
            webView.post {
                val script = "window.__usbResult && window.__usbResult('$callbackId', $granted)"
                Log.d(TAG, "usbResult callbackId=$callbackId granted=$granted")
                webView.evaluateJavascript(script, null)
            }
        }
    }

    companion object {
        private const val TAG = "WebAppInterface"
        private val CALLBACK_ID_RE = Regex("^[A-Za-z0-9_-]+$")
    }
}
