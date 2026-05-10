package com.nw5w.graywolf.webview

import android.webkit.JavascriptInterface

/**
 * The single JS bridge exposed to the production Svelte SPA. Phase 3
 * surface is intentionally minimal: the SPA reads the per-launch
 * bearer token and adds it to every fetch / WebSocket call. POC-C
 * TX-test and POC-D PTT trigger methods are gone; phase 5 rewires
 * PTT through the proto path.
 */
class WebAppInterface(
    private val tokenProvider: () -> String,
) {
    @JavascriptInterface
    fun getBearerToken(): String = tokenProvider()
}
