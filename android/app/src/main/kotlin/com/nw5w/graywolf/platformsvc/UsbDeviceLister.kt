package com.nw5w.graywolf.platformsvc

import android.hardware.usb.UsbDevice
import android.hardware.usb.UsbManager
import android.util.Log
import com.nw5w.graywolf.platformproto.UsbClass
import com.nw5w.graywolf.platformproto.UsbDevice as ProtoUsbDevice

private const val TAG = "UsbDeviceLister"

/**
 * Maps Android `UsbManager.deviceList` into the proto UsbDevice rows
 * the Go side expects when handling /api/ptt/available on Android.
 *
 * vid/pid/product/manufacturer/devicePath are exposed without USB
 * permission; the operator grants permission later (when the dialog's
 * "Request permission" CTA fires) so the chosen device can actually be
 * opened. Serial numbers DO require permission and are intentionally
 * not surfaced here.
 *
 * Class filter: UNKNOWN means no filter (return everything). Any other
 * value keeps devices that advertise that class either at the device
 * level or on one of their interfaces.
 */
internal object UsbDeviceLister {
    fun list(usbManager: UsbManager?, classFilter: UsbClass): List<ProtoUsbDevice> {
        if (usbManager == null) {
            Log.w(TAG, "list called with null UsbManager; returning empty")
            return emptyList()
        }
        val all = usbManager.deviceList.values.toList()
        Log.i(TAG, "list classFilter=$classFilter attached=${all.size}")
        val out = all.mapNotNull { dev ->
            val classes = deviceClasses(dev)
            val skipped = classFilter != UsbClass.USB_CLASS_UNKNOWN && classFilter !in classes
            Log.d(TAG, "  ${dev.deviceName} vid=0x${"%04X".format(dev.vendorId)} " +
                "pid=0x${"%04X".format(dev.productId)} product=\"${dev.productName ?: ""}\" " +
                "classes=$classes skipped=$skipped")
            if (skipped) return@mapNotNull null
            ProtoUsbDevice.newBuilder()
                .setVid(dev.vendorId)
                .setPid(dev.productId)
                .setProduct(dev.productName ?: "")
                .setManufacturer(dev.manufacturerName ?: "")
                .setDevicePath(dev.deviceName ?: "")
                .addAllClasses(classes)
                .build()
        }
        Log.i(TAG, "list returning ${out.size} device(s)")
        return out
    }

    private fun deviceClasses(dev: UsbDevice): List<UsbClass> {
        val seen = LinkedHashSet<UsbClass>()
        mapUsbClass(dev.deviceClass)?.let(seen::add)
        for (i in 0 until dev.interfaceCount) {
            mapUsbClass(dev.getInterface(i).interfaceClass)?.let(seen::add)
        }
        return seen.toList()
    }

    /**
     * Android USB class codes per the USB-IF spec. We only enumerate
     * the four classes the proto schema knows; anything else (mass
     * storage, printer, etc.) is irrelevant to PTT detection.
     */
    private fun mapUsbClass(usbClassCode: Int): UsbClass? = when (usbClassCode) {
        1 -> UsbClass.USB_CLASS_AUDIO
        2 -> UsbClass.USB_CLASS_CDC_ACM
        3 -> UsbClass.USB_CLASS_HID
        255 -> UsbClass.USB_CLASS_VENDOR
        else -> null
    }
}
