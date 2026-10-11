package com.nw5w.graywolf.audio

import android.content.Context
import android.media.AudioAttributes
import android.media.AudioDeviceCallback
import android.media.AudioDeviceInfo
import android.media.AudioFormat
import android.media.AudioManager
import android.media.AudioTimestamp
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
    // Cumulative frames (not shorts: one stereo frame = 2 shorts) handed to
    // AudioTrack.write on the current track — the ceiling for
    // presentedFrames() so getTimestamp extrapolation can't over-report.
    // Reset whenever a new track is built.
    @Volatile private var framesWritten: Long = 0L
    // Reused across drain polls; getTimestamp fills it in place.
    private val timestamp = AudioTimestamp()

    // Digirig Lite tone-PTT state. When `stereo` is true the output track is
    // CHANNEL_OUT_STEREO and each mono AFSK sample becomes a frame
    // {L=AFSK, R=tone-or-silence}. Driven by setTone() off the Rust TX thread.
    @Volatile private var sampleRate: Int = 22050
    @Volatile private var stereo: Boolean = false
    @Volatile private var toneActive: Boolean = false
    @Volatile private var toneHz: Int = 0
    private var osc: ToneOscillator? = null
    // Reused interleave scratch (written only on the TX/write thread).
    private var stereoScratch: ShortArray = ShortArray(0)

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
        this.sampleRate = sampleRate
        if (stereo && osc == null) osc = ToneOscillator(sampleRate)
        // New track → playback head restarts at 0, so the presentedFrames
        // ceiling must too.
        framesWritten = 0L

        val t = buildTrack(stereo)
        routeToUsb(t)
        t.play()
        track = t

        // Register for hot-swap notifications.
        am.registerAudioDeviceCallback(deviceCallback, null)

        Log.i(TAG, "AudioTxPump init rate=$sampleRate stereo=$stereo routed=$routedDevice")
    }

    /** Build a streaming AudioTrack with the requested channel layout. The
     *  test seam (trackFactory) ignores the layout and returns a mock. */
    private fun buildTrack(stereo: Boolean): AudioTrack {
        val tf = trackFactory
        if (tf != null) return tf.invoke(sampleRate)

        val channelMask =
            if (stereo) AudioFormat.CHANNEL_OUT_STEREO else AudioFormat.CHANNEL_OUT_MONO
        val bufBytes = AudioTrack.getMinBufferSize(
            sampleRate,
            channelMask,
            AudioFormat.ENCODING_PCM_16BIT,
        ) * 4
        check(bufBytes > 0) { "AudioTrack.getMinBufferSize=$bufBytes" }

        val t = AudioTrack.Builder()
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
                    .setChannelMask(channelMask)
                    .build()
            )
            .setBufferSizeInBytes(bufBytes)
            .setTransferMode(AudioTrack.MODE_STREAM)
            .build()
        // Digirig Lite tone PTT needs a real stereo track to separate the
        // keying tone (right) from the AFSK (left). If the device could only
        // give us mono, the tone can't be emitted and the radio won't key —
        // surface that instead of failing silently. (A HAL-level downmix on a
        // 2ch track won't show here; that's covered by the on-device gate.)
        if (stereo && t.channelCount < 2) {
            Log.e(TAG, "AudioTxPump: requested stereo but got ${t.channelCount}ch — " +
                "Digirig tone PTT cannot key the radio on this output device")
        }
        return t
    }

    /** Auto-route a track to the first USB audio output, else system default. */
    private fun routeToUsb(t: AudioTrack) {
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
    }

    /**
     * Rust → Kotlin tone control (Digirig Lite tone PTT). A non-zero `hz` that
     * differs from the current one rebuilds the track as stereo so the right
     * channel can carry the keying tone; `hz == 0` rebuilds as mono. `active`
     * gates whether the tone is emitted during the next transmission. Called
     * off the modem TX thread.
     */
    @Synchronized
    override fun setTone(active: Boolean, hz: Int) {
        if (hz != toneHz) {
            toneHz = hz
            val wantStereo = hz > 0
            if (wantStereo != stereo && track != null) {
                // Layout must change on a live track — rebuild it.
                rebuildTrack(wantStereo)
            } else {
                // No live track yet (pre-warm before start): record the layout
                // so start() builds the right one.
                stereo = wantStereo
            }
        }
        // Keep the oscillator consistent with the layout regardless of path.
        if (stereo && osc == null) osc = ToneOscillator(sampleRate)
        if (!stereo) osc = null
        if (active && !toneActive) osc?.reset()
        toneActive = active
        osc?.setFrequency(hz)
        Log.i(TAG, "setTone active=$active hz=$hz stereo=$stereo")
    }

    @Synchronized
    private fun rebuildTrack(wantStereo: Boolean) {
        val old = track
        if (old != null) {
            try { old.stop() } catch (_: Throwable) {}
            try { old.release() } catch (_: Throwable) {}
        }
        stereo = wantStereo
        osc = if (wantStereo) ToneOscillator(sampleRate) else null
        val t = buildTrack(wantStereo)
        routeToUsb(t)
        t.play()
        track = t
        // New track → playback head restarts at 0, so the presentedFrames
        // ceiling must too.
        framesWritten = 0L
        Log.i(TAG, "AudioTxPump rebuilt stereo=$wantStereo routed=$routedDevice")
    }

    /**
     * Called from Rust modem via JNI on every TX PCM frame.
     * Blocking write — Rust modem TX thread is OK to block while audio drains.
     * Returns the number of mono input samples (= output frames) written, in
     * both mono and stereo layouts, so the Rust drain watermark and
     * [presentedFrames] share a unit; negative AudioTrack error codes pass
     * through. Returns -1 if the pump is stopped.
     *
     * `@Synchronized` on the same monitor as [setTone]/[rebuildTrack]/[stop]:
     * key/unkey and pushSamples already serialise on the single TX-worker
     * thread, but the `ConfigurePtt` pre-warm calls setTone from the *IPC
     * thread*. Without this lock that pre-warm could `stop()`/`release()` the
     * AudioTrack mid-write (use-after-release / native crash) if a PTT
     * reconfigure overlapped an in-flight transmission. Holding the monitor
     * across the blocking write only stalls that rare cross-thread pre-warm,
     * never the TX path itself. It also publishes a consistent
     * track/stereo/osc snapshot to this thread.
     */
    @Synchronized
    override fun pushSamples(samples: ShortArray, count: Int): Int {
        val t = track ?: return -1
        if (!stereo) {
            val written = t.write(samples, 0, count, AudioTrack.WRITE_BLOCKING)
            if (written > 0) framesWritten += written
            return written
        }
        // Stereo (Digirig Lite tone PTT): L = AFSK, R = keying tone (while
        // active) or silence (idle). The radio keys on the right channel.
        if (stereoScratch.size < count * 2) stereoScratch = ShortArray(count * 2)
        val o = osc
        val emitTone = toneActive && o != null
        for (i in 0 until count) {
            stereoScratch[i * 2] = samples[i]
            stereoScratch[i * 2 + 1] = if (emitTone) o!!.nextI16() else 0
        }
        val written = t.write(stereoScratch, 0, count * 2, AudioTrack.WRITE_BLOCKING)
        if (written < 0) return written
        // write() counts shorts (2 per stereo frame); report frames so the
        // Rust drain watermark is in the same unit as presentedFrames().
        val frames = written / 2
        framesWritten += frames
        return frames
    }

    /**
     * Frames physically presented at the output. Primary source is
     * AudioTrack.getTimestamp (its framePosition/nanoTime reference the output,
     * so extrapolating to now accounts for the full HAL→USB latency). When the
     * timestamp is unavailable (common right after play() and on some USB
     * stacks) we fall back to the mixer playback head, which excludes that
     * latency but never over-reports. The result is clamped to framesWritten so
     * a stale timestamp during an inter-transmission underrun can't extrapolate
     * past what we actually wrote and make the Rust drain loop unkey early.
     *
     * `@Synchronized` with [pushSamples]/[setTone]/[rebuildTrack]/[stop] so a
     * cross-thread tone pre-warm can't release the track mid-query. Any
     * Throwable is swallowed and reported as 0: an exception escaping into the
     * JNI downcall would be left pending on the Rust TX thread, and 0 only
     * makes the drain loop hold PTT longer (bounded by its timeout).
     */
    @Synchronized
    override fun presentedFrames(): Long {
        val t = track ?: return 0L
        return try {
            val written = framesWritten
            val raw: Long = if (t.getTimestamp(timestamp)) {
                val elapsedNs = System.nanoTime() - timestamp.nanoTime
                val extra = if (elapsedNs > 0) elapsedNs * sampleRate / 1_000_000_000L else 0L
                timestamp.framePosition + extra
            } else {
                // Int frame counter; mask to unsigned 32-bit for the wrap boundary.
                t.playbackHeadPosition.toLong() and 0xFFFF_FFFFL
            }
            minOf(raw, written)
        } catch (e: Throwable) {
            Log.w(TAG, "presentedFrames failed: $e")
            0L
        }
    }

    @Synchronized
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
