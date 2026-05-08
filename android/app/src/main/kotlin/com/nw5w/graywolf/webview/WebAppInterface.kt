package com.nw5w.graywolf.webview

import android.webkit.JavascriptInterface

class WebAppInterface(private val tokenProvider: () -> String) {
    @JavascriptInterface
    fun getBearerToken(): String = tokenProvider()
}
