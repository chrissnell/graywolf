package com.nw5w.graywolf.usb

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.hardware.usb.UsbConstants
import android.hardware.usb.UsbDevice
import android.hardware.usb.UsbDeviceConnection
import android.hardware.usb.UsbInterface
import android.hardware.usb.UsbManager
import android.os.Build
import android.util.Log
import com.hoho.android.usbserial.driver.UsbSerialPort
import org.json.JSONObject

/**
 * USB PTT adapter for POC-D. Owns the singletons that key/unkey radios via
 * USB wire toggling: CP2102N RTS for the Digirig PTT path, CM108 HID GPIO for
 * the AIOC path (and Digirig's secondary HID path).
 *
 * Lifecycle:
 *   GraywolfApp.onCreate    -> UsbPttAdapter.init(applicationContext)
 *   MainActivity.onResume   -> UsbPttAdapter.enumerate()  (Activity-driven so
 *                              requestPermission has a foreground UI host)
 *   GraywolfService.onDestroy -> UsbPttAdapter.closeAll() (releases handles
 *                              cleanly across Service restarts)
 *
 * Permission flow:
 *   On enumerate(), each matched device with no granted permission triggers
 *   UsbManager.requestPermission(device, pendingIntent). The adapter's
 *   BroadcastReceiver picks up the grant broadcast and calls tryOpen(device).
 *
 * CM108 invariant (commit 7a71c53): claim ONLY the HID interface, never the
 * audio interfaces. Doing otherwise detaches the kernel snd-usb-audio driver
 * and breaks AudioRecord on this device.
 */
object UsbPttAdapter {
    private const val TAG = "UsbPttAdapter"
    private const val ACTION_USB_PERMISSION = "com.nw5w.graywolf.USB_PERMISSION"

    // Vendor / product IDs locked by spec §3.
    private const val CP2102N_VID = 0x10C4
    private const val CP2102N_PID = 0xEA60
    private const val DIGIRIG_CM108_VID = 0x0D8C
    private const val DIGIRIG_CM108_PID = 0x0012

    // CM108 HID Output Report defaults (spec §1 criterion 7).
    @Volatile var cm108GpioBit: Int = 3
        private set

    private lateinit var appContext: Context
    private lateinit var usbManager: UsbManager
    private var receiverRegistered = false

    // Open-state grouped into immutable handles so JS-thread reads of the
    // (connection, hidIface) pair never tear when the broadcast-receiver
    // thread swaps the handle. Single @Volatile pointer = atomic publish.
    internal data class Cp2102nHandle(val device: UsbDevice, val port: UsbSerialPort)
    internal data class Cm108Handle(
        val device: UsbDevice,
        val connection: UsbDeviceConnection,
        val hidIface: Int,
    )

    @Volatile internal var cp2102n: Cp2102nHandle? = null  // populated in Task 3
    @Volatile internal var cm108: Cm108Handle? = null      // populated in Task 4

    // Per-transport locks. setRts / setHidGpio / tryOpen's mutation of the
    // matching handle synchronize on these, so the JS-thread (WebView binder)
    // and the broadcast-receiver thread can't race the open/close path.
    internal val cp2102nLock = Any()
    internal val cm108Lock = Any()

    private val permissionReceiver = object : BroadcastReceiver() {
        override fun onReceive(ctx: Context, intent: Intent) {
            if (intent.action != ACTION_USB_PERMISSION) return
            val device: UsbDevice? = if (Build.VERSION.SDK_INT >= 33) {
                intent.getParcelableExtra(UsbManager.EXTRA_DEVICE, UsbDevice::class.java)
            } else {
                @Suppress("DEPRECATION")
                intent.getParcelableExtra(UsbManager.EXTRA_DEVICE)
            }
            val granted = intent.getBooleanExtra(UsbManager.EXTRA_PERMISSION_GRANTED, false)
            if (device == null) {
                Log.w(TAG, "permission broadcast with null device")
                return
            }
            Log.i(TAG, "permission result device=${device.deviceName} granted=$granted")
            if (granted) tryOpen(device)
        }
    }

    fun init(ctx: Context) {
        if (this::appContext.isInitialized) return
        appContext = ctx.applicationContext
        usbManager = appContext.getSystemService(Context.USB_SERVICE) as UsbManager
        registerReceiverIfNeeded()
        Log.i(TAG, "init complete")
    }

    private fun registerReceiverIfNeeded() {
        if (receiverRegistered) return
        val filter = IntentFilter(ACTION_USB_PERMISSION)
        if (Build.VERSION.SDK_INT >= 33) {
            appContext.registerReceiver(permissionReceiver, filter, Context.RECEIVER_NOT_EXPORTED)
        } else {
            @Suppress("UnspecifiedRegisterReceiverFlag")
            appContext.registerReceiver(permissionReceiver, filter)
        }
        receiverRegistered = true
    }

    /**
     * Enumerate currently attached USB devices and request permission for any
     * recognized PTT-capable device that the OS has not yet granted us.
     * Devices already permissioned (Use-by-default cache) are opened directly.
     *
     * Idempotent — safe to call multiple times. Already-open transports are
     * skipped (no double-open / handle leak across re-invocations).
     *
     * Driver: MainActivity.onResume. The Activity-foreground guarantee is
     * what lets `requestPermission` surface its system dialog; calling this
     * from a backgrounded Service silently no-ops the prompt.
     *
     * No hot-plug watcher; phase 5.
     */
    fun enumerate() {
        check(this::appContext.isInitialized) { "init(context) must be called first" }
        val devices = usbManager.deviceList
        Log.i(TAG, "enumerate: ${devices.size} attached USB device(s)")
        for ((_, dev) in devices) {
            val role = classify(dev)
            Log.i(
                TAG,
                "device name=${dev.deviceName} vid=0x${"%04X".format(dev.vendorId)} " +
                    "pid=0x${"%04X".format(dev.productId)} role=$role ifaces=${dev.interfaceCount}"
            )
            if (role == DeviceRole.UNKNOWN) continue
            // Skip if this transport already has an open handle on this device.
            if (role == DeviceRole.CP2102N && cp2102n?.device?.deviceName == dev.deviceName) {
                Log.i(TAG, "enumerate: CP2102N already open, skipping")
                continue
            }
            if (role == DeviceRole.CM108 && cm108?.device?.deviceName == dev.deviceName) {
                Log.i(TAG, "enumerate: CM108 already open, skipping")
                continue
            }
            if (usbManager.hasPermission(dev)) {
                tryOpen(dev)
            } else {
                requestPermission(dev)
            }
        }
    }

