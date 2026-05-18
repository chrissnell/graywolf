package com.nw5w.graywolf.audio

import android.content.Context
import android.media.AudioAttributes
import android.media.AudioDeviceCallback
import android.media.AudioDeviceInfo
import android.media.AudioFormat
import android.media.AudioManager
import android.media.AudioTrack
import android.util.Log
import com.nw5w.graywolf.jni.AudioTxCallback

/**
 * Streaming AudioTrack TX pump. Symmetric to AudioPump (RX). Stays in PLAY
 * state from Service boot; PCM only flows when the Rust modem TX governor
 * pushes samples via pushSamples(). Auto-routes to the first USB audio output;
 * falls back to system default if none is found. Hot-swap is handled via an
 * AudioManager.AudioDeviceCallback registered in start().
 *
 * Pass an Application context to avoid leaking the Service.
 */
class AudioTxPump(
    private val appContext: Context,
    // Internal factory hook for unit tests. Production callers leave it null.
    private val trackFactory: ((Int) -> AudioTrack)? = null,
) : AudioTxCallback {

    @Volatile private var track: AudioTrack? = null
    @Volatile private var routedDevice: String = "<none>"

    private val am: AudioManager by lazy {
        appContext.getSystemService(AudioManager::class.java)
    }

    /** USB audio dongles enumerate as one of three AudioDeviceInfo types
     *  depending on their descriptor (raw class-1 device, USB-Audio headset,
     *  USB-Audio accessory). Match all three — Digirig presents as
     *  TYPE_USB_HEADSET, AIOC as TYPE_USB_DEVICE. */
    private fun isUsbAudioOutput(d: AudioDeviceInfo): Boolean =
        d.type == AudioDeviceInfo.TYPE_USB_DEVICE ||
        d.type == AudioDeviceInfo.TYPE_USB_HEADSET ||
        d.type == AudioDeviceInfo.TYPE_USB_ACCESSORY

    private val deviceCallback = object : AudioDeviceCallback() {
        override fun onAudioDevicesAdded(addedDevices: Array<out AudioDeviceInfo>) {
            val usbOut = addedDevices.firstOrNull { it.isSink && isUsbAudioOutput(it) }
                ?: return
            val t = track ?: return
            t.setPreferredDevice(usbOut)
            routedDevice = usbOut.productName?.toString() ?: "USB device"
            Log.i(TAG, "AudioTxPump hot-swap: routed to USB output: $routedDevice")
        }

        override fun onAudioDevicesRemoved(removedDevices: Array<out AudioDeviceInfo>) {
            val t = track ?: return
            // preferredDevice is null when we never explicitly routed (e.g. boot with no
            // USB device present); in that case there's nothing to unwire here.
            val current = t.preferredDevice ?: return
            val removed = removedDevices.any { it.id == current.id }
            if (removed) {
                t.setPreferredDevice(null)
                routedDevice = "system default (USB audio dongle removed)"
                Log.w(TAG, "AudioTxPump hot-swap: $routedDevice")
            }
        }
    }

    fun start(sampleRate: Int = 22050) {
        if (track != null) return

        val t: AudioTrack = if (trackFactory != null) {
            trackFactory.invoke(sampleRate)
        } else {
            val bufBytes = AudioTrack.getMinBufferSize(
                sampleRate,
                AudioFormat.CHANNEL_OUT_MONO,
                AudioFormat.ENCODING_PCM_16BIT,
            ) * 4
            check(bufBytes > 0) { "AudioTrack.getMinBufferSize=$bufBytes" }

            AudioTrack.Builder()
                .setAudioAttributes(
                    AudioAttributes.Builder()
                        .setUsage(AudioAttributes.USAGE_MEDIA)
                        .setContentType(AudioAttributes.CONTENT_TYPE_MUSIC)
                        .build()
                )
                .setAudioFormat(
                    AudioFormat.Builder()
                        .setEncoding(AudioFormat.ENCODING_PCM_16BIT)
                        .setSampleRate(sampleRate)
                        .setChannelMask(AudioFormat.CHANNEL_OUT_MONO)
                        .build()
                )
                .setBufferSizeInBytes(bufBytes)
                .setTransferMode(AudioTrack.MODE_STREAM)
                .build()
        }

        // Auto-route to first USB audio output (TYPE_USB_DEVICE / USB_HEADSET /
        // USB_ACCESSORY — see isUsbAudioOutput).
        val usbOut = am.getDevices(AudioManager.GET_DEVICES_OUTPUTS)
            .firstOrNull { isUsbAudioOutput(it) }
        if (usbOut != null) {
            t.setPreferredDevice(usbOut)
            routedDevice = usbOut.productName?.toString() ?: "USB device"
            Log.i(TAG, "AudioTxPump routed to USB output: $routedDevice")
        } else {
            routedDevice = "system default (no USB audio dongle found)"
            Log.w(TAG, "AudioTxPump: $routedDevice")
        }

        t.play()
        track = t

        // Register for hot-swap notifications.
        am.registerAudioDeviceCallback(deviceCallback, null)

        Log.i(TAG, "AudioTxPump init rate=$sampleRate routed=$routedDevice")
    }

    /**
     * Called from Rust modem via JNI on every TX PCM frame.
     * Blocking write — Rust modem TX thread is OK to block while audio drains.
     * Returns -1 if the pump is stopped.
     */
    override fun pushSamples(samples: ShortArray, count: Int): Int {
        val t = track ?: return -1
        return t.write(samples, 0, count, AudioTrack.WRITE_BLOCKING)
    }

    fun stop() {
        val t = track ?: return
        am.unregisterAudioDeviceCallback(deviceCallback)
        try {
            try { t.stop() } catch (_: Throwable) {}
            try { t.release() } catch (_: Throwable) {}
        } finally {
            track = null
        }
        Log.i(TAG, "AudioTxPump stopped")
    }

    companion object {
        private const val TAG = "AudioTxPump"
    }
}
