package com.nw5w.graywolf

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.Manifest
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import androidx.core.content.ContextCompat
import android.graphics.drawable.Icon
import android.hardware.usb.UsbManager
import android.net.ConnectivityManager
import android.os.Build
import android.os.IBinder
import android.util.Log
import java.net.Inet6Address
import com.nw5w.graywolf.BuildConfig
import com.nw5w.graywolf.audio.AudioPump
import com.nw5w.graywolf.audio.AudioTxPump
import com.nw5w.graywolf.binaries.GoLauncher
import com.nw5w.graywolf.binaries.Supervisor
import com.nw5w.graywolf.gps.GpsAdapter
import com.nw5w.graywolf.jni.ModemBridge
import com.nw5w.graywolf.platformsvc.PlatformServer
import com.nw5w.graywolf.usb.UsbPttAdapter
import java.io.File

class GraywolfService : Service() {
    private val audioPump = AudioPump()
    private var audioTxPump: AudioTxPump? = null
    private var goLauncher: GoLauncher? = null
    private var platformServer: PlatformServer? = null
    private var gpsAdapter: GpsAdapter? = null
    private val supervisor = Supervisor(onRestart = ::supervisorRestart)

    private val stopReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            if (intent.action == ACTION_STOP) {
                Log.i(TAG, "stop action received; shutting down")
                stopSelf()
            }
        }
    }

    private fun socketPath(): String =
        File(cacheDir, "graywolf-modem.sock").absolutePath

    private fun platformSocketPath(): String =
        File(cacheDir, "platform.sock").absolutePath

    /**
     * Read the active network's DNS server list from ConnectivityManager
     * and return as a comma-separated string of IP literals. Empty when
     * no active network or no DNS servers (Wi-Fi off / airplane mode).
     *
     * IPv6 addresses are wrapped in brackets so the consumer (Go side)
     * can parse them as `host:port` directly.
     */
    private fun currentDnsServers(): String {
        val cm = getSystemService(ConnectivityManager::class.java) ?: return ""
        val net = cm.activeNetwork ?: return ""
        val lp = cm.getLinkProperties(net) ?: return ""
        return lp.dnsServers.joinToString(",") { addr ->
            if (addr is Inet6Address) "[${addr.hostAddress}]" else addr.hostAddress ?: ""
        }
    }

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
        val tileCacheDir = File(filesDir, "tiles").also { it.mkdirs() }
        val dnsServers = currentDnsServers()
        Log.i(TAG, "GRAYWOLF_DNS_SERVERS=$dnsServers")
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
                "GRAYWOLF_DB" to File(filesDir, "graywolf.db").absolutePath,
                "GRAYWOLF_HISTORY_DB" to File(filesDir, "graywolf-history.db").absolutePath,
                "GRAYWOLF_TILE_CACHE" to tileCacheDir.absolutePath,
                "GRAYWOLF_PLATFORM" to "android",
                // Android has no /etc/resolv.conf; without this Go's net
                // resolver falls through to dialing [::1]:53 and every
                // outbound DNS lookup fails with "connection refused".
                // Pull DNS server list from the active network's
                // LinkProperties and let Go override its DefaultResolver.
                "GRAYWOLF_DNS_SERVERS" to dnsServers,
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
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            registerReceiver(
                stopReceiver,
                IntentFilter(ACTION_STOP),
                Context.RECEIVER_NOT_EXPORTED,
            )
        } else {
            @Suppress("UnspecifiedRegisterReceiverFlag")
            registerReceiver(stopReceiver, IntentFilter(ACTION_STOP))
        }
        val stopIntent = Intent(ACTION_STOP).setPackage(packageName)
        val stopPending = PendingIntent.getBroadcast(
            this, 0, stopIntent,
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        val notif: Notification = Notification.Builder(this, getString(R.string.notification_channel_id))
            .setContentTitle(getString(R.string.notification_title))
            .setContentText(getString(R.string.notification_text))
            .setSmallIcon(R.drawable.ic_notification)
            .addAction(
                Notification.Action.Builder(
                    Icon.createWithResource(this, android.R.drawable.ic_menu_close_clear_cancel),
                    getString(R.string.notification_stop_label),
                    stopPending,
                ).build()
            )
            .build()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            // Phase 4a adds LOCATION FGS type alongside MICROPHONE.
            // Android 14 throws SecurityException if we declare an FGS
            // type whose paired access perm is denied at runtime, so
            // only include FGS_TYPE_LOCATION when ACCESS_FINE_LOCATION
            // is actually granted. RECORD_AUDIO is always granted by
            // this point (MainActivity.ensurePerms gates the launch).
            var fgsType = ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE
            if (ContextCompat.checkSelfPermission(this, Manifest.permission.ACCESS_FINE_LOCATION)
                == PackageManager.PERMISSION_GRANTED) {
                fgsType = fgsType or ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION
            } else {
                Log.i(TAG, "ACCESS_FINE_LOCATION denied; starting FGS without location type")
            }
            // MEDIA_PLAYBACK pairs with no runtime perm; always safe to include.
            fgsType = fgsType or ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PLAYBACK
            // Per spec §3.6 + Android 14: CONNECTED_DEVICE FGS type requires that
            // at least one USB device has been granted permission at start time, or
            // startForeground throws SecurityException. Probe with UsbManager
            // directly (UsbPttAdapter isn't init'd yet at this point).
            val usbManager = getSystemService(UsbManager::class.java)
            val hasGrantedUsbDevice = usbManager?.deviceList?.values
                ?.any { usbManager.hasPermission(it) } == true
            if (hasGrantedUsbDevice) {
                fgsType = fgsType or ServiceInfo.FOREGROUND_SERVICE_TYPE_CONNECTED_DEVICE
            } else {
                Log.i(TAG, "no USB device permission yet; starting FGS without CONNECTED_DEVICE type")
            }
            startForeground(NOTIF_ID, notif, fgsType)
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

        // Install JNI callbacks immediately after loadLibrary (modemVersion triggers it).
        // Must precede bootModem so any TX/PTT activation the modem fires on boot
        // finds a registered callback. T5/T6/T7 supply the implementations.
        ModemBridge.installPttCallback(UsbPttAdapter)
        val txPump = AudioTxPump(applicationContext)
        audioTxPump = txPump
        ModemBridge.installAudioTxCallback(txPump)

        // Start TX audio + USB PTT adapter after FGS is active (spec §3.1).
        txPump.start()
        UsbPttAdapter.init(applicationContext)
        UsbPttAdapter.enumerate()

        // Bring up the Go ↔ Kotlin platform contract before exec'ing the Go child.
        // Phase 2: Hello + GpsFix only; the Go child connects, handshakes, and
        // logs the schema version. Real GpsFix producer is wired in phase 4.
        platformServer = PlatformServer(
            socketPath = platformSocketPath(),
            serverVersion = BuildConfig.VERSION_NAME,
            schemaVersion = 1,
        ).also { it.start() }
        gpsAdapter = GpsAdapter(this, platformServer!!).also { it.start() }

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

        supervisor.start { goLauncher?.process }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int = START_STICKY

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onDestroy() {
        supervisor.stop()
        goListenerReady = false
        gpsAdapter?.stop()
        gpsAdapter = null
        goLauncher?.stop()
        audioPump.stop()
        audioTxPump?.stop()
        audioTxPump = null
        UsbPttAdapter.closeAll()
        platformServer?.stop()
        ModemBridge.modemStop()
        try {
            unregisterReceiver(stopReceiver)
        } catch (_: IllegalArgumentException) { /* idempotent */ }
        super.onDestroy()
    }

    companion object {
        private const val TAG = "GraywolfService"
        private const val NOTIF_ID = 0x6757
        const val ACTION_STOP = "com.nw5w.graywolf.STOP"

        @Volatile
        var goListenerReady: Boolean = false
            private set
    }
}
