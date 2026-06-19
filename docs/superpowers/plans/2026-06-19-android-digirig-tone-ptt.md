# Android Digirig Lite Tone PTT Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an Android operator pick "Digirig Lite (tone PTT)" for a channel so the radio keys from a `mark_freq` tone on the right audio channel while the AFSK packet plays on the left — with no PTT wire configured.

**Architecture:** The Rust modem keeps pushing **mono** AFSK to the Kotlin `AudioTxPump`. When the channel uses tone PTT, `AudioTxPump` runs a **stereo** `AudioTrack` and synthesises the keying tone on the right channel (a Kotlin port of desktop's `PttTone`), with AFSK duplicated to the left. The tone is gated on at PTT-key and off at PTT-unkey by reusing the TX governor's existing key/unkey bracketing, routed through a new `setTone(active, hz)` JNI upcall on the already-installed `AudioTxCallback`. A new `PTT_METHOD_DIGIRIG_TONE = 5` carries the selection end to end. The mono JNI sample contract and the governor's drain-timing math are unchanged.

**Tech Stack:** Rust (`graywolf-modem`, JNI via `jni` 0.21, `android-test-stub` host tests), Kotlin (`AudioTrack`, JUnit), Protobuf, SvelteKit (Vitest), Go (`pkg/webapi/dto`).

**Design reference:** `docs/superpowers/specs/2026-06-19-android-digirig-tone-ptt-design.md`. Option B (Kotlin sink synthesises the tone) is chosen there; this plan implements it.

---

## Why the tone is synthesised in Kotlin, not interleaved in Rust

`modem/tx_worker.rs::drive_tx_cycle` computes the expected play duration as
`samples.len() / sample_rate` and holds PTT until that elapses. If Rust
interleaved mono→stereo (2N samples) before building the job, `expected` would
double and the radio would stay keyed twice as long. Pushing **mono** (N
samples) and letting the Kotlin sink write N **stereo frames** to a stereo
`AudioTrack` plays for the correct N/sample_rate duration while `expected` stays
correct. Hence: Rust stays mono; the sink goes stereo.

## File Structure

| File | Responsibility | Change |
|---|---|---|
| `proto/platform.proto` | `PttMethod` enum | add value `5` |
| `proto/graywolf.proto` | `ConfigurePtt.ptt_method` doc | note 1..5 |
| `graywolf-modem/src/tx/ptt_android_consts.rs` | Rust method ints | add const + test |
| `android/app/src/main/kotlin/com/nw5w/graywolf/usb/PttMethodConsts.kt` | Kotlin method ints | add const |
| `graywolf-modem/src/android/upcall.rs` | JNI upcalls | add `setTone` upcall + mock + tests |
| `graywolf-modem/src/lib.rs` | crate JNI re-exports | re-export `jni_audio_set_tone` + mock |
| `graywolf-modem/src/jni/ModemBridge` (Kotlin) | callback interface | add `setTone` to `AudioTxCallback` |
| `android/.../audio/ToneOscillator.kt` | sine generator | **new file** |
| `android/.../audio/AudioTxPump.kt` | stereo tone mode | add `setTone`, stereo render |
| `graywolf-modem/src/tx/ptt_android.rs` | Android PTT driver | tone branch in key/unkey |
| `graywolf-modem/src/tx/ptt.rs` | `build_driver` | accept method `5` |
| `graywolf-modem/src/android/config_state.rs` | per-channel flag | add `digirig_tone` flag |
| `graywolf-modem/src/android/mod.rs` | TX/PTT arms | lead-in + pre-warm |
| `web/src/routes/ptt/devices/methodOptions.android.js` | picker | add entry |
| `pkg/webapi/dto/ptt.go` | validation | allow `5` for `android` |
| `docs/handbook/ptt.html`, `docs/wiki/` | operator + topology docs | document method |

---

## Task 1: Add `PTT_METHOD_DIGIRIG_TONE = 5` constant across proto / Rust / Kotlin

**Files:**
- Modify: `proto/platform.proto:134-140`
- Modify: `proto/graywolf.proto:217-225` (comment only)
- Modify: `graywolf-modem/src/tx/ptt_android_consts.rs`
- Modify: `android/app/src/main/kotlin/com/nw5w/graywolf/usb/PttMethodConsts.kt`

- [ ] **Step 1: Extend the Rust appendix-B test (failing)**

In `graywolf-modem/src/tx/ptt_android_consts.rs`, add an assertion to the existing test:

```rust
    #[test]
    fn ptt_method_constants_match_spec_appendix_b() {
        assert_eq!(PTT_METHOD_UNKNOWN, 0);
        assert_eq!(PTT_METHOD_CP2102N_RTS, 1);
        assert_eq!(PTT_METHOD_CM108_HID, 2);
        assert_eq!(PTT_METHOD_AIOC_CDC_DTR, 3);
        assert_eq!(PTT_METHOD_VOX, 4);
        assert_eq!(PTT_METHOD_DIGIRIG_TONE, 5);
    }
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd graywolf-modem && cargo test --features android-test-stub ptt_method_constants_match_spec_appendix_b`
Expected: FAIL — `cannot find value PTT_METHOD_DIGIRIG_TONE in this scope`.

- [ ] **Step 3: Add the constant**

In `graywolf-modem/src/tx/ptt_android_consts.rs`, after the `PTT_METHOD_VOX` line:

```rust
/// proto `PTT_METHOD_VOX = 4` — no PTT wire; audio drives VOX
pub const PTT_METHOD_VOX: i32 = 4;
/// proto `PTT_METHOD_DIGIRIG_TONE = 5` — no PTT wire; a right-channel tone
/// keys the Digirig Lite while AFSK plays on the left (Android tone PTT).
pub const PTT_METHOD_DIGIRIG_TONE: i32 = 5;
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd graywolf-modem && cargo test --features android-test-stub ptt_method_constants_match_spec_appendix_b`
Expected: PASS.

- [ ] **Step 5: Add the proto enum value**

In `proto/platform.proto`, inside `enum PttMethod`:

```proto
enum PttMethod {
  PTT_METHOD_UNKNOWN = 0;
  PTT_METHOD_CP2102N_RTS = 1;
  PTT_METHOD_CM108_HID = 2;
  PTT_METHOD_AIOC_CDC_DTR = 3;
  PTT_METHOD_VOX = 4;
  PTT_METHOD_DIGIRIG_TONE = 5;
}
```

- [ ] **Step 6: Update the `ConfigurePtt.ptt_method` comment in `proto/graywolf.proto`**

Change the `// (proto/platform.proto: 1=...4=PTT_METHOD_VOX).` comment to read `1..5` and append `5=PTT_METHOD_DIGIRIG_TONE`.

- [ ] **Step 7: Add the Kotlin constant**

In `android/app/src/main/kotlin/com/nw5w/graywolf/usb/PttMethodConsts.kt`, after `PTT_METHOD_VOX`:

```kotlin
    /** proto `PTT_METHOD_VOX = 4` — no PTT wire; audio drives VOX */
    const val PTT_METHOD_VOX = 4
    /** proto `PTT_METHOD_DIGIRIG_TONE = 5` — no PTT wire; right-channel tone keys the Digirig Lite */
    const val PTT_METHOD_DIGIRIG_TONE = 5
```

- [ ] **Step 8: Commit**

```bash
git add proto/platform.proto proto/graywolf.proto \
  graywolf-modem/src/tx/ptt_android_consts.rs \
  android/app/src/main/kotlin/com/nw5w/graywolf/usb/PttMethodConsts.kt
git commit -m "feat(ptt): add PTT_METHOD_DIGIRIG_TONE=5 across proto, Rust, Kotlin"
```

---

## Task 2: Add the `jni_audio_set_tone(active, hz)` JNI upcall

The tone gate (on/off) and frequency travel from Rust to Kotlin on the
already-installed `AudioTxCallback` object via a new `setTone(boolean, int)`
method. This task wires the Rust side (android impl, host stub, crate
re-exports) and its unit tests; the Kotlin interface method is added in Task 3.

**Files:**
- Modify: `graywolf-modem/src/android/upcall.rs`
- Modify: `graywolf-modem/src/lib.rs`

- [ ] **Step 1: Write the failing stub-mode upcall test**

In `graywolf-modem/src/android/upcall.rs`, inside `mod tests`, add:

```rust
    #[test]
    #[serial]
    fn audio_set_tone_without_mock_returns_err() {
        clear_mocks();
        let err = super::jni_audio_set_tone(true, 1200).unwrap_err();
        assert!(err.contains("no AudioTx callback installed"), "unexpected: {err}");
        clear_mocks();
    }

    #[test]
    #[serial]
    fn audio_set_tone_forwards_active_and_hz_to_mock() {
        use std::sync::{Arc, Mutex};
        clear_mocks();
        let seen: Arc<Mutex<Option<(bool, i32)>>> = Arc::new(Mutex::new(None));
        let seen2 = seen.clone();
        super::stub_impl::install_tone_mock(move |active, hz| {
            *seen2.lock().unwrap() = Some((active, hz));
        });
        super::jni_audio_set_tone(true, 1200).expect("ok when mock installed");
        assert_eq!(*seen.lock().unwrap(), Some((true, 1200)));
        clear_mocks();
    }
```

Also add `install_tone_mock` to the test `use` line:
`use super::stub_impl::{clear_mocks, install_audio_tx_mock, install_ptt_mock, install_tone_mock};`

- [ ] **Step 2: Run to verify it fails**

Run: `cd graywolf-modem && cargo test --features android-test-stub audio_set_tone`
Expected: FAIL — `jni_audio_set_tone` / `install_tone_mock` not found.

- [ ] **Step 3: Implement the host stub**

In `graywolf-modem/src/android/upcall.rs`, inside `mod stub_impl`, add the mock slot and functions next to the audio-tx mock:

```rust
    static TONE_MOCK: Mutex<Option<Box<dyn Fn(bool, i32) + Send + Sync>>> = Mutex::new(None);

    /// Test-only: install a closure that receives `setTone(active, hz)` calls.
    pub fn install_tone_mock<F>(f: F)
    where
        F: Fn(bool, i32) + Send + Sync + 'static,
    {
        *TONE_MOCK.lock().unwrap() = Some(Box::new(f));
    }

    pub(crate) fn jni_audio_set_tone(active: bool, hz: i32) -> Result<(), String> {
        let guard = TONE_MOCK.lock().unwrap();
        let f = guard
            .as_ref()
            .ok_or_else(|| "no AudioTx callback installed".to_string())?;
        f(active, hz);
        Ok(())
    }
```

Add `*TONE_MOCK.lock().unwrap() = None;` to `clear_mocks()`.

- [ ] **Step 4: Run to verify it passes**

Run: `cd graywolf-modem && cargo test --features android-test-stub audio_set_tone`
Expected: PASS.

- [ ] **Step 5: Implement the Android (JVM) path**

In `mod android_impl`, extend the cached `AudioTxCallback` struct to hold a second method id and resolve it in `install_audio_tx`:

```rust
    struct AudioTxCallback {
        obj: GlobalRef,
        method: JMethodID,     // pushSamples([SI)I
        tone: JMethodID,       // setTone(ZI)V
    }
```

In `install_audio_tx`, after resolving `pushSamples`, resolve `setTone` and store both:

```rust
        let tone = match env.get_method_id(&class, "setTone", "(ZI)V") {
            Ok(m) => m,
            Err(e) => {
                error!("installAudioTxCallback: get_method_id(setTone) failed: {e}");
                return;
            }
        };
        *audio_tx_slot().lock().unwrap() =
            Some(AudioTxCallback { obj: global, method, tone });
```

Add the upcall helper next to `jni_tx_push_samples`:

```rust
    /// Invoke `AudioTxCallback.setTone(active, hz) -> void`. Returns `Err`
    /// only when no callback is installed or the JNI attach/call fails.
    pub(crate) fn jni_audio_set_tone(active: bool, hz: i32) -> Result<(), String> {
        let vm = get_vm()?;
        let (callback, tone_id) = {
            let slot = audio_tx_slot().lock().unwrap();
            let cb = slot
                .as_ref()
                .ok_or_else(|| "no AudioTx callback installed".to_string())?;
            (cb.obj.clone(), cb.tone)
        };
        let mut env = vm
            .attach_current_thread()
            .map_err(|e| format!("setTone: attach_current_thread: {e}"))?;
        let active_jni: jni::sys::jboolean = active as u8;
        // SAFETY: method id resolved against the same class at install time.
        unsafe {
            env.call_method_unchecked(
                callback.as_obj(),
                tone_id,
                jni::signature::ReturnType::Primitive(jni::signature::Primitive::Void),
                &[
                    jni::sys::jvalue { z: active_jni },
                    jni::sys::jvalue { i: hz },
                ],
            )
        }
        .map_err(|e| format!("setTone JNI call failed: {e}"))?;
        Ok(())
    }
```

- [ ] **Step 6: Update the public re-exports**

In the re-export section of `upcall.rs`:

```rust
#[cfg(all(target_os = "android", not(feature = "android-test-stub")))]
pub(crate) use android_impl::{
    install_audio_tx, install_ptt, jni_audio_set_tone, jni_ptt_set, jni_tx_push_samples,
};

#[cfg(feature = "android-test-stub")]
pub(crate) use stub_impl::{jni_audio_set_tone, jni_ptt_set, jni_tx_push_samples};
#[cfg(feature = "android-test-stub")]
pub use stub_impl::{clear_mocks, install_audio_tx_mock, install_ptt_mock, install_tone_mock};
```

- [ ] **Step 7: Re-export at the crate root**

In `graywolf-modem/src/lib.rs`, find the existing `pub(crate) use ... upcall::{...}` (or `android::upcall`) re-export of `jni_ptt_set` / `jni_tx_push_samples` and add `jni_audio_set_tone`. Mirror it for the test-stub `install_tone_mock` next to `install_ptt_mock`. (Match the exact path the existing `jni_ptt_set` re-export uses; grep `jni_ptt_set` in `lib.rs`.)

- [ ] **Step 8: Run the full upcall suite + verify the JVM build compiles**

Run: `cd graywolf-modem && cargo test --features android-test-stub upcall`
Expected: PASS.
Run (compile-only for the real target): `cd graywolf-modem && cargo check --target aarch64-linux-android` *(skip if the NDK target is unavailable in this environment; note it in the commit body)*.

- [ ] **Step 9: Commit**

```bash
git add graywolf-modem/src/android/upcall.rs graywolf-modem/src/lib.rs
git commit -m "feat(android): add jni_audio_set_tone upcall on AudioTxCallback"
```

---

## Task 3: Add `setTone` to the Kotlin `AudioTxCallback` interface and create `ToneOscillator`

**Files:**
- Modify: `android/app/src/main/kotlin/com/nw5w/graywolf/jni/ModemBridge.kt`
- Create: `android/app/src/main/kotlin/com/nw5w/graywolf/audio/ToneOscillator.kt`
- Create: `android/app/src/test/kotlin/com/nw5w/graywolf/audio/ToneOscillatorTest.kt`

- [ ] **Step 1: Write the failing `ToneOscillator` test**

Create `android/app/src/test/kotlin/com/nw5w/graywolf/audio/ToneOscillatorTest.kt`:

```kotlin
package com.nw5w.graywolf.audio

import kotlin.math.PI
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
        osc.next() // consume sample 0
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

    @Test fun toI16ClampsAndScales() {
        val osc = ToneOscillator(sampleRate = 22050)
        osc.setFrequency(1200)
        // Peak amplitude must not exceed Short.MAX_VALUE.
        var maxAbs = 0
        repeat(22050) {
            val v = osc.nextI16().toInt()
            if (kotlin.math.abs(v) > maxAbs) maxAbs = kotlin.math.abs(v)
        }
        assertTrue("peak $maxAbs should be near 0.6 FS", maxAbs in 19000..20500)
    }
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd android && ./gradlew :app:testDebugUnitTest --tests "com.nw5w.graywolf.audio.ToneOscillatorTest"`
Expected: FAIL — `ToneOscillator` not found.

- [ ] **Step 3: Implement `ToneOscillator`**

Create `android/app/src/main/kotlin/com/nw5w/graywolf/audio/ToneOscillator.kt` (Kotlin port of `audio/soundcard.rs::PttTone`):

```kotlin
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
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd android && ./gradlew :app:testDebugUnitTest --tests "com.nw5w.graywolf.audio.ToneOscillatorTest"`
Expected: PASS.

- [ ] **Step 5: Add `setTone` to the `AudioTxCallback` interface**

In `android/app/src/main/kotlin/com/nw5w/graywolf/jni/ModemBridge.kt`, extend the interface:

```kotlin
interface AudioTxCallback {
    fun pushSamples(samples: ShortArray, count: Int): Int

    /**
     * Called by the Rust modem on PTT key/unkey for the Digirig Lite tone
     * method. `active=true` starts emitting a sine of `hz` on the right
     * channel (radio keys on it); `active=false` stops it. A non-zero `hz`
     * also tells the pump this channel is a tone channel, so it runs the
     * output in stereo. `hz=0` with `active=false` returns the pump to mono.
     */
    fun setTone(active: Boolean, hz: Int)
}
```

- [ ] **Step 6: Run the Kotlin build to confirm the interface compiles**

Run: `cd android && ./gradlew :app:compileDebugKotlin`
Expected: FAIL — `AudioTxPump` does not implement `setTone` yet. (This is expected; Task 4 implements it. If you prefer a green build between tasks, do Step 7 of Task 4 first. Either way, commit this task's new files now.)

- [ ] **Step 7: Commit**

```bash
git add android/app/src/main/kotlin/com/nw5w/graywolf/audio/ToneOscillator.kt \
  android/app/src/test/kotlin/com/nw5w/graywolf/audio/ToneOscillatorTest.kt \
  android/app/src/main/kotlin/com/nw5w/graywolf/jni/ModemBridge.kt
git commit -m "feat(android): ToneOscillator + setTone on AudioTxCallback"
```

---

## Task 4: Stereo tone mode in `AudioTxPump`

`AudioTxPump` gains a stereo `AudioTrack` mode. `setTone(active, hz)` is called
off the Rust TX thread: a non-zero `hz` that differs from the current one
rebuilds the track as stereo (idempotent — only on change); `hz=0` rebuilds as
mono. While `active`, every mono AFSK sample becomes a stereo frame
`{L=afsk, R=toneOscillator.nextI16()}`; while inactive on a stereo track, the
right channel is silent and AFSK is duplicated to both channels so existing
non-tone wiring is unaffected. The oscillator resets on each `active=true` edge.

**Files:**
- Modify: `android/app/src/main/kotlin/com/nw5w/graywolf/audio/AudioTxPump.kt`
- Test: `android/app/src/test/kotlin/com/nw5w/graywolf/audio/AudioTxPumpTest.kt`

- [ ] **Step 1: Write the failing interleave test**

Add to `AudioTxPumpTest.kt` (the existing test already injects a fake `AudioTrack` via `trackFactory`; reuse that hook and capture writes). Add a capturing fake that records every `write(ShortArray, off, len, mode)`:

```kotlin
    @Test fun toneModeWritesInterleavedRightChannelTone() {
        val written = mutableListOf<Short>()
        val fakeTrack = fakeAudioTrack(onWrite = { buf, off, len -> for (i in off until off + len) written.add(buf[i]); len })
        val pump = AudioTxPump(appContext(), trackFactory = { fakeTrack })
        pump.start(sampleRate = 22050)
        pump.setTone(active = true, hz = 1200)   // → stereo, tone on
        pump.pushSamples(shortArrayOf(100, 200, 300), 3)
        // 3 mono samples → 6 interleaved stereo samples (L,R pairs).
        assertEquals(6, written.size)
        // Left channel carries the AFSK unchanged.
        assertEquals(100.toShort(), written[0])
        assertEquals(200.toShort(), written[2])
        assertEquals(300.toShort(), written[4])
        // Right channel carries a non-silent tone.
        assertTrue(written[1] != 0.toShort() || written[3] != 0.toShort() || written[5] != 0.toShort())
    }

    @Test fun monoModeUnchangedWhenNoTone() {
        val written = mutableListOf<Short>()
        val fakeTrack = fakeAudioTrack(onWrite = { buf, off, len -> for (i in off until off + len) written.add(buf[i]); len })
        val pump = AudioTxPump(appContext(), trackFactory = { fakeTrack })
        pump.start(sampleRate = 22050)
        pump.pushSamples(shortArrayOf(100, 200, 300), 3)
        assertEquals(listOf<Short>(100, 200, 300), written) // straight passthrough
    }
```

(Add the small `fakeAudioTrack(onWrite=...)` and `appContext()` helpers if the
existing test file lacks them — model them on the test's current
`trackFactory` usage so `write(ShortArray, Int, Int, Int)` routes to `onWrite`.)

- [ ] **Step 2: Run to verify it fails**

Run: `cd android && ./gradlew :app:testDebugUnitTest --tests "com.nw5w.graywolf.audio.AudioTxPumpTest"`
Expected: FAIL — `setTone` unresolved / mono passthrough writes the wrong count.

- [ ] **Step 3: Implement stereo tone mode in `AudioTxPump`**

Add fields, a stereo-aware `buildTrack`, `setTone`, and an interleaving
`pushSamples`. Key changes (merge into the existing class):

```kotlin
    @Volatile private var sampleRate: Int = 22050
    @Volatile private var stereo: Boolean = false
    @Volatile private var toneActive: Boolean = false
    @Volatile private var toneHz: Int = 0
    private var osc: ToneOscillator? = null
    // Reused scratch buffer for interleaving (write thread only).
    private var stereoScratch: ShortArray = ShortArray(0)
```

Factor the `AudioTrack` construction in `start()` into `buildTrack(stereo: Boolean): AudioTrack` that selects the channel mask:

```kotlin
    private fun buildTrack(stereo: Boolean): AudioTrack {
        val tf = trackFactory
        if (tf != null) return tf.invoke(sampleRate)
        val channelMask =
            if (stereo) AudioFormat.CHANNEL_OUT_STEREO else AudioFormat.CHANNEL_OUT_MONO
        val bufBytes = AudioTrack.getMinBufferSize(
            sampleRate, channelMask, AudioFormat.ENCODING_PCM_16BIT,
        ) * 4
        check(bufBytes > 0) { "AudioTrack.getMinBufferSize=$bufBytes" }
        return AudioTrack.Builder()
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
    }
```

Have `start()` store `this.sampleRate = sampleRate` and call `buildTrack(stereo)`; keep the existing USB auto-route + `registerAudioDeviceCallback` logic.

Add `setTone`:

```kotlin
    /**
     * Rust → Kotlin tone control (Digirig Lite tone PTT). A non-zero `hz`
     * that differs from the current one rebuilds the track as stereo so the
     * right channel can carry the tone; `hz == 0` rebuilds as mono. `active`
     * gates whether the tone is emitted during the next transmission. Called
     * off the modem TX thread.
     */
    @Synchronized
    fun setTone(active: Boolean, hz: Int) {
        if (hz != toneHz) {
            toneHz = hz
            val wantStereo = hz > 0
            if (wantStereo != stereo || track != null) {
                rebuildTrack(wantStereo)
            }
        }
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
        // Re-apply USB routing to the new track.
        am.getDevices(AudioManager.GET_DEVICES_OUTPUTS)
            .firstOrNull { isUsbAudioOutput(it) }
            ?.let { t.setPreferredDevice(it) }
        t.play()
        track = t
    }
```

Replace `pushSamples` with an interleaving version:

```kotlin
    override fun pushSamples(samples: ShortArray, count: Int): Int {
        val t = track ?: return -1
        if (!stereo) {
            return t.write(samples, 0, count, AudioTrack.WRITE_BLOCKING)
        }
        // Stereo: L = AFSK, R = tone (active) or silence (idle). AFSK is
        // duplicated to L only; the right channel is the keying tone.
        if (stereoScratch.size < count * 2) stereoScratch = ShortArray(count * 2)
        val o = osc
        val emitTone = toneActive && o != null
        for (i in 0 until count) {
            stereoScratch[i * 2] = samples[i]
            stereoScratch[i * 2 + 1] = if (emitTone) o!!.nextI16() else 0
        }
        return t.write(stereoScratch, 0, count * 2, AudioTrack.WRITE_BLOCKING)
    }
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd android && ./gradlew :app:testDebugUnitTest --tests "com.nw5w.graywolf.audio.AudioTxPumpTest"`
Expected: PASS.

- [ ] **Step 5: Compile the whole app (interface + impl now consistent)**

Run: `cd android && ./gradlew :app:compileDebugKotlin`
Expected: SUCCESS.

- [ ] **Step 6: Commit**

```bash
git add android/app/src/main/kotlin/com/nw5w/graywolf/audio/AudioTxPump.kt \
  android/app/src/test/kotlin/com/nw5w/graywolf/audio/AudioTxPumpTest.kt
git commit -m "feat(android): stereo tone mode in AudioTxPump (L=AFSK, R=tone)"
```

---

## Task 5: Route the Android PTT driver's key/unkey to the tone upcall

For method `5`, `AndroidPtt` keys by emitting the tone instead of toggling a
USB line: `key()` → `jni_audio_set_tone(true, mark_freq)`, `unkey()` →
`jni_audio_set_tone(false, mark_freq)`. Methods 1–4 keep calling `jni_ptt_set`
unchanged. The frequency is read from `config_state` at key time so it is always
the channel's current `mark_freq`.

**Files:**
- Modify: `graywolf-modem/src/tx/ptt_android.rs`
- Modify: `graywolf-modem/src/tx/ptt.rs:606-614` (`build_driver`)

- [ ] **Step 1: Write the failing driver test**

In `graywolf-modem/src/tx/ptt_android.rs` `mod tests`, add (uses the new tone mock from Task 2):

```rust
    #[test]
    #[serial]
    fn digirig_tone_method_keys_via_tone_upcall_not_ptt() {
        use crate::tx::ptt_android_consts::PTT_METHOD_DIGIRIG_TONE;
        crate::clear_mocks();
        let tone: Arc<Mutex<Option<(bool, i32)>>> = Arc::new(Mutex::new(None));
        let tone2 = tone.clone();
        crate::install_tone_mock(move |active, hz| { *tone2.lock().unwrap() = Some((active, hz)); });
        // A pttSet mock that panics ensures method 5 never touches the USB path.
        crate::install_ptt_mock(|_, _| panic!("method 5 must not call pttSet"));

        let mut ptt = AndroidPtt::new(PTT_METHOD_DIGIRIG_TONE);
        ptt.key().expect("tone key should succeed");
        let (active, hz) = tone.lock().unwrap().expect("setTone must be called");
        assert!(active, "key must start the tone");
        assert!(hz > 0, "key must pass a non-zero mark frequency, got {hz}");

        ptt.unkey().expect("tone unkey should succeed");
        assert_eq!(tone.lock().unwrap().unwrap().0, false, "unkey must stop the tone");
        crate::clear_mocks();
    }
```

For the host stub, `config_state::mark_freq()` must return a non-zero default
or be settable. If it defaults to 0 under the stub, set it first via
`crate::android::config_state::set_channel_dsp(1200, 1200, 2200);` at the top of
the test (config_state compiles under `android-test-stub`).

- [ ] **Step 2: Run to verify it fails**

Run: `cd graywolf-modem && cargo test --features android-test-stub digirig_tone_method_keys_via_tone_upcall`
Expected: FAIL — `AndroidPtt` still calls `jni_ptt_set` for method 5.

- [ ] **Step 3: Implement the tone branch in `AndroidPtt`**

In `graywolf-modem/src/tx/ptt_android.rs`:

```rust
use super::ptt::PttDriver;
use super::ptt_android_consts::PTT_METHOD_DIGIRIG_TONE;

impl PttDriver for AndroidPtt {
    fn key(&mut self) -> Result<(), String> {
        if self.method == PTT_METHOD_DIGIRIG_TONE {
            let hz = crate::android::config_state::mark_freq() as i32;
            return crate::jni_audio_set_tone(true, hz)
                .map_err(|e| format!("android digirig tone key: {e}"));
        }
        crate::jni_ptt_set(self.method, true)
            .map_err(|e| format!("android ptt key (method={}): {}", self.method, e))
    }

    fn unkey(&mut self) -> Result<(), String> {
        if self.method == PTT_METHOD_DIGIRIG_TONE {
            let hz = crate::android::config_state::mark_freq() as i32;
            return crate::jni_audio_set_tone(false, hz)
                .map_err(|e| format!("android digirig tone unkey: {e}"));
        }
        crate::jni_ptt_set(self.method, false)
            .map_err(|e| format!("android ptt unkey (method={}): {}", self.method, e))
    }
}
```

If `crate::android::config_state` is not reachable from `tx/` under the stub
cfg, add a thin re-export `pub(crate) use android::config_state;` at the crate
root guarded by the same cfg, and reference `crate::config_state::mark_freq()`.
(Grep how `tx/mod.rs` reaches other `android::` items, e.g. in `android/mod.rs`’s
`build_samples` call, and match that path.)

- [ ] **Step 4: Accept method 5 in `build_driver`**

In `graywolf-modem/src/tx/ptt.rs`, extend the Android match arm:

```rust
                    use crate::tx::ptt_android_consts::{
                        PTT_METHOD_AIOC_CDC_DTR, PTT_METHOD_CM108_HID, PTT_METHOD_CP2102N_RTS,
                        PTT_METHOD_DIGIRIG_TONE, PTT_METHOD_VOX,
                    };
                    let method = cfg.ptt_method as i32;
                    match method {
                        PTT_METHOD_CP2102N_RTS
                        | PTT_METHOD_CM108_HID
                        | PTT_METHOD_AIOC_CDC_DTR
                        | PTT_METHOD_VOX
                        | PTT_METHOD_DIGIRIG_TONE => {
                            Ok(Box::new(super::ptt_android::AndroidPtt::new(method)))
                        }
                        n => Err(format!("android ptt: unknown method int {}", n)),
                    }
```

- [ ] **Step 5: Run the driver + build_driver tests**

Run: `cd graywolf-modem && cargo test --features android-test-stub ptt_android`
Expected: PASS. Also run `cargo test --features android-test-stub build_driver` — existing 1–4 cases still pass.

- [ ] **Step 6: Commit**

```bash
git add graywolf-modem/src/tx/ptt_android.rs graywolf-modem/src/tx/ptt.rs
git commit -m "feat(android): route digirig_tone PTT (method 5) through tone upcall"
```

---

## Task 6: Pre-warm the stereo track and prepend the silent lead-in (Android TX arm)

Two refinements in `android/mod.rs` so the radio is keyed before AFSK:

1. **Pre-warm:** when a channel's PTT is configured, tell `AudioTxPump` the
   tone frequency (so it rebuilds stereo before the first TX). For method 5,
   call `jni_audio_set_tone(false, mark_freq)`; for others, `(false, 0)`.
2. **Silent lead-in:** for a tone channel, prepend `DIGIRIG_TONE_LEAD_MS` (500 ms)
   of silence to the mono AFSK so the tone (gated on at key) leads the packet —
   matching desktop. Tracked by a `config_state` flag.

**Files:**
- Modify: `graywolf-modem/src/android/config_state.rs`
- Modify: `graywolf-modem/src/android/mod.rs` (ConfigurePtt arm ~458-474; TransmitFrame arm ~499-525)

- [ ] **Step 1: Write the failing config_state flag test**

In `graywolf-modem/src/android/config_state.rs` tests (or create a `#[cfg(test)]` block if none), add:

```rust
    #[test]
    fn digirig_tone_flag_round_trips() {
        set_digirig_tone(true);
        assert!(digirig_tone());
        set_digirig_tone(false);
        assert!(!digirig_tone());
    }
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd graywolf-modem && cargo test --features android-test-stub digirig_tone_flag_round_trips`
Expected: FAIL — `set_digirig_tone` / `digirig_tone` not found.

- [ ] **Step 3: Add the flag to config_state**

In `graywolf-modem/src/android/config_state.rs`, mirror the existing atomics:

```rust
use std::sync::atomic::{AtomicBool, Ordering};

static DIGIRIG_TONE: AtomicBool = AtomicBool::new(false);

/// Record whether the active channel's PTT method is Digirig Lite tone PTT,
/// so the TX builder prepends the silent lead-in for it.
pub fn set_digirig_tone(on: bool) {
    DIGIRIG_TONE.store(on, Ordering::Relaxed);
}

pub fn digirig_tone() -> bool {
    DIGIRIG_TONE.load(Ordering::Relaxed)
}
```

(If the module already imports `Ordering`/atomics, reuse them; do not duplicate the `use`.)

- [ ] **Step 4: Run to verify it passes**

Run: `cd graywolf-modem && cargo test --features android-test-stub digirig_tone_flag_round_trips`
Expected: PASS.

- [ ] **Step 5: Set the flag + pre-warm in the ConfigurePtt arm**

In `graywolf-modem/src/android/mod.rs`, in the `ConfigurePtt` handling (where
`register_driver` is called, ~458-474), after the driver registers:

```rust
                            let is_tone = cfg.ptt_method as i32
                                == crate::tx::ptt_android_consts::PTT_METHOD_DIGIRIG_TONE
                                && cfg.method == "android";
                            config_state::set_digirig_tone(is_tone);
                            // Pre-warm the Kotlin sink: rebuild stereo (tone hz)
                            // or mono (hz=0) before the first TX so the first
                            // key has no track-rebuild latency. Best-effort.
                            let hz = if is_tone { config_state::mark_freq() as i32 } else { 0 };
                            if let Err(e) = crate::jni_audio_set_tone(false, hz) {
                                warn!("pre-warm setTone(hz={hz}): {e}");
                            }
```

(Adjust field access to the actual `cfg` binding name in that arm — it is the
`ConfigurePtt` payload; confirm whether `method`/`ptt_method` are on it.)

- [ ] **Step 6: Prepend the silent lead-in in the TransmitFrame arm**

In the `TransmitFrame` arm, after `build_samples` returns `Ok(samples)` (~508)
and before constructing the `TxJob`, prepend the lead-in for tone channels:

```rust
                                Ok(mut samples) => {
                                    if config_state::digirig_tone() {
                                        // 500 ms silent left-channel lead-in so
                                        // the right-channel tone (gated on at
                                        // key) leads the packet, mirroring the
                                        // desktop DIGIRIG_TONE_LEAD_MS lead-in.
                                        let lead = (TARGET_SAMPLE_RATE as u64 * 500 / 1000) as usize;
                                        let mut buf = vec![0i16; lead];
                                        buf.extend_from_slice(&samples);
                                        samples = buf;
                                    }
                                    let job = TxJob {
                                        channel: tf.channel,
                                        samples,
                                        // ... unchanged: sample_rate, output_device_id,
                                        //     sink_config { channels: 1, ptt_tone_hz: 0, ... }
```

The `sink_config` stays mono with `ptt_tone_hz: 0` — the tone is a Kotlin-side
concern now; `AndroidTxSink` ignores `sink_config`.

- [ ] **Step 7: Add an android-arm lead-in unit test**

If the lead-in logic is non-trivial to reach through the IPC loop, extract it
into a tiny pure helper and test it:

```rust
/// Prepend `ms` of silence at `sample_rate` to `samples` (Digirig tone lead-in).
pub(crate) fn prepend_silence(samples: Vec<i16>, sample_rate: u32, ms: u32) -> Vec<i16> {
    let lead = (sample_rate as u64 * ms as u64 / 1000) as usize;
    let mut buf = vec![0i16; lead];
    buf.extend_from_slice(&samples);
    buf
}
```

```rust
    #[test]
    fn prepend_silence_adds_exact_lead() {
        let out = prepend_silence(vec![7i16; 10], 22050, 500);
        assert_eq!(out.len(), 11035); // 11025 silent + 10
        assert!(out[..11025].iter().all(|&s| s == 0));
        assert_eq!(&out[11025..], &[7i16; 10]);
    }
```

Use `prepend_silence` in Step 6 instead of the inline version.

- [ ] **Step 8: Run tests + commit**

Run: `cd graywolf-modem && cargo test --features android-test-stub`
Expected: PASS.

```bash
git add graywolf-modem/src/android/config_state.rs graywolf-modem/src/android/mod.rs
git commit -m "feat(android): tone-channel stereo pre-warm + silent TX lead-in"
```

---

## Task 7: Add the Android PTT method picker entry

**Files:**
- Modify: `web/src/routes/ptt/devices/methodOptions.android.js`
- Test: `web/src/routes/ptt/devices/methodOptions.test.js`

- [ ] **Step 1: Write the failing picker test**

In `methodOptions.test.js`, add (match the file's existing import + assertion style):

```js
import { ANDROID_METHODS } from './methodOptions.android.js';

test('android methods include Digirig Lite tone PTT (ptt_method 5)', () => {
  const tone = ANDROID_METHODS.find(
    (m) => m.wire.method === 'android' && m.wire.ptt_method === 5,
  );
  expect(tone).toBeDefined();
  expect(tone.label).toMatch(/Digirig Lite/i);
  // Tone PTT needs the Digirig Lite as the USB audio output, not a serial line.
  expect(tone.deviceClass).toBe('usb-audio');
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npx vitest run src/routes/ptt/devices/methodOptions.test.js`
Expected: FAIL — no entry with `ptt_method === 5`.

- [ ] **Step 3: Add the entry**

In `web/src/routes/ptt/devices/methodOptions.android.js`, append to `ANDROID_METHODS`:

```js
  { wire: { method: 'android', ptt_method: 5 },
    label: 'Digirig Lite (tone PTT)',
    meta: 'No PTT wire — a tone on the right channel keys the radio while the packet plays on the left. Requires the Digirig Lite as the USB audio output.',
    deviceClass: 'usb-audio' },
```

(Confirm `androidDeviceSource.list()` handles `deviceClass: 'usb-audio'` — if it
filters only known serial/HID classes, add a branch that lists USB **audio**
outputs, or, if no device selection applies to tone PTT, omit `deviceClass` and
verify the picker renders the method with no device row. Match whichever pattern
the VOX entry uses, since VOX likewise needs no serial device.)

- [ ] **Step 4: Run to verify it passes**

Run: `cd web && npx vitest run src/routes/ptt/devices/methodOptions.test.js`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/routes/ptt/devices/methodOptions.android.js \
  web/src/routes/ptt/devices/methodOptions.test.js
git commit -m "feat(web): add Android Digirig Lite tone PTT method option"
```

---

## Task 8: Allow `ptt_method = 5` for `method == "android"` in the Go DTO

**Files:**
- Modify: `pkg/webapi/dto/ptt.go:41-50`
- Test: `pkg/webapi/dto/ptt_test.go`

- [ ] **Step 1: Write the failing validation test**

In `pkg/webapi/dto/ptt_test.go`, add:

```go
func TestPttRequestValidate_AndroidDigirigToneAccepted(t *testing.T) {
	r := PttRequest{ChannelID: 1, Method: "android", PttMethod: 5}
	if err := r.Validate(); err != nil {
		t.Fatalf("android ptt_method=5 should be valid, got: %v", err)
	}
}

func TestPttRequestValidate_AndroidOutOfRangeRejected(t *testing.T) {
	r := PttRequest{ChannelID: 1, Method: "android", PttMethod: 6}
	if err := r.Validate(); err == nil {
		t.Fatal("android ptt_method=6 should be rejected")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/webapi/dto/ -run TestPttRequestValidate_Android`
Expected: FAIL — `ptt_method=5` currently rejected.

- [ ] **Step 3: Extend the range check**

In `pkg/webapi/dto/ptt.go`:

```go
	// android method requires ptt_method in 1..5 (spec Appendix B):
	//   1 = CP2102N_RTS, 2 = CM108_HID, 3 = AIOC_CDC_DTR, 4 = VOX,
	//   5 = DIGIRIG_TONE
	if r.Method == "android" {
		switch r.PttMethod {
		case 1, 2, 3, 4, 5:
			// valid
		default:
			return fmt.Errorf("android ptt method requires ptt_method in 1..5 (spec Appendix B), got %d", r.PttMethod)
		}
	}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./pkg/webapi/dto/ -run TestPttRequestValidate_Android`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/webapi/dto/ptt.go pkg/webapi/dto/ptt_test.go
git commit -m "feat(api): accept android ptt_method=5 (Digirig Lite tone PTT)"
```

---

## Task 9: Documentation (handbook + wiki)

**Files:**
- Modify: `docs/handbook/ptt.html` (the existing `#digirig-tone-ptt` section, ~238-265)
- Modify: `docs/wiki/` (the page covering Android PTT / `methodOptions`; grep `PTT_METHOD_VOX` and the phase-4b page)

- [ ] **Step 1: Extend the handbook Digirig tone section for Android**

In `docs/handbook/ptt.html` under `<h2 id="digirig-tone-ptt">`, add an Android
paragraph: on Android the method is selected as **"Digirig Lite (tone PTT)"** in
the channel PTT picker; no device path is needed; the app drives the Digirig
Lite as the USB **audio** output and hard-pins **AFSK = left, tone = right** (no
output-channel selector, unlike desktop). Note the stereo-USB requirement and
that a mono-only USB negotiation will be reported as an error rather than
silently failing to key.

- [ ] **Step 2: Update the wiki**

On the Android PTT topology page (per CLAUDE.md "wiki-worthy" rule — new method
crossing proto/Rust/Kotlin/web/Go), record: the new `PTT_METHOD_DIGIRIG_TONE = 5`
value; that the tone is synthesised in `AudioTxPump` (stereo, `L=AFSK/R=tone`),
not in Rust; that PTT key/unkey for method 5 flows through the
`AudioTxCallback.setTone` upcall rather than `UsbPttCallback.pttSet`; and the
fixed L/R mapping. Cross-link the design doc.

- [ ] **Step 3: Commit**

```bash
git add docs/handbook/ptt.html docs/wiki/
git commit -m "docs: Android Digirig Lite tone PTT (handbook + wiki)"
```

---

## Integration verification (after all tasks)

- [ ] **Rust:** `cd graywolf-modem && cargo test --features android-test-stub` — all green.
- [ ] **Kotlin:** `cd android && ./gradlew :app:testDebugUnitTest` — all green.
- [ ] **Web:** `cd web && npx vitest run src/routes/ptt/devices/` — all green.
- [ ] **Go:** `go test ./pkg/webapi/dto/` — all green.
- [ ] **Cross-language constant sync (T13):** confirm the Kotlin `PttMethodConsts`,
  Rust `ptt_android_consts`, and proto enum all carry `5`; if the T13 sync test
  exists, extend its expected set to include `DIGIRIG_TONE = 5`.
- [ ] **On-device acceptance gate (manual, real hardware):** Digirig Lite + radio
  on Android. Select "Digirig Lite (tone PTT)", transmit a beacon, and confirm
  (a) the radio keys, (b) a reference station decodes the packet, (c) the tone is
  on the **right** wire and AFSK on the **left**. This validates the design's
  top risk — that Android USB-audio honours `CHANNEL_OUT_STEREO` channel order
  to the Digirig Lite end to end. If channels are swapped or down-mixed, revisit
  the L/R assignment in `AudioTxPump.pushSamples` before shipping.
- [ ] **Mono-negotiation failure mode (manual):** force/observe a mono-only USB
  output and confirm the operator sees an actionable error rather than a silent
  no-key. (If `AudioTrack` cannot open stereo, surface it via the existing
  status/log path instead of dropping the tone.)

---

## Notes / decisions baked into this plan

- **Tone in Kotlin, not Rust interleave** — preserves the governor's mono
  drain-timing (see top section).
- **Method 5 keys via `setTone`, not `pttSet`** — keeps the `UsbPttCallback`
  contract unchanged and decouples `UsbPttAdapter` from `AudioTxPump`; Rust
  bridges them via the existing `AudioTxCallback`.
- **Hard-pinned L=AFSK / R=tone on Android** — Android has no per-channel
  output selector; the mapping is fixed to the Digirig Lite wiring (desktop's
  offered follow-up, applied here).
- **`mark_freq` read at key time** from `config_state` — always the channel's
  current value; no new config plumbing and no ConfigurePtt/ConfigureChannel
  ordering hazard.
- **Out of scope:** desktop changes (done in #358); per-channel output picker on
  Android; multiple concurrent dongles; hot method-swap on a live channel.
```
