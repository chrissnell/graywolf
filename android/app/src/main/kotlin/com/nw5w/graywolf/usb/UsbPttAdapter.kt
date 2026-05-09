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
import com.hoho.android.usbserial.driver.Cp21xxSerialDriver
import com.hoho.android.usbserial.driver.UsbSerialDriver
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
     * Attempt to open a device for which we already hold permission.
     * Branches on classified role; CP2102N opens via usb-serial-for-android,
     * CM108 claims its HID interface only.
     *
     * Dispatches the actual open to a background thread because the caller
     * may be the BroadcastReceiver (USB permission grant) or MainActivity
     * onResume — both run on the main thread, and synchronous USB control
     * transfers can stall it long enough to trip Input-dispatch ANR.
     */
    internal fun tryOpen(dev: UsbDevice) {
        val role = classify(dev)
        Log.i(TAG, "tryOpen ${dev.deviceName} role=$role")
        when (role) {
            DeviceRole.CP2102N -> Thread({ openCp2102n(dev) }, "ptt-open-cp2102n").apply { isDaemon = true }.start()
            DeviceRole.CM108   -> Thread({ openCm108(dev) }, "ptt-open-cm108").apply { isDaemon = true }.start()
            DeviceRole.UNKNOWN -> Log.w(TAG, "tryOpen on UNKNOWN device — skipping")
        }
    }

    private fun openCp2102n(dev: UsbDevice) = synchronized(cp2102nLock) {
        if (cp2102n != null) {
            Log.i(TAG, "openCp2102n: already open, skipping")
            return@synchronized
        }
        try {
            Log.i(TAG, "openCp2102n: step=ctor")
            val driver: UsbSerialDriver = Cp21xxSerialDriver(dev)
            Log.i(TAG, "openCp2102n: step=openDevice")
            val conn: UsbDeviceConnection = usbManager.openDevice(dev) ?: run {
                Log.e(TAG, "openDevice returned null for CP2102N — permission revoked?")
                return@synchronized
            }
            Log.i(TAG, "openCp2102n: step=ports")
            val port = driver.ports.firstOrNull() ?: run {
                Log.e(TAG, "CP2102N driver returned 0 ports")
                conn.close()
                return@synchronized
            }
            Log.i(TAG, "openCp2102n: step=port.open")
            port.open(conn)
            // Spec §7 trap: some CP210x variants need setLineEncoding before
            // RTS toggles take effect. usb-serial-for-android handles this
            // via setParameters; call once at a benign default.
            Log.i(TAG, "openCp2102n: step=setParameters")
            port.setParameters(9600, 8, UsbSerialPort.STOPBITS_1, UsbSerialPort.PARITY_NONE)
            Log.i(TAG, "openCp2102n: step=rts=false")
            port.rts = false
            cp2102n = Cp2102nHandle(dev, port)
            Log.i(TAG, "CP2102N opened ${dev.deviceName}")
        } catch (t: Throwable) {
            Log.e(TAG, "openCp2102n failed: $t")
        }
    }

    private fun openCm108(dev: UsbDevice) = synchronized(cm108Lock) {
        if (cm108 != null) {
            Log.i(TAG, "openCm108: already open, skipping")
            return@synchronized
        }
        val ifaceId = findHidInterface(dev)
        if (ifaceId < 0) {
            Log.e(TAG, "openCm108: no HID interface on ${dev.deviceName}")
            return@synchronized
        }
        val conn: UsbDeviceConnection = usbManager.openDevice(dev) ?: run {
            Log.e(TAG, "openCm108: openDevice returned null — permission revoked?")
            return@synchronized
        }
        val iface: UsbInterface = (0 until dev.interfaceCount)
            .map { dev.getInterface(it) }
            .first { it.id == ifaceId }
        // Critical invariant (commit 7a71c53): claim ONLY this HID interface.
        // Do NOT pass force=true on audio interfaces — that detaches the
        // kernel snd-usb-audio driver and breaks AudioRecord on Android.
        if (!conn.claimInterface(iface, /* force = */ true)) {
            Log.e(TAG, "openCm108: claimInterface($ifaceId) failed")
            conn.close()
            return@synchronized
        }
        cm108 = Cm108Handle(dev, conn, ifaceId)
        Log.i(
            TAG,
            "CM108 opened ${dev.deviceName} hid_iface=$ifaceId vid=0x${"%04X".format(dev.vendorId)} " +
                "pid=0x${"%04X".format(dev.productId)}"
        )
    }

    /** Transport-keyed (not vendor-keyed) so the CM108 HID path can fan out
     *  to both Digirig and AIOC under the same name. WebView button labels
     *  retain the vendor name for operator clarity. */
    fun keyCp2102nRts(): Boolean = setRts(true)
    fun unkeyCp2102nRts(): Boolean = setRts(false)

    private fun setRts(state: Boolean): Boolean = synchronized(cp2102nLock) {
        val h = cp2102n ?: run {
            Log.w(TAG, "setRts($state) but CP2102N not open")
            return@synchronized false
        }
        return@synchronized try {
            h.port.rts = state
            Log.i(TAG, "ptt: cp2102n_rts=$state")
            true
        } catch (t: Throwable) {
            Log.e(TAG, "setRts($state) failed: $t")
            false
        }
    }

    fun keyCm108Hid(): Boolean = setHidGpio(true)
    fun unkeyCm108Hid(): Boolean = setHidGpio(false)

    /**
     * Empirical CM108 GPIO bit search: AIOC firmware revisions have used
     * bits 0..3 over time. Allowed range 0..7. Out-of-range calls are no-ops
     * with a warn log. Setting the bit does not key — caller must follow
     * with keyCm108Hid().
     */
    fun setCm108Bit(bit: Int): Boolean {
        if (bit < 0 || bit > 7) {
            Log.w(TAG, "setCm108Bit($bit) out of range")
            return false
        }
        cm108GpioBit = bit
        Log.i(TAG, "cm108_gpio_bit=$bit")
        return true
    }

    private fun setHidGpio(state: Boolean): Boolean = synchronized(cm108Lock) {
        val h = cm108 ?: run {
            Log.w(TAG, "setHidGpio($state) but CM108 not open")
            return@synchronized false
        }
        val gpioByte: Byte = if (state) (1 shl cm108GpioBit).toByte() else 0
        // 4-byte CM108 HID Output Report: [0x00, 0x00, gpio, 0x00].
        val report = byteArrayOf(0x00, 0x00, gpioByte, 0x00)
        // controlTransfer(requestType, request, value, index, buffer, length, timeout_ms)
        //   requestType = 0x21 (Class | Interface | Host->Device)
        //   request     = 0x09 (SET_REPORT)
        //   value       = 0x0200 (Output Report, ID 0)
        //   index       = HID interface number
        val rc = h.connection.controlTransfer(
            /* requestType = */ 0x21,
            /* request     = */ 0x09,
            /* value       = */ 0x0200,
            /* index       = */ h.hidIface,
            /* buffer      = */ report,
            /* length      = */ report.size,
            /* timeout_ms  = */ 200,
        )
        Log.i(TAG, "ptt: cm108_set_report bit=$cm108GpioBit state=$state rc=$rc")
        return@synchronized rc == report.size
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
