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
import com.hoho.android.usbserial.driver.CdcAcmSerialDriver
import com.hoho.android.usbserial.driver.Cp21xxSerialDriver
import com.hoho.android.usbserial.driver.UsbSerialDriver
import com.hoho.android.usbserial.driver.UsbSerialPort
import com.nw5w.graywolf.jni.UsbPttCallback
import org.json.JSONArray
import org.json.JSONObject

/**
 * USB PTT adapter for POC-D. Owns the singletons that key/unkey radios via
 * USB wire toggling: CP2102N RTS for the Digirig PTT path, CDC-ACM DTR (RTS
 * held low) for the AIOC path, and CM108 HID GPIO for generic CM108-class
 * hardware and Digirig's secondary HID path. (See the AIOC inline note below
 * and POC-D results: AIOC firmware >=1.2.0 accepts HID Set_Report but does not
 * drive the PTT GPIO; CDC-ACM is the actual PTT control path.)
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
object UsbPttAdapter : UsbPttCallback {
    private const val TAG = "UsbPttAdapter"
    private const val ACTION_USB_PERMISSION = "com.nw5w.graywolf.USB_PERMISSION"

    // Vendor / product IDs locked by spec §3.
    private const val CP2102N_VID = 0x10C4
    private const val CP2102N_PID = 0xEA60
    private const val DIGIRIG_CM108_VID = 0x0D8C
    private const val DIGIRIG_CM108_PID = 0x0012
    // AIOC (All-in-One-Cable, skuep firmware): STM32 USB composite device.
    // Exposes CM108-compat HID for backwards software compat, but on this
    // firmware revision the HID Output Report is accepted (rc=4) without
    // driving the PTT GPIO. The CDC-ACM RTS line is the actual PTT path.
    private const val AIOC_VID = 0x1209
    private const val AIOC_PID = 0x7388

    // CM108 HID Output Report defaults. Pin is 1-indexed, matching the CM108
    // datasheet ("GPIO3") and graywolf-modem's tx/ptt_cm108_unix.rs naming.
    // Internally mask = 1 shl (pin - 1). Default 3 = datasheet PTT (mask 0x04).
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
    internal data class AiocHandle(val device: UsbDevice, val port: UsbSerialPort)

    @Volatile internal var cp2102n: Cp2102nHandle? = null
    @Volatile internal var cm108: Cm108Handle? = null
    @Volatile internal var aioc: AiocHandle? = null

    // Per-transport locks. setRts / setHidGpio / tryOpen's mutation of the
    // matching handle synchronize on these, so the JS-thread (WebView binder)
    // and the broadcast-receiver thread can't race the open/close path.
    internal val cp2102nLock = Any()
    internal val cm108Lock = Any()
    internal val aiocLock = Any()

    // One-shot grant/deny callbacks registered by requestPermissionFor().
    // Key = deviceName; entry consumed on first broadcast delivery.
    private val pendingPermissionCallbacks =
        java.util.concurrent.ConcurrentHashMap<String, (Boolean) -> Unit>()

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
            pendingPermissionCallbacks.remove(device.deviceName)?.invoke(granted)
        }
    }

    /**
     * System-broadcast hotplug watcher. Receives USB_DEVICE_ATTACHED whenever
     * a USB device is plugged in (regardless of whether the app is foreground)
     * and USB_DEVICE_DETACHED on unplug. ATTACHED → classify; if recognized,
     * request permission (if missing) or tryOpen (if granted). DETACHED →
     * release any handle whose deviceName matches the unplugged device so a
     * subsequent re-attach gets a fresh open instead of inheriting a stale
     * UsbDeviceConnection.
     *
     * This closes the gap onResume() can't cover: when MainActivity is already
     * resumed (PTT page visible) and the operator swaps adapters, onResume
     * never re-fires, so the swap-in device would otherwise sit unrecognized.
     */
    private val hotPlugReceiver = object : BroadcastReceiver() {
        override fun onReceive(ctx: Context, intent: Intent) {
            val device: UsbDevice? = if (Build.VERSION.SDK_INT >= 33) {
                intent.getParcelableExtra(UsbManager.EXTRA_DEVICE, UsbDevice::class.java)
            } else {
                @Suppress("DEPRECATION")
                intent.getParcelableExtra(UsbManager.EXTRA_DEVICE)
            }
            if (device == null) {
                Log.w(TAG, "hotplug broadcast with null device (action=${intent.action})")
                return
            }
            when (intent.action) {
                UsbManager.ACTION_USB_DEVICE_ATTACHED -> {
                    val role = classify(device)
                    Log.i(TAG, "hotplug ATTACHED ${device.deviceName} vid=0x${"%04X".format(device.vendorId)} pid=0x${"%04X".format(device.productId)} role=$role")
                    if (role == DeviceRole.UNKNOWN) return
                    if (usbManager.hasPermission(device)) {
                        tryOpen(device)
                    } else {
                        requestPermission(device)
                    }
                }
                UsbManager.ACTION_USB_DEVICE_DETACHED -> {
                    Log.i(TAG, "hotplug DETACHED ${device.deviceName}")
                    closeForDevice(device)
                }
            }
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
        val permFilter = IntentFilter(ACTION_USB_PERMISSION)
        val hotPlugFilter = IntentFilter().apply {
            addAction(UsbManager.ACTION_USB_DEVICE_ATTACHED)
            addAction(UsbManager.ACTION_USB_DEVICE_DETACHED)
        }
        if (Build.VERSION.SDK_INT >= 33) {
            appContext.registerReceiver(permissionReceiver, permFilter, Context.RECEIVER_NOT_EXPORTED)
            appContext.registerReceiver(hotPlugReceiver, hotPlugFilter, Context.RECEIVER_NOT_EXPORTED)
        } else {
            @Suppress("UnspecifiedRegisterReceiverFlag")
            appContext.registerReceiver(permissionReceiver, permFilter)
            @Suppress("UnspecifiedRegisterReceiverFlag")
            appContext.registerReceiver(hotPlugReceiver, hotPlugFilter)
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
     * Hot-plug is handled separately by hotPlugReceiver (USB_DEVICE_ATTACHED
     * / DETACHED), so this is primarily an at-startup / on-resume sweep.
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
            for (i in 0 until dev.interfaceCount) {
                val iface = dev.getInterface(i)
                val epDesc = StringBuilder()
                for (e in 0 until iface.endpointCount) {
                    val ep = iface.getEndpoint(e)
                    epDesc.append(" ep[").append(e).append("] addr=0x")
                        .append("%02X".format(ep.address)).append(" type=").append(ep.type)
                        .append(" dir=").append(ep.direction)
                }
                Log.i(TAG,
                    "  iface[$i] id=${iface.id} class=${iface.interfaceClass} " +
                        "subclass=${iface.interfaceSubclass} proto=${iface.interfaceProtocol}" +
                        " endpoints=${iface.endpointCount}$epDesc")
            }
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
            if (role == DeviceRole.AIOC && aioc?.device?.deviceName == dev.deviceName) {
                Log.i(TAG, "enumerate: AIOC already open, skipping")
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
        synchronized(aiocLock) {
            aioc?.let { h ->
                try { h.port.close() } catch (t: Throwable) { Log.w(TAG, "aioc.close: $t") }
                Log.i(TAG, "closeAll: aioc released ${h.device.deviceName}")
            }
            aioc = null
        }
    }

    /**
     * Release any open handle whose deviceName matches the supplied device,
     * leaving the other transports untouched. Called from the DETACHED
     * hot-plug broadcast so a re-attach gets a fresh open instead of using
     * a stale UsbDeviceConnection backed by an unplugged device.
     */
    private fun closeForDevice(dev: UsbDevice) {
        synchronized(cp2102nLock) {
            cp2102n?.let { h ->
                if (h.device.deviceName == dev.deviceName) {
                    try { h.port.close() } catch (t: Throwable) { Log.w(TAG, "cp2102n.close on detach: $t") }
                    Log.i(TAG, "closeForDevice: cp2102n released ${dev.deviceName}")
                    cp2102n = null
                }
            }
        }
        synchronized(cm108Lock) {
            cm108?.let { h ->
                if (h.device.deviceName == dev.deviceName) {
                    try {
                        val iface = (0 until h.device.interfaceCount)
                            .map { h.device.getInterface(it) }
                            .firstOrNull { it.id == h.hidIface }
                        if (iface != null) h.connection.releaseInterface(iface)
                        h.connection.close()
                    } catch (t: Throwable) { Log.w(TAG, "cm108.close on detach: $t") }
                    Log.i(TAG, "closeForDevice: cm108 released ${dev.deviceName}")
                    cm108 = null
                }
            }
        }
        synchronized(aiocLock) {
            aioc?.let { h ->
                if (h.device.deviceName == dev.deviceName) {
                    try { h.port.close() } catch (t: Throwable) { Log.w(TAG, "aioc.close on detach: $t") }
                    Log.i(TAG, "closeForDevice: aioc released ${dev.deviceName}")
                    aioc = null
                }
            }
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
            DeviceRole.AIOC    -> Thread({ openAioc(dev) }, "ptt-open-aioc").apply { isDaemon = true }.start()
            DeviceRole.UNKNOWN -> Log.w(TAG, "tryOpen on UNKNOWN device — skipping")
        }
    }

    private fun openCp2102n(dev: UsbDevice) = synchronized(cp2102nLock) {
        cp2102n?.let { prior ->
            if (prior.device.deviceName == dev.deviceName) {
                Log.i(TAG, "openCp2102n: already open on ${dev.deviceName}, skipping")
                return@synchronized
            }
            Log.i(TAG, "openCp2102n: replacing stale handle ${prior.device.deviceName} -> ${dev.deviceName}")
            try { prior.port.close() } catch (t: Throwable) { Log.w(TAG, "stale cp2102n.close: $t") }
            cp2102n = null
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
        cm108?.let { prior ->
            if (prior.device.deviceName == dev.deviceName) {
                Log.i(TAG, "openCm108: already open on ${dev.deviceName}, skipping")
                return@synchronized
            }
            Log.i(TAG, "openCm108: replacing stale handle ${prior.device.deviceName} -> ${dev.deviceName}")
            try {
                val priorIface = (0 until prior.device.interfaceCount)
                    .map { prior.device.getInterface(it) }
                    .firstOrNull { it.id == prior.hidIface }
                if (priorIface != null) prior.connection.releaseInterface(priorIface)
                prior.connection.close()
            } catch (t: Throwable) { Log.w(TAG, "stale cm108.close: $t") }
            cm108 = null
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

    /**
     * Dispatcher entry point called by the Rust modem via JNI on every PTT
     * actuation. Selects the transport by spec-Appendix-B method int and
     * delegates to the existing setRts / setAiocRts / setHidGpio private
     * helpers. VOX is a no-op (returns true; the audio path drives PTT).
     *
     * Returns true on success, false on dispatcher failure (no open transport,
     * unknown method int, or underlying transport returned false). The Rust
     * side propagates false as Err back into the TX governor.
     */
    override fun pttSet(method: Int, keyed: Boolean): Boolean {
        return when (method) {
            PttMethodConsts.PTT_METHOD_CP2102N_RTS -> setRts(keyed)
            PttMethodConsts.PTT_METHOD_AIOC_CDC_DTR -> setAiocRts(keyed)
            PttMethodConsts.PTT_METHOD_CM108_HID -> setHidGpio(keyed)
            PttMethodConsts.PTT_METHOD_VOX -> true
            else -> {
                Log.w(TAG, "pttSet unknown method=$method")
                false
            }
        }
    }

    /** Transport-keyed (not vendor-keyed) so the CM108 HID path can fan out
     *  to both Digirig and AIOC under the same name. WebView button labels
     *  retain the vendor name for operator clarity. */
    fun keyCp2102nRts(): Boolean = setRts(true)
    fun unkeyCp2102nRts(): Boolean = setRts(false)

    private fun setRts(state: Boolean): Boolean {
        val attempted = synchronized(cp2102nLock) {
            val h = cp2102n
            if (h == null) {
                Log.w(TAG, "setRts($state) but CP2102N not open")
                return@synchronized null
            }
            try {
                h.port.rts = state
                Log.i(TAG, "ptt: cp2102n_rts=$state")
                true
            } catch (t: Throwable) {
                Log.e(TAG, "setRts($state) failed: $t")
                // Stale handle (USB device went away under us, hub glitch,
                // bus reset). Drop it so the next enumerate() opens fresh.
                try { h.port.close() } catch (_: Throwable) { /* already broken */ }
                cp2102n = null
                false
            }
        }
        if (attempted == false) {
            // Re-enumerate so a still-attached CP2102N gets re-opened;
            // caller (Test PTT) can retry on the next click.
            Log.i(TAG, "setRts: scheduling re-enumeration after stale-handle failure")
            try { enumerate() } catch (t: Throwable) { Log.w(TAG, "re-enumerate threw: $t") }
        }
        return attempted ?: false
    }

    fun keyCm108Hid(): Boolean = setHidGpio(true)
    fun unkeyCm108Hid(): Boolean = setHidGpio(false)

    /**
     * Empirical CM108 GPIO bit search: AIOC firmware revisions have used
     * bits 0..3 over time. Allowed range 0..7. Out-of-range calls are no-ops
     * with a warn log. Setting the bit does not key — caller must follow
     * with keyCm108Hid().
     */
    fun setCm108Bit(pin: Int): Boolean {
        if (pin < 1 || pin > 8) {
            Log.w(TAG, "setCm108Bit($pin) out of range (1..8)")
            return false
        }
        cm108GpioBit = pin
        Log.i(TAG, "cm108_gpio_pin=$pin")
        return true
    }

    private fun setHidGpio(state: Boolean): Boolean {
        val attempted = setHidGpioLocked(state)
        if (attempted == false) {
            Log.i(TAG, "setHidGpio: scheduling re-enumeration after stale-handle failure")
            try { enumerate() } catch (t: Throwable) { Log.w(TAG, "re-enumerate threw: $t") }
        }
        return attempted ?: false
    }

    private fun setHidGpioLocked(state: Boolean): Boolean? = synchronized(cm108Lock) {
        val h = cm108 ?: run {
            Log.w(TAG, "setHidGpio($state) but CM108 not open")
            return@synchronized null
        }
        // Layout matches graywolf-modem/src/tx/ptt_cm108_unix.rs and
        // ptt_cm108_macos.rs (which key the AIOC successfully on desktop):
        //   byte 0 = HID_OR0  GPIO write mode (always 0)
        //   byte 1 = HID_OR1  GPIO output values
        //   byte 2 = HID_OR2  GPIO data direction (1=output)
        //   byte 3 = HID_OR3  SPDIF control (unused)
        // Pin is 1-indexed (datasheet GPIO3 -> pin 3 -> mask 0x04). Direction
        // mask MUST be set to put the pin in output mode; leaving it 0 leaves
        // the pin floating and the HID write is silently a no-op even though
        // controlTransfer returns rc=4. The HID report ID (0) is encoded in
        // the wValue (0x0200) of the SET_REPORT control transfer, not the
        // buffer payload — so the on-the-wire prefix length is 4, not 5.
        val pin = cm108GpioBit
        val mask: Byte = (1 shl (pin - 1)).toByte()
        val value: Byte = if (state) mask else 0
        val report = byteArrayOf(0x00, value, mask, 0x00)
        val rc = h.connection.controlTransfer(
            /* requestType = */ 0x21,
            /* request     = */ 0x09,
            /* value       = */ 0x0200,
            /* index       = */ h.hidIface,
            /* buffer      = */ report,
            /* length      = */ report.size,
            /* timeout_ms  = */ 200,
        )
        Log.i(TAG, "ptt: cm108_set_report pin=$pin mask=0x%02X value=0x%02X state=$state rc=$rc"
            .format(mask.toInt() and 0xFF, value.toInt() and 0xFF))
        if (rc < 0) {
            // Stale handle (USB device went away, hub glitch). Drop so a
            // re-enumerate opens fresh.
            try {
                val iface = (0 until h.device.interfaceCount)
                    .map { h.device.getInterface(it) }
                    .firstOrNull { it.id == h.hidIface }
                if (iface != null) h.connection.releaseInterface(iface)
                h.connection.close()
            } catch (_: Throwable) { /* already broken */ }
            cm108 = null
            return@synchronized false
        }
        return@synchronized rc == report.size
    }

    private fun openAioc(dev: UsbDevice) = synchronized(aiocLock) {
        aioc?.let { prior ->
            if (prior.device.deviceName == dev.deviceName) {
                Log.i(TAG, "openAioc: already open on ${dev.deviceName}, skipping")
                return@synchronized
            }
            Log.i(TAG, "openAioc: replacing stale handle ${prior.device.deviceName} -> ${dev.deviceName}")
            try { prior.port.close() } catch (t: Throwable) { Log.w(TAG, "stale aioc.close: $t") }
            aioc = null
        }
        try {
            Log.i(TAG, "openAioc: step=ctor")
            val driver: UsbSerialDriver = CdcAcmSerialDriver(dev)
            Log.i(TAG, "openAioc: step=openDevice")
            val conn: UsbDeviceConnection = usbManager.openDevice(dev) ?: run {
                Log.e(TAG, "openDevice returned null for AIOC — permission revoked?")
                return@synchronized
            }
            Log.i(TAG, "openAioc: step=ports count=${driver.ports.size}")
            val port = driver.ports.firstOrNull() ?: run {
                Log.e(TAG, "AIOC driver returned 0 ports")
                conn.close()
                return@synchronized
            }
            Log.i(TAG, "openAioc: step=port.open")
            port.open(conn)
            Log.i(TAG, "openAioc: step=setParameters")
            port.setParameters(9600, 8, UsbSerialPort.STOPBITS_1, UsbSerialPort.PARITY_NONE)
            // AIOC firmware >=1.2.0 PTT spec: assert when DTR=1 AND RTS=0.
            // Pre-set unkeyed (DTR=0). RTS held at 0 because the firmware
            // wants RTS=0 in the keyed state too; flipping it can be read
            // as "release" by some firmware revisions.
            Log.i(TAG, "openAioc: step=dtr=rts=false")
            port.dtr = false
            port.rts = false
            aioc = AiocHandle(dev, port)
            Log.i(TAG, "AIOC opened ${dev.deviceName}")
        } catch (t: Throwable) {
            Log.e(TAG, "openAioc failed: $t")
        }
    }

    /** AIOC PTT path is CDC-ACM RTS, NOT CM108 HID GPIO — the AIOC firmware
     *  accepts HID Set_Report but does not wire it to a GPIO output. */
    fun keyAiocCdcRts(): Boolean = setAiocRts(true)
    fun unkeyAiocCdcRts(): Boolean = setAiocRts(false)

    private fun setAiocRts(state: Boolean): Boolean {
        val attempted = synchronized(aiocLock) {
            val h = aioc
            if (h == null) {
                Log.w(TAG, "setAiocRts($state) but AIOC not open")
                return@synchronized null
            }
            try {
                // AIOC firmware >=1.2.0: PTT asserted on DTR=1 AND RTS=0.
                // RTS must stay 0 in BOTH key and unkey states — the firmware
                // releases PTT only when DTR drops to 0.
                h.port.rts = false
                h.port.dtr = state
                Log.i(TAG, "ptt: aioc_cdc dtr=$state rts=0")
                true
            } catch (t: Throwable) {
                Log.e(TAG, "setAiocRts($state) failed: $t")
                try { h.port.close() } catch (_: Throwable) { /* already broken */ }
                aioc = null
                false
            }
        }
        if (attempted == false) {
            Log.i(TAG, "setAiocRts: scheduling re-enumeration after stale-handle failure")
            try { enumerate() } catch (t: Throwable) { Log.w(TAG, "re-enumerate threw: $t") }
        }
        return attempted ?: false
    }

    /** Classify a device by vid/pid + structural fingerprint. */
    internal fun classify(dev: UsbDevice): DeviceRole {
        if (dev.vendorId == CP2102N_VID && dev.productId == CP2102N_PID) {
            return DeviceRole.CP2102N
        }
        if (dev.vendorId == AIOC_VID && dev.productId == AIOC_PID) {
            return DeviceRole.AIOC
        }
        if (dev.vendorId == DIGIRIG_CM108_VID && dev.productId == DIGIRIG_CM108_PID) {
            return DeviceRole.CM108
        }
        // Generic structural fingerprint for unknown vid/pids:
        //   audio + CDC-ACM   -> AIOC-class (RTS PTT)
        //   audio + HID only  -> CM108-class (HID GPIO PTT)
        var hasHid = false
        var hasAudio = false
        var hasCdc = false
        for (i in 0 until dev.interfaceCount) {
            when (dev.getInterface(i).interfaceClass) {
                UsbConstants.USB_CLASS_HID -> hasHid = true
                UsbConstants.USB_CLASS_AUDIO -> hasAudio = true
                UsbConstants.USB_CLASS_COMM -> hasCdc = true
            }
        }
        return when {
            hasAudio && hasCdc -> DeviceRole.AIOC
            hasAudio && hasHid -> DeviceRole.CM108
            else -> DeviceRole.UNKNOWN
        }
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
        val ai = aioc
        put("cp2102n_open", sp != null)
        put("cm108_open", cm != null)
        put("aioc_open", ai != null)
        put("cm108_hid_iface", cm?.hidIface ?: -1)
        put("cm108_gpio_bit", cm108GpioBit)
        cm?.let {
            put("cm108_vid", "0x%04X".format(it.device.vendorId))
            put("cm108_pid", "0x%04X".format(it.device.productId))
        }
        ai?.let {
            put("aioc_vid", "0x%04X".format(it.device.vendorId))
            put("aioc_pid", "0x%04X".format(it.device.productId))
        }
        var foundCp = false
        var foundCm = false
        var foundAioc = false
        if (this@UsbPttAdapter::usbManager.isInitialized) {
            for ((_, dev) in usbManager.deviceList) {
                when (classify(dev)) {
                    DeviceRole.CP2102N -> foundCp = true
                    DeviceRole.CM108   -> foundCm = true
                    DeviceRole.AIOC    -> foundAioc = true
                    DeviceRole.UNKNOWN -> {}
                }
            }
        }
        put("cp2102n_found", foundCp)
        put("cm108_found", foundCm)
        put("aioc_found", foundAioc)
    }

    /**
     * Snapshot of attached USB devices in JSON-array form for the WebView
     * channel-config UI. Each entry has vid/pid (hex strings), device name,
     * classified role, and current OS permission state.
     *
     * Different from status(): status() reports open-handle slots; this
     * reports the raw attached-device list regardless of role or handle state.
     */
    fun enumerateForJs(): JSONArray {
        check(this::appContext.isInitialized) { "init(context) must be called first" }
        val out = JSONArray()
        for ((_, dev) in usbManager.deviceList) {
            val role = classify(dev)
            out.put(JSONObject().apply {
                put("vid", dev.vendorId)                            // decimal Int — for requestUsbPermission()
                put("pid", dev.productId)                           // decimal Int — for requestUsbPermission()
                put("vid_hex", "0x%04X".format(dev.vendorId))      // hex string — for display
                put("pid_hex", "0x%04X".format(dev.productId))
                put("name", dev.productName ?: dev.deviceName)
                put("role", role.name)
                put("permission_granted", usbManager.hasPermission(dev))
            })
        }
        return out
    }

    /**
     * Request runtime permission for the first attached device matching
     * (vid, pid). Fires [onResult] exactly once: immediately with `false`
     * if no matching device is attached; immediately with `true` if
     * permission is already granted (and triggers tryOpen); otherwise
     * asynchronously when the system permission broadcast lands.
     */
    fun requestPermissionFor(vid: Int, pid: Int, onResult: (Boolean) -> Unit) {
        check(this::appContext.isInitialized) { "init(context) must be called first" }
        val device = usbManager.deviceList.values
            .firstOrNull { it.vendorId == vid && it.productId == pid }
        if (device == null) {
            Log.w(TAG, "requestPermissionFor: no device vid=$vid pid=$pid attached")
            onResult(false)
            return
        }
        if (usbManager.hasPermission(device)) {
            // Already granted; open the device and notify the caller.
            tryOpen(device)
            onResult(true)
            return
        }
        // Register one-shot callback consumed by permissionReceiver on broadcast.
        pendingPermissionCallbacks[device.deviceName] = onResult
        requestPermission(device)
    }

    enum class DeviceRole { CP2102N, CM108, AIOC, UNKNOWN }
}
