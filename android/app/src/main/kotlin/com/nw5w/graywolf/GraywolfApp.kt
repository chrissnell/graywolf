package com.nw5w.graywolf

import android.app.Application
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

    // Written by GraywolfService during storage migration, read by
    // WebAppInterface via a lambda so both sides share without coupling.
    @Volatile
    var migrationStateJson: String = """{"state":"idle","progress":0,"message":""}"""

    // Written by GraywolfService after bootGoChild, read by WebAppInterface.
    @Volatile
    var storageInfoJson: String =
        """{"use_sd_card":false,"sd_card_available":false,"sd_card_path":"","internal_path":""}"""

    override fun onCreate() {
        super.onCreate()
        val b = ByteArray(32)
        SecureRandom().nextBytes(b)
        bearerToken = b.joinToString("") { "%02x".format(it) }
    }
}

