package com.nw5w.graywolf.audio

import kotlin.math.PI
import kotlin.math.sin

/**
 * Steady-sine generator for the Digirig Lite keying tone. Mirrors the Rust
 * desktop `PttTone` (audio/soundcard.rs): a phase accumulator advanced one
 * frame per sample, emitting ~0.6 FS so the Digirig Lite tone detector trips
 * reliably (a touch hotter than the ~0.5 FS AFSK). The tone keys the radio's
 * PTT detector, not the audio path, so it is intentionally not scaled by the
 * channel output gain. Phase starts and resets at a zero crossing so
 * back-to-back transmissions do not accumulate an offset.
 *
 * Not thread-safe: only the AudioTxPump write thread advances it.
 */
class ToneOscillator(private val sampleRate: Int) {
    private var phase = 0.0          // radians, wrapped to [0, 2π)
    private var phaseInc = 0.0       // 2π·freq / sampleRate

    fun setFrequency(hz: Int) {
        phaseInc = if (hz > 0) 2.0 * PI * hz / sampleRate else 0.0
    }

    /** Next float sample in [-PEAK, PEAK], advancing the phase by one frame. */
    fun next(): Float {
        val s = (PEAK * sin(phase)).toFloat()
        phase += phaseInc
        if (phase >= 2.0 * PI) phase -= 2.0 * PI
        return s
    }

    /** Next sample as PCM16, clamped to the i16 range. */
    fun nextI16(): Short {
        val v = next() * Short.MAX_VALUE
        return v.coerceIn(Short.MIN_VALUE.toFloat(), Short.MAX_VALUE.toFloat()).toInt().toShort()
    }

    /** Restart at a zero crossing (call between transmissions). */
    fun reset() {
        phase = 0.0
    }

    companion object {
        /** Peak amplitude as a fraction of full scale (matches PttTone). */
        const val PEAK = 0.6f
    }
}
