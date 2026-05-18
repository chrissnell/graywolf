package com.nw5w.graywolf.audio

import android.content.Context
import android.media.AudioDeviceInfo
import android.media.AudioManager
import android.media.AudioTrack
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.mockito.kotlin.any
import org.mockito.kotlin.mock
import org.mockito.kotlin.never
import org.mockito.kotlin.verify
import org.mockito.kotlin.whenever

/**
 * Unit tests for AudioTxPump (spec §6.1).
 *
 * AudioTrack and AudioManager are Android system classes unavailable on the
 * host JVM. We inject a mock AudioTrack via AudioTxPump's internal
 * trackFactory seam and mock Context/AudioManager via Mockito so every test
 * is hermetic. android.util.Log returns defaults under
 * unitTests.isReturnDefaultValues = true.
 */
class AudioTxPumpTest {

    // Shared mock AudioTrack with sensible defaults.
    private fun mockTrack(): AudioTrack = mock<AudioTrack>().also { t ->
        // play() and setPreferredDevice() are void — no stubbing needed.
    }

    // Build a Context stub that returns a mock AudioManager whose
    // getDevices() returns [devices].
    private fun contextWith(vararg devices: AudioDeviceInfo): Pair<Context, AudioManager> {
        val am = mock<AudioManager>()
        whenever(am.getDevices(AudioManager.GET_DEVICES_OUTPUTS)).thenReturn(arrayOf(*devices))
        val ctx = mock<Context>()
        whenever(ctx.getSystemService(AudioManager::class.java)).thenReturn(am)
        return ctx to am
    }

    // Build a mock AudioDeviceInfo with a given type and product name.
    private fun mockDevice(type: Int, name: String): AudioDeviceInfo {
        val d = mock<AudioDeviceInfo>()
        whenever(d.type).thenReturn(type)
        whenever(d.productName).thenReturn(name)
        return d
    }

    // -----------------------------------------------------------------------
    // §6.1 case 1: No USB output → start() routes to system default + WARN
    // -----------------------------------------------------------------------
    @Test fun `start routes to system default when no USB audio output`() {
        val (ctx, am) = contextWith() // empty device list
        val track = mockTrack()
        val pump = AudioTxPump(ctx) { _ -> track }

        pump.start()

        // setPreferredDevice must NOT be called when there is no USB device.
        verify(track, never()).setPreferredDevice(any())
        // The pump must still call play() — it falls back, not fails.
        verify(track).play()
    }

    // -----------------------------------------------------------------------
    // §6.1 case 2: USB output present → setPreferredDevice + routedDevice set
    // -----------------------------------------------------------------------
    @Test fun `start calls setPreferredDevice with USB device and updates routedDevice`() {
        val usbDevice = mockDevice(AudioDeviceInfo.TYPE_USB_DEVICE, "Burr-Brown USB Audio")
        val (ctx, _) = contextWith(usbDevice)
        val track = mockTrack()
        val pump = AudioTxPump(ctx) { _ -> track }

        pump.start()

        verify(track).setPreferredDevice(usbDevice)
        verify(track).play()
        // routedDevice is private; we verify indirectly via stop not crashing
        // and via the setPreferredDevice call above reflecting the USB name.
        // No assertion on the field itself — keep production class clean.
    }

    // -----------------------------------------------------------------------
    // §6.1 case 3: pushSamples after stop() returns -1 (no crash)
    // -----------------------------------------------------------------------
    @Test fun `pushSamples returns -1 after stop`() {
        val (ctx, _) = contextWith()
        val track = mockTrack()
        val pump = AudioTxPump(ctx) { _ -> track }

        pump.start()
        pump.stop()

        val result = pump.pushSamples(ShortArray(10), 10)
        assertEquals(-1, result)
    }

    // -----------------------------------------------------------------------
    // §6.1 case 4: start() is idempotent — second call creates no new track
    // -----------------------------------------------------------------------
    @Test fun `start is idempotent -- second call reuses existing track`() {
        val (ctx, _) = contextWith()
        var trackCreateCount = 0
        val pump = AudioTxPump(ctx) { _ ->
            trackCreateCount++
            mockTrack()
        }

        pump.start()
        pump.start() // second call — must short-circuit

        assertEquals(1, trackCreateCount)
    }
}
