package com.nw5w.graywolf

import android.Manifest
import android.annotation.SuppressLint
import android.app.Activity
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.os.PowerManager
import android.provider.Settings
import android.util.Log
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import com.nw5w.graywolf.webview.WebAppInterface
import com.nw5w.graywolf.webview.WebBridgeIds

class MainActivity : Activity() {
    private lateinit var webView: WebView
    private val mainHandler = Handler(Looper.getMainLooper())
    private var didReloadOnError = false
    private var batteryOptIntentChecked = false
    // Pending JS callback id for an in-flight BLUETOOTH_CONNECT permission
    // request. Cleared in onRequestPermissionsResult after we post the
    // window.__btResult dispatch back to the WebView.
    private var pendingBtPermCallback: String? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        webView = WebView(this).also {
            it.settings.javaScriptEnabled = true
            it.settings.domStorageEnabled = true
            // Make the WebView feel like a native app: no pinch-zoom,
            // no zoom controls, no overscroll glow, no scrollbars on
            // the chrome (the SPA renders its own).
            it.settings.setSupportZoom(false)
            it.settings.builtInZoomControls = false
            it.settings.displayZoomControls = false
            it.overScrollMode = android.view.View.OVER_SCROLL_NEVER
            it.isHorizontalScrollBarEnabled = false
            it.isVerticalScrollBarEnabled = false
            // Long-press text-select gesture also feels app-y when
            // disabled on map/control surfaces; SPA can re-enable
            // per-region with CSS user-select:text on inputs/textareas.
            it.isLongClickable = false
            it.setOnLongClickListener { true }
            it.addJavascriptInterface(
                WebAppInterface(
                    tokenProvider = { (application as GraywolfApp).bearerToken },
                    webView = it,
                    requestBtPermission = ::requestBluetoothPermission,
                ),
                "GraywolfWebInterface",
            )
            it.webViewClient = object : WebViewClient() {
                override fun onReceivedError(view: WebView, req: WebResourceRequest, err: WebResourceError) {
                    Log.w(TAG, "webview error code=${err.errorCode} desc=${err.description}")
                    if (!didReloadOnError && req.isForMainFrame) {
                        didReloadOnError = true
                        mainHandler.postDelayed({ view.reload() }, 1000)
                    }
                }
            }
        }
        setContentView(webView)
        ensurePerms()
    }

    private fun ensurePerms() {
        val needed = mutableListOf<String>()
        if (checkSelfPermission(Manifest.permission.RECORD_AUDIO) != PackageManager.PERMISSION_GRANTED) {
            needed += Manifest.permission.RECORD_AUDIO
        }
        if (checkSelfPermission(Manifest.permission.ACCESS_FINE_LOCATION) != PackageManager.PERMISSION_GRANTED) {
            needed += Manifest.permission.ACCESS_FINE_LOCATION
        }
        if (Build.VERSION.SDK_INT >= 33 &&
            checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            needed += Manifest.permission.POST_NOTIFICATIONS
        }
        if (needed.isNotEmpty()) {
            requestPermissions(needed.toTypedArray(), REQ_PERMS)
        } else {
            startEverything()
        }
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        if (requestCode == REQ_PERMS) {
            startEverything()
            return
        }
        if (requestCode == REQ_BT_PERMS) {
            val granted = grantResults.isNotEmpty() &&
                grantResults[0] == PackageManager.PERMISSION_GRANTED
            val callbackId = pendingBtPermCallback
            pendingBtPermCallback = null
            if (callbackId != null) postBtResult(callbackId, granted)
        }
    }

    /**
     * Request the BLUETOOTH_CONNECT runtime permission and report the result
     * back to the WebView via window.__btResult(callbackId, granted).
     *
     * On API <31 the permission is install-time (the legacy BLUETOOTH /
     * BLUETOOTH_ADMIN entries in the manifest cover us) so we resolve the
     * callback immediately with granted=true.
     *
     * If the permission is already granted, we likewise short-circuit.
     *
     * Otherwise we store the callbackId, fire requestPermissions(), and let
     * onRequestPermissionsResult() post the result.
     */
    fun requestBluetoothPermission(callbackId: String) {
        if (!WebBridgeIds.CALLBACK_ID_RE.matches(callbackId)) {
            Log.w(TAG, "rejected invalid bt callbackId: $callbackId")
            return
        }
        // requestPermissions() is documented to run on the main thread, and
        // pendingBtPermCallback is read on the main thread by
        // onRequestPermissionsResult. @JavascriptInterface methods are invoked
        // on the WebView binder thread, so hop to the main looper before
        // touching either. The postBtResult short-circuits already target the
        // main thread via webView.post inside postBtResult.
        mainHandler.post {
            if (Build.VERSION.SDK_INT < Build.VERSION_CODES.S) {
                // API <31: legacy BLUETOOTH / BLUETOOTH_ADMIN are install-time.
                postBtResult(callbackId, true)
                return@post
            }
            if (checkSelfPermission(Manifest.permission.BLUETOOTH_CONNECT) == PackageManager.PERMISSION_GRANTED) {
                postBtResult(callbackId, true)
                return@post
            }
            pendingBtPermCallback = callbackId
            requestPermissions(arrayOf(Manifest.permission.BLUETOOTH_CONNECT), REQ_BT_PERMS)
        }
    }

    // Dispatch the BT permission result back into the SPA. callbackId is
    // re-validated against CALLBACK_ID_RE before JS interpolation so a
    // malformed value can't escape the string literal.
    private fun postBtResult(callbackId: String, granted: Boolean) {
        if (!WebBridgeIds.CALLBACK_ID_RE.matches(callbackId)) {
            Log.w(TAG, "refusing to post bt result for invalid callbackId: $callbackId")
            return
        }
        webView.post {
            val script = "window.__btResult && window.__btResult('$callbackId', $granted)"
            Log.d(TAG, "btResult callbackId=$callbackId granted=$granted")
            webView.evaluateJavascript(script, null)
        }
    }

    private fun startEverything() {
        startForegroundService(Intent(this, GraywolfService::class.java))
        val started = System.currentTimeMillis()
        val r = object : Runnable {
            override fun run() {
                if (GraywolfService.goListenerReady) {
                    webView.loadUrl("http://127.0.0.1:8080/")
                    Log.i(TAG, "poc-b: webview_loaded")
                } else if (System.currentTimeMillis() - started < 30_000) {
                    mainHandler.postDelayed(this, 250)
                } else {
                    Log.e(TAG, "go listener never became ready")
                }
            }
        }
        mainHandler.postDelayed(r, 500)
    }

    override fun onResume() {
        super.onResume()
        if (!batteryOptIntentChecked) {
            batteryOptIntentChecked = true
            maybeRequestBatteryOptWhitelist()
        }
    }

    @SuppressLint("BatteryLife")
    private fun maybeRequestBatteryOptWhitelist() {
        if (batteryOptWhitelistRequested(this)) return
        val pm = getSystemService(PowerManager::class.java) ?: return
        if (pm.isIgnoringBatteryOptimizations(packageName)) {
            markBatteryOptWhitelistRequested(this)
            return
        }
        try {
            val intent = Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS)
                .setData(Uri.parse("package:$packageName"))
            startActivity(intent)
        } catch (t: Throwable) {
            Log.w(TAG, "battery-opt whitelist intent failed: $t")
        }
        markBatteryOptWhitelistRequested(this)
    }

    override fun onDestroy() {
        webView.destroy()
        super.onDestroy()
    }

    companion object {
        private const val TAG = "MainActivity"
        private const val REQ_PERMS = 0x101
        // Distinct request code for BLUETOOTH_CONNECT runtime permission so
        // onRequestPermissionsResult can route the result to the SPA's
        // pending callback instead of the startup-perms code path.
        private const val REQ_BT_PERMS = 0x102
        private const val PREFS_NAME = "graywolf-prefs"
        private const val PREF_BATTERY_OPT_REQUESTED = "battery_opt_whitelist_requested_v1"

        fun batteryOptWhitelistRequested(ctx: Context): Boolean =
            ctx.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
                .getBoolean(PREF_BATTERY_OPT_REQUESTED, false)

        fun markBatteryOptWhitelistRequested(ctx: Context) {
            ctx.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
                .edit().putBoolean(PREF_BATTERY_OPT_REQUESTED, true).apply()
        }
    }
}