    /**
     * Release CP2102N and CM108 handles. Called from `GraywolfService.onDestroy`
     * so a Service restart re-opens fresh handles instead of leaking the
     * previous `UsbDeviceConnection` / `UsbSerialPort`. Safe to call when no
     * handles are open.
     */
    fun closeAll() {
        synchronized(cp2102nLock) {
            cp2102n?.let { h ->
                try { h.port.close() } catch (t: Throwable) { Log.w(TAG, "cp2102n.close: $t") }
                Log.i(TAG, "closeAll: cp2102n released ${h.device.deviceName}")
            }
            cp2102n = null
        }
        synchronized(cm108Lock) {
            cm108?.let { h ->
                try {
                    val iface = (0 until h.device.interfaceCount)
                        .map { h.device.getInterface(it) }
                        .firstOrNull { it.id == h.hidIface }
                    if (iface != null) h.connection.releaseInterface(iface)
                    h.connection.close()
                } catch (t: Throwable) { Log.w(TAG, "cm108.close: $t") }
                Log.i(TAG, "closeAll: cm108 released ${h.device.deviceName}")
            }
            cm108 = null
        }
    }

    private fun requestPermission(dev: UsbDevice) {
        val intent = Intent(ACTION_USB_PERMISSION).setPackage(appContext.packageName)
        val flags = if (Build.VERSION.SDK_INT >= 31) {
            PendingIntent.FLAG_MUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        } else {
            PendingIntent.FLAG_UPDATE_CURRENT
        }
        val pi = PendingIntent.getBroadcast(appContext, 0, intent, flags)
        Log.i(TAG, "requestPermission ${dev.deviceName}")
        usbManager.requestPermission(dev, pi)
    }

    /**
     * Attempt to open a device for which we already hold permission. Task 3
     * (CP2102N) and Task 4 (CM108) plug into this — for now the function
     * exists so the permission broadcast has a hook to call.
     */
    internal fun tryOpen(dev: UsbDevice) {
        val role = classify(dev)
        Log.i(TAG, "tryOpen ${dev.deviceName} role=$role (no-op until Task 3/4)")
    }

    /** Classify a device by vid/pid + structural fingerprint. */
    internal fun classify(dev: UsbDevice): DeviceRole {
        if (dev.vendorId == CP2102N_VID && dev.productId == CP2102N_PID) {
            return DeviceRole.CP2102N
        }
        if (dev.vendorId == DIGIRIG_CM108_VID && dev.productId == DIGIRIG_CM108_PID) {
            return DeviceRole.CM108
        }
        // Generic CM108-class fingerprint: composite device with at least one
        // HID interface and at least one audio-class interface. Catches the
        // AIOC even though its pid isn't known at plan-write time.
        var hasHid = false
        var hasAudio = false
        for (i in 0 until dev.interfaceCount) {
            when (dev.getInterface(i).interfaceClass) {
                UsbConstants.USB_CLASS_HID -> hasHid = true
                UsbConstants.USB_CLASS_AUDIO -> hasAudio = true
            }
        }
        return if (hasHid && hasAudio) DeviceRole.CM108 else DeviceRole.UNKNOWN
    }

    /** First HID-class interface number on a CM108 device. -1 if none found. */
    internal fun findHidInterface(dev: UsbDevice): Int {
        for (i in 0 until dev.interfaceCount) {
            val iface: UsbInterface = dev.getInterface(i)
            if (iface.interfaceClass == UsbConstants.USB_CLASS_HID) return iface.id
        }
        return -1
    }

    /** Snapshot of opened state for the WebView status row. Status keys are
     *  transport-keyed (cp2102n_*, cm108_*); these names also flow into the
     *  phase-5 device-status proto, so don't rename them lightly. */
    fun status(): JSONObject = JSONObject().apply {
        val sp = cp2102n
        val cm = cm108
        put("cp2102n_open", sp != null)
        put("cm108_open", cm != null)
        put("cm108_hid_iface", cm?.hidIface ?: -1)
        put("cm108_gpio_bit", cm108GpioBit)
        cm?.let {
            put("cm108_vid", "0x%04X".format(it.device.vendorId))
            put("cm108_pid", "0x%04X".format(it.device.productId))
        }
        // Found-but-not-open state for the status row. Cheap on every poll
        // since deviceList is a HashMap reference; classify walks interface
        // counts only on cache miss.
        var foundCp = false
        var foundCm = false
        if (this@UsbPttAdapter::usbManager.isInitialized) {
            for ((_, dev) in usbManager.deviceList) {
                when (classify(dev)) {
                    DeviceRole.CP2102N -> foundCp = true
                    DeviceRole.CM108   -> foundCm = true
                    DeviceRole.UNKNOWN -> {}
                }
            }
        }
        put("cp2102n_found", foundCp)
        put("cm108_found", foundCm)
    }

    enum class DeviceRole { CP2102N, CM108, UNKNOWN }
}
