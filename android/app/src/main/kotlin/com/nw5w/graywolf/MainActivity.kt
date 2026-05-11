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

class MainActivity : Activity() {
    private lateinit var webView: WebView
    private val mainHandler = Handler(Looper.getMainLooper())
    private var didReloadOnError = false
    private var batteryOptIntentChecked = false

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
                WebAppInterface { (application as GraywolfApp).bearerToken },
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
        if (requestCode == REQ_PERMS) startEverything()
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
