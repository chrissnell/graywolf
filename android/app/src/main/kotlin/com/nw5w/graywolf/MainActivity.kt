package com.nw5w.graywolf

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.util.Log
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import com.nw5w.graywolf.usb.UsbPttAdapter
import com.nw5w.graywolf.webview.WebAppInterface

class MainActivity : Activity() {
    private lateinit var webView: WebView
    private val mainHandler = Handler(Looper.getMainLooper())
    private var didReloadOnError = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        webView = WebView(this).also {
            it.settings.javaScriptEnabled = true
            it.settings.domStorageEnabled = true
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
        UsbPttAdapter.enumerate()
    }

    override fun onDestroy() {
        webView.destroy()
        super.onDestroy()
    }

    companion object {
        private const val TAG = "MainActivity"
        private const val REQ_PERMS = 0x101
    }
}
