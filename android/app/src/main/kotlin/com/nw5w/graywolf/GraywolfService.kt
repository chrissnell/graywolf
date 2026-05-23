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
import android.bluetooth.BluetoothDevice
import android.bluetooth.BluetoothManager
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
import com.nw5w.graywolf.platformsvc.BtSerialAdapter
import com.nw5w.graywolf.platformsvc.BindContendedException
import com.nw5w.graywolf.platformsvc.PlatformServer
import com.nw5w.graywolf.platformsvc.SystemBluetoothFacade
import com.nw5w.graywolf.platformsvc.UsbDeviceLister
import com.nw5w.graywolf.usb.UsbPttAdapter
import java.io.File
import kotlin.concurrent.thread

class GraywolfService : Service() {
    private val audioPump = AudioPump()
    private var audioTxPump: AudioTxPump? = null
    private var goLauncher: GoLauncher? = null
    private var platformServer: PlatformServer? = null
    private var gpsAdapter: GpsAdapter? = null
    // Worker that runs the blocking audio/USB HAL init off the main thread
    // (see onCreate). onDestroy joins it before tearing those resources down.
    @Volatile private var startupThread: Thread? = null
    private var btSerialAdapter: BtSerialAdapter? = null
    private val supervisor = Supervisor(onRestart = ::supervisorRestart)

