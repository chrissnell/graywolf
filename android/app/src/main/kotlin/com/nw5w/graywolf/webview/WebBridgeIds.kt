package com.nw5w.graywolf.webview

/**
 * Shared identifier validation for the WebView <-> native JS bridge.
 *
 * Any callbackId or other token that gets interpolated into JS executed by
 * webView.evaluateJavascript(...) MUST match CALLBACK_ID_RE first. Keeping
 * the regex in one place prevents drift between MainActivity and
 * WebAppInterface, where divergent rules would silently break the
 * defense-in-depth check against string-escape attacks.
 */
internal object WebBridgeIds {
    val CALLBACK_ID_RE = Regex("^[A-Za-z0-9_-]+$")
}
