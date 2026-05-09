package com.nw5w.graywolf

import android.app.Application
import com.nw5w.graywolf.usb.UsbPttAdapter
import java.security.SecureRandom

/**
 * Application owns the per-process bearer token. The Service can be
 * killed and restarted under Doze / low-memory pressure, but the
 * Application object lives for the whole process — keeping the token
 * stable across Service restarts so the Activity/WebView never read a
 * stale value.
 */
class GraywolfApp : Application() {
    lateinit var bearerToken: String
        private set

    override fun onCreate() {
        super.onCreate()
        UsbPttAdapter.init(this)
        val b = ByteArray(32)
        SecureRandom().nextBytes(b)
        bearerToken = b.joinToString("") { "%02x".format(it) }
    }
}
