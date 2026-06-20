package com.nw5w.graywolf.audio

import kotlin.math.PI
import kotlin.math.abs
import kotlin.math.sin
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ToneOscillatorTest {
    @Test fun startsAtZeroCrossing() {
        val osc = ToneOscillator(sampleRate = 22050)
        osc.setFrequency(1200)
        assertEquals(0f, osc.next(), 1e-4f)
    }

    @Test fun matchesSineAtPeakLevel() {
        val sr = 22050
        val hz = 1200
        val osc = ToneOscillator(sampleRate = sr)
        osc.setFrequency(hz)
        osc.next() // consume sample 0 (phase 0)
        val expected = (ToneOscillator.PEAK * sin(2.0 * PI * hz / sr)).toFloat()
        assertEquals(expected, osc.next(), 1e-3f)
    }

    @Test fun resetReturnsToZeroCrossing() {
        val osc = ToneOscillator(sampleRate = 22050)
        osc.setFrequency(1200)
        repeat(5) { osc.next() }
        osc.reset()
        assertEquals(0f, osc.next(), 1e-4f)
    }

    @Test fun toI16ClampsNearPeakLevel() {
        val osc = ToneOscillator(sampleRate = 22050)
        osc.setFrequency(1200)
        var maxAbs = 0
        repeat(22050) {
            val v = abs(osc.nextI16().toInt())
            if (v > maxAbs) maxAbs = v
        }
        // ~0.6 * 32767 ≈ 19660, allowing for sampling not hitting the exact peak.
        assertTrue("peak $maxAbs should be near 0.6 FS", maxAbs in 19000..20500)
    }

    @Test fun zeroFrequencyIsSilent() {
        val osc = ToneOscillator(sampleRate = 22050)
        osc.setFrequency(0)
        repeat(100) { assertEquals(0f, osc.next(), 1e-6f) }
    }
}