    private val stopReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            if (intent.action == ACTION_STOP) {
                Log.i(TAG, "stop action received; shutting down")
                stopSelf()
            }
        }
    }

    /**
     * ACTION_BOND_STATE_CHANGED listener: tears down any open RFCOMM
     * sockets to a now-unpaired device, and refreshes the bonded list on
     * the Go side when a new pairing completes. The system broadcasts
     * this intent without requiring BLUETOOTH_CONNECT to receive (the
     * permission is only needed for direct API reads).
     */
    private val bondReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            if (intent.action != BluetoothDevice.ACTION_BOND_STATE_CHANGED) return
            val device: BluetoothDevice? = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                intent.getParcelableExtra(BluetoothDevice.EXTRA_DEVICE, BluetoothDevice::class.java)
            } else {
                @Suppress("DEPRECATION")
                intent.getParcelableExtra(BluetoothDevice.EXTRA_DEVICE)
            }
            val mac = device?.address ?: return
            val newState = intent.getIntExtra(
                BluetoothDevice.EXTRA_BOND_STATE,
                BluetoothDevice.ERROR,
            )
            val adapter = btSerialAdapter ?: return
            when (newState) {
                BluetoothDevice.BOND_NONE -> {
                    Log.i(TAG, "bond lost mac=$mac; closing any open RFCOMM handles")
                    adapter.onBondLost(mac)
                }
                BluetoothDevice.BOND_BONDED -> {
                    Log.i(TAG, "bonded mac=$mac; pushing refreshed bonded list")
                    adapter.handleBondedRequest()
                }
                else -> { /* BOND_BONDING or other transient -- ignore */ }
            }
        }
    }

    private fun socketPath(): String =
        File(cacheDir, "graywolf-modem.sock").absolutePath

    private fun platformSocketPath(): String = platformSocketName(this)

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
            registerReceiver(
                bondReceiver,
                IntentFilter(BluetoothDevice.ACTION_BOND_STATE_CHANGED),
                Context.RECEIVER_NOT_EXPORTED,
            )
        } else {
            @Suppress("UnspecifiedRegisterReceiverFlag")
            registerReceiver(stopReceiver, IntentFilter(ACTION_STOP))
            @Suppress("UnspecifiedRegisterReceiverFlag")
            registerReceiver(
                bondReceiver,
                IntentFilter(BluetoothDevice.ACTION_BOND_STATE_CHANGED),
            )
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

        // USB PTT adapter init is cheap (stores context, gets the USB service,
        // registers a receiver) and must precede enumerate(); keep it on the main
        // thread so onResume's enumerate() never races an uninitialized adapter.
        UsbPttAdapter.init(applicationContext)

        // AudioTrack construction + setPreferredDevice and USB device opens are
        // synchronous HAL/binder calls that block for seconds when a USB audio
        // dongle is wedged -- a real failure mode on this hardware, since a churned
        // hub can wedge a Digirig. On the main thread that ANRs onCreate within 5s
        // (lessons: feedback_android_usb_open_worker_thread,
        // feedback_android_audiotxpump_main_thread_anr). Run them off the main
        // thread: the modem TX/PTT callbacks are already installed above and
        // tolerate the brief window before this completes (AudioTxPump drops audio
        // while track is null; UsbPttAdapter re-enumerates when a handle is absent).
        // These are output-path only -- no ordering dependency on modem boot below.
        // onDestroy joins this thread (bounded) before tearing the same resources down.
        startupThread = thread(start = true, isDaemon = true, name = "graywolf-io-init") {
            txPump.start()
            UsbPttAdapter.enumerate()
        }

        // Bring up the Go ↔ Kotlin platform contract before exec'ing the Go child.
        // Phase 2: Hello + GpsFix only; the Go child connects, handshakes, and
        // logs the schema version. Real GpsFix producer is wired in phase 4.
        try {
            platformServer = PlatformServer(
                socketPath = platformSocketPath(),
                serverVersion = BuildConfig.VERSION_NAME,
                schemaVersion = 2,
            ).also { it.start() }
        } catch (e: BindContendedException) {
            // A previous instance still owns the platformsvc socket after the
            // bounded wait. Don't crash (that would relaunch and re-collide);
            // bow out and let the surviving instance keep running. The UI-gated
            // launch path normally prevents reaching here.
            Log.w(TAG, "platformsvc address owned by another instance; stopping this duplicate", e)
            stopSelf()
            return
        }
        gpsAdapter = GpsAdapter(this, platformServer!!).also { it.start() }

        // Bluetooth-classic KISS TNC adapter. Wired AFTER PlatformServer.start()
        // because its sendMessage callback closes over platformServer.broadcastBt.
        // Tolerates a missing BluetoothAdapter (devices without BT, or BT off):
        // SystemBluetoothFacade no-ops bondedDevices and rejects connectRfcomm.
        val btManager = getSystemService(BluetoothManager::class.java)
        val btFacade = SystemBluetoothFacade(btManager?.adapter)
        btSerialAdapter = BtSerialAdapter(
            facade = btFacade,
            sendMessage = { msg -> platformServer!!.broadcastBt(msg) },
        ).also { platformServer!!.attachBtAdapter(it) }

        // USB enumeration provider for the unified PTT tab. The Go side
        // calls platformsvc.ListUsbDevices() when the operator opens the
        // Change Device dialog on Android. No permission is required to
        // enumerate (vid/pid/product are exposed without grant); permission
        // is requested separately when the device is selected.
        platformServer!!.attachUsbDeviceLister { classFilter ->
            UsbDeviceLister.list(
                getSystemService(UsbManager::class.java),
                classFilter,
                beforeQuery = { UsbPttAdapter.pruneStaleHandles() },
            )
        }

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

    // Swiping the app from recents removes the Activity but, with
    // android:stopWithTask unset (default false), the foreground service
    // keeps running and so would the forked Go backend. Stop ourselves so
    // onDestroy's full teardown runs (supervisor, Go child SIGTERM, modem,
    // audio, USB PTT, platform server). A fresh launch then rebuilds the
    // service -- re-enumerating USB and rebooting the modem -- which is
    // also exactly what hot-swap recovery needs.
    override fun onTaskRemoved(rootIntent: Intent?) {
        Log.i(TAG, "onTaskRemoved: task swiped away, stopping service")
        // Mark this as a deliberate stop so the USB_DEVICE_ATTACHED relaunch
        // caused by our own teardown releasing the radio (the interfaces
        // re-enumerate ~2s later) is suppressed in MainActivity rather than
        // silently reviving the station the operator just dismissed.
        MainActivity.markUserStopped(this)
        stopSelf()
        super.onTaskRemoved(rootIntent)
    }

    override fun onDestroy() {
        supervisor.stop()
        goListenerReady = false
        // The off-main-thread audio/USB init (onCreate) may still be in flight --
        // wait for it (bounded) before stopping txPump / closing USB handles, so we
        // don't tear down a half-built AudioTrack or open USB handle. If it's wedged
        // on the HAL the join times out and teardown proceeds; the daemon thread dies
        // with the process. The wait runs here, not on input dispatch, so it can't ANR.
        startupThread?.let {
            it.interrupt()
            it.join(2_000)
        }
        startupThread = null
        gpsAdapter?.stop()
        gpsAdapter = null
        goLauncher?.stop()
        audioPump.stop()
        audioTxPump?.stop()
        audioTxPump = null
        UsbPttAdapter.closeAll()
        // Adapter shutdown emits any final SerialClose frames via broadcastBt
        // -- it MUST run before platformServer.stop() tears the socket down.
        btSerialAdapter?.shutdown()
        btSerialAdapter = null
        platformServer?.stop()
        ModemBridge.modemStop()
        try {
            unregisterReceiver(stopReceiver)
        } catch (_: IllegalArgumentException) { /* idempotent */ }
        try {
            unregisterReceiver(bondReceiver)
        } catch (_: IllegalArgumentException) { /* idempotent */ }
        super.onDestroy()
    }

    companion object {
        private const val TAG = "GraywolfService"

        // The abstract-namespace socket name PlatformServer binds. Exposed so
        // MainActivity can probe whether a previous backend is still alive
        // (connect succeeds) before starting a new one. Must match
        // platformSocketPath() exactly.
        fun platformSocketName(ctx: android.content.Context): String =
            java.io.File(ctx.cacheDir, "platform.sock").absolutePath
        private const val NOTIF_ID = 0x6757
        const val ACTION_STOP = "com.nw5w.graywolf.STOP"

        @Volatile
        var goListenerReady: Boolean = false
            private set
    }
}
