package com.nw5w.graywolf

import android.Manifest
import android.annotation.SuppressLint
import android.app.Activity
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.hardware.usb.UsbManager
import android.net.LocalSocket
import android.net.LocalSocketAddress
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.os.PowerManager
import android.provider.Settings
import android.util.Log
import com.nw5w.graywolf.usb.UsbPttAdapter
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import com.nw5w.graywolf.webview.WebAppInterface
import com.nw5w.graywolf.webview.WebBridgeIds
import java.io.IOException

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
        // If the system auto-relaunched us via USB_DEVICE_ATTACHED right after a
        // deliberate swipe-stop, that "attach" is the radio's USB interfaces
        // (CP2102N/C-Media) re-enumerating when our process released them during
        // teardown -- not a genuine plug-in. Honor the user's intent to stop:
        // finish without starting the service/backend. A launcher tap (action
        // MAIN) or a genuine re-plug after the window is NOT suppressed.
        if (intent?.action == UsbManager.ACTION_USB_DEVICE_ATTACHED &&
            wasRecentlyStoppedByUser(this)) {
            Log.i(TAG, "ignoring USB-attach relaunch within stop window; staying stopped")
            finish()
            return
        }
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
        // We're committing to running, so clear any prior deliberate-stop marker;
        // future USB attaches should launch normally.
        clearUserStopped(this)
        // Wait for any previous instance to fully exit before starting a new
        // backend. A live predecessor still answers on the platformsvc socket;
        // starting now would collide on the bind and (historically) crash-loop,
        // churning the USB bus. The probe blocks, so it runs on a background
        // thread; UI updates post back to the main thread.
        waitForPredecessorThenStart()
    }

    // Background-threaded probe of the platformsvc abstract socket. While a
    // predecessor answers, show the waiting page and re-probe every
    // PROBE_STEP_MS; once it stops answering (or PROBE_TIMEOUT_MS elapses)
    // start the foreground service and begin the readiness poll.
    private fun waitForPredecessorThenStart() {
        val socketName = GraywolfService.platformSocketName(this)
        Thread({
            val deadline = System.currentTimeMillis() + PROBE_TIMEOUT_MS
            var shownWaiting = false
            while (predecessorAlive(socketName) && System.currentTimeMillis() < deadline) {
                if (!shownWaiting) {
                    shownWaiting = true
                    mainHandler.post { showWaitingPage() }
                }
                try {
                    Thread.sleep(PROBE_STEP_MS)
                } catch (_: InterruptedException) {
                    Thread.currentThread().interrupt()
                    break
                }
            }
            mainHandler.post { startServiceAndAwaitReady() }
        }, "predecessor-wait").apply { isDaemon = true; start() }
    }

    // True if a previous backend still accepts connections on the abstract
    // platformsvc socket. connect() throwing (refused / no such address) means
    // the address is free.
    private fun predecessorAlive(socketName: String): Boolean {
        val s = LocalSocket()
        return try {
            s.connect(LocalSocketAddress(socketName, LocalSocketAddress.Namespace.ABSTRACT))
            true
        } catch (_: IOException) {
            false
        } finally {
            try { s.close() } catch (_: IOException) { /* ignore */ }
        }
    }

    private fun showWaitingPage() {
        val html = """
            <!doctype html><html><head><meta name="viewport"
            content="width=device-width,initial-scale=1">
            <style>
              html,body{height:100%;margin:0;background:#0b0d10;color:#cfd6e4;
                font-family:-apple-system,Roboto,sans-serif;
                display:flex;align-items:center;justify-content:center}
              .box{text-align:center;padding:2rem}
              .t{font-size:1.1rem;margin-bottom:.5rem}
              .s{font-size:.85rem;color:#8b94a7}
            </style></head><body><div class="box">
            <div class="t">Waiting for the previous session to close&hellip;</div>
            <div class="s">Graywolf is shutting down a prior instance before starting.</div>
            </div></body></html>
        """.trimIndent()
        webView.loadData(html, "text/html", "utf-8")
    }

    private fun startServiceAndAwaitReady() {
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
        // Re-enumerate USB devices on resume. Two flows depend on this:
        //   1) USB_DEVICE_ATTACHED (manifest intent filter on this activity)
        //      brings the activity to the front when an interesting device
        //      plugs in; onResume catches the new device and opens it.
        //   2) Operator swaps the configured PTT method (e.g., AIOC -> Digirig)
        //      via the PTT tab; the freshly-relevant device may be unopened
        //      because UsbPttAdapter only opens devices that match an active
        //      method. Re-enumerating after a method switch reaches the
        //      newly-relevant device on the next resume.
        try {
            UsbPttAdapter.enumerate()
        } catch (t: Throwable) {
            // init() not yet run, or the adapter is between handles —
            // logged inside the adapter; not actionable here.
            Log.w(TAG, "onResume enumerate threw: $t")
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
        // The USB-attach suppression path finish()es in onCreate before webView
        // is built, which skips straight here -- guard the lateinit.
        if (::webView.isInitialized) webView.destroy()
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
        private const val PREF_USER_STOPPED_AT = "user_stopped_at_ms_v1"

        // Window after a deliberate swipe-stop during which a USB_DEVICE_ATTACHED
        // relaunch is treated as our own teardown re-enumeration (the radio's USB
        // interfaces detach + re-attach ~2s after the process dies) rather than a
        // genuine plug-in. Generous enough to cover slow hubs without swallowing a
        // real re-plug seconds later.
        private const val STOP_RELAUNCH_SUPPRESS_WINDOW_MS = 15_000L

        // Predecessor-exit probe cadence and ceiling. The probe runs off the
        // main thread; the timeout is a safety valve so a stuck/zombie
        // predecessor can't block launch forever -- on timeout we start anyway
        // and PlatformServer.start()'s own bounded retry is the final backstop.
        private const val PROBE_STEP_MS = 200L
        private const val PROBE_TIMEOUT_MS = 12_000L

        fun batteryOptWhitelistRequested(ctx: Context): Boolean =
            ctx.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
                .getBoolean(PREF_BATTERY_OPT_REQUESTED, false)

        fun markBatteryOptWhitelistRequested(ctx: Context) {
            ctx.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
                .edit().putBoolean(PREF_BATTERY_OPT_REQUESTED, true).apply()
        }

        // Record the moment the operator deliberately stopped the station (swipe
        // from recents). Called by GraywolfService.onTaskRemoved before stopSelf.
        fun markUserStopped(ctx: Context) {
            ctx.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
                .edit().putLong(PREF_USER_STOPPED_AT, System.currentTimeMillis()).apply()
        }

        fun wasRecentlyStoppedByUser(ctx: Context): Boolean {
            val at = ctx.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
                .getLong(PREF_USER_STOPPED_AT, 0L)
            return at != 0L && System.currentTimeMillis() - at < STOP_RELAUNCH_SUPPRESS_WINDOW_MS
        }

        fun clearUserStopped(ctx: Context) {
            ctx.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
                .edit().remove(PREF_USER_STOPPED_AT).apply()
        }
    }
}
