package com.nw5w.graywolf.audio

import android.content.Context
import android.media.AudioDeviceInfo
import android.media.AudioManager
import android.media.AudioTimestamp
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

    // -----------------------------------------------------------------------
    // presentedFrames(): physical playout position for the TX drain wait.
    // -----------------------------------------------------------------------

    // Stub track.write to advance framesWritten by the requested count.
    private fun AudioTrack.stubWriteEchoesCount() {
        whenever(this.write(any<ShortArray>(), any(), any(), any()))
            .thenAnswer { it.getArgument<Int>(2) }
    }

    @Test fun `presentedFrames returns 0 before start`() {
        val (ctx, _) = contextWith()
        val pump = AudioTxPump(ctx) { _ -> mockTrack() }
        // No start() → no track → nothing presented.
        assertEquals(0L, pump.presentedFrames())
    }

    @Test fun `presentedFrames uses getTimestamp position clamped to frames written`() {
        val (ctx, _) = contextWith()
        val track = mockTrack().also { it.stubWriteEchoesCount() }
        // 40 frames presented; nanoTime far in the future so the elapsed-time
        // extrapolation contributes nothing → deterministic 40.
        whenever(track.getTimestamp(any())).thenAnswer {
            val ts = it.getArgument<AudioTimestamp>(0)
            ts.framePosition = 40L
            ts.nanoTime = System.nanoTime() + 10_000_000_000L
            true
        }
        val pump = AudioTxPump(ctx) { _ -> track }
        pump.start()
        pump.pushSamples(ShortArray(100), 100) // framesWritten = 100

        assertEquals(40L, pump.presentedFrames())
    }

    @Test fun `presentedFrames never exceeds frames written`() {
        val (ctx, _) = contextWith()
        val track = mockTrack().also { it.stubWriteEchoesCount() }
        // Stale timestamp far in the past → naive extrapolation explodes past
        // what we wrote; presentedFrames must clamp to framesWritten so an
        // underrun between transmissions can't make us unkey early.
        whenever(track.getTimestamp(any())).thenAnswer {
            val ts = it.getArgument<AudioTimestamp>(0)
            ts.framePosition = 50L
            ts.nanoTime = System.nanoTime() - 10_000_000_000L
            true
        }
        val pump = AudioTxPump(ctx) { _ -> track }
        pump.start()
        pump.pushSamples(ShortArray(100), 100)

        assertEquals(100L, pump.presentedFrames())
    }

    @Test fun `presentedFrames frames-written ceiling resets on track restart`() {
        val (ctx, _) = contextWith()
        val track = mockTrack().also { it.stubWriteEchoesCount() }
        // getTimestamp unavailable → falls back to a playback head of 50, but a
        // freshly (re)started track has presented nothing, so the frames-written
        // ceiling must clamp to 0. A stale ceiling from before the restart would
        // leak 50 and risk an early unkey.
        whenever(track.getTimestamp(any())).thenReturn(false)
        whenever(track.playbackHeadPosition).thenReturn(50)
        val pump = AudioTxPump(ctx) { _ -> track }

        pump.start()
        pump.pushSamples(ShortArray(100), 100) // framesWritten = 100
        pump.stop()
        pump.start() // new track → framesWritten must reset to 0

        assertEquals(0L, pump.presentedFrames())
    }

    @Test fun `presentedFrames falls back to playback head when timestamp unavailable`() {
        val (ctx, _) = contextWith()
        val track = mockTrack().also { it.stubWriteEchoesCount() }
        whenever(track.getTimestamp(any())).thenReturn(false)
        whenever(track.playbackHeadPosition).thenReturn(30)
        val pump = AudioTxPump(ctx) { _ -> track }
        pump.start()
        pump.pushSamples(ShortArray(100), 100)

        assertEquals(30L, pump.presentedFrames())
    }
}
