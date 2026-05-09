package com.nw5w.graywolf

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import android.util.Log
import com.nw5w.graywolf.BuildConfig
import com.nw5w.graywolf.audio.AudioPump
import com.nw5w.graywolf.binaries.GoLauncher
import com.nw5w.graywolf.binaries.Supervisor
import com.nw5w.graywolf.jni.ModemBridge
import com.nw5w.graywolf.platformsvc.PlatformServer
import com.nw5w.graywolf.usb.UsbPttAdapter
import java.io.File
import kotlin.concurrent.thread

class GraywolfService : Service() {
    private val audioPump = AudioPump()
    private var goLauncher: GoLauncher? = null
    private var platformServer: PlatformServer? = null
    private var gainPoller: Thread? = null
    private val supervisor = Supervisor(onRestart = ::supervisorRestart)

    private fun socketPath(): String =
        File(cacheDir, "graywolf-modem.sock").absolutePath

    private fun platformSocketPath(): String =
        File(cacheDir, "platform.sock").absolutePath

    private fun bootModem(): Boolean {
        val rc = ModemBridge.modemStart(socketPath(), /* gainDb = */ -6.0f)
        if (rc != 0) {
            Log.e(TAG, "modemStart rc=$rc")
            return false
        }
        val ready = ModemBridge.modemAwaitReady(10_000)
        Log.i(TAG, "modemAwaitReady=$ready")
        return ready
    }

    private fun bootGoChild(): Boolean {
        val bearerToken = (application as GraywolfApp).bearerToken
        val goPath = File(applicationInfo.nativeLibraryDir, "libgraywolf.so").absolutePath
        val launcher = GoLauncher(
            executablePath = goPath,
            env = mapOf(
                "GRAYWOLF_MODEM_SOCKET" to socketPath(),
                // android.net.LocalServerSocket(String) binds in the Linux
                // abstract namespace (no filesystem entry, name prefixed
                // with NUL). Go's net package dials abstract sockets via a
                // leading "@". We expose the abstract-form address to the
                // Go child so both sides agree.
                "GRAYWOLF_PLATFORM_SOCKET" to "@" + platformSocketPath(),
                "GRAYWOLF_LISTEN" to "127.0.0.1:8080",
                "GRAYWOLF_LISTEN_TOKEN" to bearerToken,
            ),
        )
        val ok = launcher.startAndAwaitReady(10_000)
        if (!ok) {
            Log.e(TAG, "go child did not signal readiness")
            return false
        }
        goLauncher = launcher
        goListenerReady = true
        Log.i(TAG, "poc-b: go_child_up")
        return true
    }

    private fun supervisorRestart(): Boolean {
        Log.i(TAG, "poc-b: supervisor_restart_begin")
        goListenerReady = false
        audioPump.stop()
        goLauncher?.stop()
        ModemBridge.modemStop()
        if (!bootModem()) return false
        audioPump.start()
        return bootGoChild()
    }

    override fun onCreate() {
        super.onCreate()
        val mgr = getSystemService(NotificationManager::class.java)!!
        mgr.createNotificationChannel(
            NotificationChannel(
                getString(R.string.notification_channel_id),
                "graywolf foreground",
                NotificationManager.IMPORTANCE_LOW,
            )
        )
        val notif: Notification = Notification.Builder(this, getString(R.string.notification_channel_id))
            .setContentTitle(getString(R.string.notification_title))
            .setContentText(getString(R.string.notification_text))
            .setSmallIcon(android.R.drawable.ic_media_play)
            .build()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(
                NOTIF_ID, notif,
                ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE
            )
        } else {
            startForeground(NOTIF_ID, notif)
        }
        val v = try {
            ModemBridge.modemVersion()
        } catch (t: Throwable) {
            Log.e(TAG, "modemVersion threw: $t")
            "ERROR"
        }
        Log.i(TAG, "modem cdylib version=$v")

        // Bring up the Go ↔ Kotlin platform contract before exec'ing the Go child.
        // Phase 2: Hello + GpsFix only; the Go child connects, handshakes, and
        // logs the schema version. Real GpsFix producer is wired in phase 4.
        platformServer = PlatformServer(
            socketPath = platformSocketPath(),
            serverVersion = BuildConfig.VERSION_NAME,
            schemaVersion = 1,
        ).also { it.start() }

        if (!bootModem()) {
            stopSelf()
            return
        }
        audioPump.start()
        if (!bootGoChild()) {
            audioPump.stop()
            ModemBridge.modemStop()
            stopSelf()
            return
        }

        gainPoller = thread(start = true, isDaemon = true, name = "gain-poll") {
            val token = (application as GraywolfApp).bearerToken
            var last = Float.NaN
            val rx = Regex("""\"db\":(-?\d+(?:\.\d+)?)""")
            while (!Thread.currentThread().isInterrupted) {
                try {
                    val u = java.net.URL("http://127.0.0.1:8080/api/_internal/gain")
                    val c = u.openConnection() as java.net.HttpURLConnection
                    c.setRequestProperty("Authorization", "Bearer $token")
                    val body = c.inputStream.bufferedReader().readText()
                    val db = rx.find(body)?.groupValues?.get(1)?.toFloatOrNull()
                    if (db != null && db != last) {
                        ModemBridge.modemSetGainDb(db)
                        Log.i(TAG, "poc-b: gain_applied db=$db")
                        last = db
                    }
                } catch (_: Throwable) { /* swallow */ }
                try { Thread.sleep(1000) } catch (_: InterruptedException) { return@thread }
            }
        }

        supervisor.start { goLauncher?.process }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int = START_STICKY

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onDestroy() {
        supervisor.stop()
        gainPoller?.interrupt()
        gainPoller = null
        goListenerReady = false
        goLauncher?.stop()
        audioPump.stop()
        platformServer?.stop()
        ModemBridge.modemStop()
        UsbPttAdapter.closeAll()
        super.onDestroy()
    }

    companion object {
        private const val TAG = "GraywolfService"
        private const val NOTIF_ID = 0x6757

        @Volatile
        var goListenerReady: Boolean = false
            private set
    }
}
