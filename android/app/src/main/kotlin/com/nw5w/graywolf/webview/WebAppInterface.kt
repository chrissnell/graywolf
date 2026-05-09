package com.nw5w.graywolf.webview

import android.util.Log
import android.webkit.JavascriptInterface
import com.nw5w.graywolf.audio.AudioTxTest
import com.nw5w.graywolf.jni.ModemBridge
import com.nw5w.graywolf.usb.UsbPttAdapter
import kotlin.concurrent.thread

class WebAppInterface(
    private val tokenProvider: () -> String,
) {
    @JavascriptInterface
    fun getBearerToken(): String = tokenProvider()

    /**
     * POC-C TX-test trigger. Called from the WebView page's button handler.
     * Builds the canned PCM16 buffer in Rust and plays it via AudioTrack on
     * a background thread so the WebView doesn't block.
     */
    @JavascriptInterface
    fun fireTxTest() {
        thread(name = "tx-test", isDaemon = true) {
            try {
                val samples = ModemBridge.modemBuildTestFrame()
                Log.i(TAG, "poc-c: tx_test_fire samples=${samples.size}")
                val ok = AudioTxTest.fireOnce(samples)
                Log.i(TAG, "poc-c: tx_test_done ok=$ok")
            } catch (t: Throwable) {
                Log.e(TAG, "poc-c: tx_test_failed: $t")
            }
        }
    }

    /** POC-D: returns the USB PTT adapter status snapshot as a JSON string. */
    @JavascriptInterface
    fun pttStatusJson(): String = UsbPttAdapter.status().toString()

    @JavascriptInterface
    fun keyCp2102nRts(): Boolean = UsbPttAdapter.keyCp2102nRts()

    @JavascriptInterface
    fun unkeyCp2102nRts(): Boolean = UsbPttAdapter.unkeyCp2102nRts()

    @JavascriptInterface
    fun keyCm108Hid(): Boolean = UsbPttAdapter.keyCm108Hid()

    @JavascriptInterface
    fun unkeyCm108Hid(): Boolean = UsbPttAdapter.unkeyCm108Hid()

    @JavascriptInterface
    fun setCm108Bit(bit: Int): Boolean = UsbPttAdapter.setCm108Bit(bit)

    companion object { private const val TAG = "WebAppInterface" }
}
