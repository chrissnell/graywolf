# Android POC-C — run report

**Date:** 2026-05-08
**Branch:** feature/android-poc-c
**Commit at end of run:** 78f5c3b

## Verdict

**YELLOW.** Stage 2 over-the-air decode passed 4 of 4 reported trials with the exact
target string, but Stage 1 (wired loop) was skipped — the operator's lab setup
went straight to RF (Digirig USB → tablet, Digirig TRS → radio mic, second
APRS station as reference receiver). Yellow is also driven by an
AudioTrack-state bug that was caught and fixed during the run; the fix is
committed and validated.

## Toolchain

- rustc 1.91.0 (rustup stable, host = aarch64-apple-darwin)
  - Note: Homebrew `rustc 1.90.0` shadowed rustup in PATH and lacked the
    `aarch64-linux-android` standard library; cross-compile required
    prepending `~/.rustup/toolchains/stable-aarch64-apple-darwin/bin` to
    PATH so cargo invoked the rustup-managed compiler.
- cargo-ndk 4.x with `-P 26` (NDK platform/API 26; `aaudio` link target
  requires API ≥ 26).
- Android NDK r29 (29.0.14206865) at `/opt/homebrew/share/android-ndk`.
- JDK 17 (Alpine builder).
- Gradle 8.14.4 on Alpine builder, Kotlin 2.0.21, Groovy 3.0.25.
- Tablet: Topicon T865, Android 14.

## Hardware

Stage 2 chain as wired this run:
- Tablet (USB-C) → Digirig USB-OTG (presents as `usb_headset` on Android).
- Digirig TRS audio output → radio's mic input.
- Operator manually keyed PTT for each trial.
- Second APRS station (NW5W-5 at the receive site, plus APRS-IS
  forwarding) decoded the live transmissions.

Stage 1 wired loop skipped — operator went straight to RF.

## Success criteria

| # | Criterion | Stage 1 | Stage 2 |
|---|---|---|---|
| 1 | Frame builds in Rust | ✅ (Rust round-trip test green; on-device JNI emits 18,393 samples per call) | n/a |
| 2 | Kotlin plays via AudioTrack | n/a (skipped) | ✅ after fix (`78f5c3b`); `tx_test_done ok=true` for every trial. |
| 3 | Reference decodes target string | n/a (skipped) | ✅ 4 of 4 reported trials |
| 4 | Clean repeat (no leak / drift) | n/a (skipped) | ✅ five back-to-back triggers with no AudioTrack failures, no logged errors, no audio drift |

Target string:
```
NW5W-8>APGRWO:!4028.56N/11150.71W< POC-C TX test - NW5W bench
```

## Trial results (Stage 2)

| Trial | Decoded | Notes |
|---|---|---|
| 1 | ✅ | Direct RF: `NW5W-8>APGRWO:!4028.56N/11150.71W< POC-C TX test - NW5W bench` |
| 2 | ✅ | Same exact string |
| 3 | ✅ | Via digipeater: `NW5W-8>APGRWO,qAR,NW5W-5:!4028.56N/11150.71W< POC-C TX test - NW5W bench` (frame body identical; `,qAR,NW5W-5` is APRS-IS injection metadata) |
| 4 | ✅ | Same as trial 3 |
| 5 | (operator stopped early; not reported) | Logcat shows playback completed cleanly: `playback complete: head=18393 frames`, `tx_test_done ok=true` |

Pre-fix attempts (4) before the AudioTrack state-check bug was diagnosed:
all returned `ok=false` with `AudioTrack state=2 after build; releasing`,
no audio played, decoder logged nothing.

## AudioTrack parameters that worked

- Sample rate: 22,050 Hz (matches POC-B RX rate; single-rate invariant
  preserved).
- Channel mask: `CHANNEL_OUT_MONO`.
- Encoding: `ENCODING_PCM_16BIT`.
- Transfer mode: `MODE_STATIC`.
- Usage / content type: `USAGE_MEDIA` / `CONTENT_TYPE_MUSIC` (HAL routed
  cleanly to the Digirig USB headset; no escalation to
  `USAGE_VOICE_COMMUNICATION` was needed).
- Buffer size in bytes: 36,786 (= 18,393 samples × 2 bytes/PCM16).
- Post-build state: `STATE_NO_STATIC_DATA` (= 2). Transitions to
  `STATE_INITIALIZED` after `write()`. The AudioTxTest helper now treats
  `STATE_NO_STATIC_DATA` as expected post-build; only `STATE_UNINITIALIZED`
  is fatal at that point.

Frame envelope: 18,393 samples / 22,050 Hz ≈ 834 ms (= 300 ms preamble +
~445 ms frame body at 1200 baud + 100 ms tail). The plan's "≈ 3.3 s"
estimate was off by ~3.3×; the canned-frame length-bound test was
adjusted to 0.7..1.0 s to match the real envelope.

## TX-side software gain

None applied. AudioTrack output was at unity; the operator did not need
any TX-side scalar to get a clean RF signal at the receive station. No
new gain invariant introduced — POC-B's RX gain knob stays untouched.

## Logcat highlights

```
05-08 13:49:24.076  7780  7780 I graywolfmodem: JNI_OnLoad: v0.13.2-99ea0dd-dirty
05-08 13:49:24.143  7780  7780 I AudioPump: AudioRecord init rate=22050 bufBytes=7056 (min=1764)
05-08 13:49:24.184  7780  7879 I graywolfmodem: poc-b: ipc_client_connected
05-08 13:49:24.456  7780  7780 I MainActivity: poc-b: webview_loaded

# Trial 1 (post-fix)
05-08 13:49:40.067  7780  7932 I graywolfmodem: modemBuildTestFrame: emitted 18393 samples (834 ms @ 22050 Hz)
05-08 13:49:40.067  7780  7932 I WebAppInterface: poc-c: tx_test_fire samples=18393
05-08 13:49:40.087  7780  7932 I AudioTxTest: AudioTrack built: rate=22050 samples=18393 bufferBytes=36786 state=2
05-08 13:49:40.972  7780  7932 I AudioTxTest: playback complete: head=18393 frames
05-08 13:49:40.975  7780  7932 I WebAppInterface: poc-c: tx_test_done ok=true

# Trial 5 (last trigger captured)
05-08 13:52:25.060  7780  7957 I graywolfmodem: modemBuildTestFrame: emitted 18393 samples (834 ms @ 22050 Hz)
05-08 13:52:25.075  7780  7957 I AudioTxTest: AudioTrack built: rate=22050 samples=18393 bufferBytes=36786 state=2
05-08 13:52:25.955  7780  7957 I AudioTxTest: playback complete: head=18393 frames
05-08 13:52:25.959  7780  7957 I WebAppInterface: poc-c: tx_test_done ok=true
```

Pre-fix (bug surface):
```
05-08 13:43:58.191  7320  7657 I graywolfmodem: modemBuildTestFrame: emitted 18393 samples (834 ms @ 22050 Hz)
05-08 13:43:58.191  7320  7657 I WebAppInterface: poc-c: tx_test_fire samples=18393
05-08 13:43:58.206  7320  7657 E AudioTxTest: AudioTrack state=2 after build; releasing
05-08 13:43:58.208  7320  7657 I WebAppInterface: poc-c: tx_test_done ok=false
```

## Reference decoder excerpt (Stage 2)

Operator-reported decodes from the second APRS station (verbatim):
```
NW5W-8>APGRWO:!4028.56N/11150.71W< POC-C TX test - NW5W bench
NW5W-8>APGRWO:!4028.56N/11150.71W< POC-C TX test - NW5W bench
NW5W-8>APGRWO,qAR,NW5W-5:!4028.56N/11150.71W< POC-C TX test - NW5W bench
NW5W-8>APGRWO,qAR,NW5W-5:!4028.56N/11150.71W< POC-C TX test - NW5W bench
```
Frame body is byte-identical across all four; the `,qAR,NW5W-5` suffix on
trials 3-4 is APRS-IS metadata indicating the packet was re-broadcast
through the NW5W-5 station and an APRS-IS server, not a transmission
difference on the tablet side.

## Yellow caveats

1. **AudioTrack state-check bug, fixed mid-run.** First four trial
   triggers all returned `ok=false` because `AudioTxTest.fireOnce` checked
   `track.state != STATE_INITIALIZED` immediately after `AudioTrack.Builder.build()`
   and rejected the track. For `MODE_STATIC`, post-build state is
   `STATE_NO_STATIC_DATA` (= 2); the track only transitions to
   `STATE_INITIALIZED` after `write()`. Fix in commit `78f5c3b`: treat
   `STATE_UNINITIALIZED` as the only fatal post-build state, and assert
   `STATE_INITIALIZED` only after the write completes. Phase 5 inherits
   this corrected lifecycle.
2. **Stage 1 (wired loop) skipped.** Lab setup went straight to RF, so
   there is no wired-loopback baseline confirming the AudioTrack output
   is bit-clean before the radio chain. Stage 2 success makes this less
   important, but if a future regression makes RF flaky, a Stage 1 rerun
   would isolate Android-side vs. radio-side cause.
3. **Plan length estimate was wrong.** The canned-frame length test
   bounded the buffer at 2.8..3.6 s; actual is 0.834 s. Bounds were
   relaxed to 0.7..1.0 s. No DSP impact — round-trip test passed first
   try at 22,050 Hz.
4. **Homebrew rustc shadowed rustup.** `cargo ndk -t arm64-v8a` failed
   with "can't find crate for `core`" until rustup's
   `aarch64-apple-darwin` toolchain was prepended to PATH. Phase-5 build
   automation (Gradle `externalNativeBuild` or CI) needs to make this
   explicit, otherwise the next clean checkout will trip the same way.

## Production-app design implications

- AudioTrack `MODE_STATIC` works for one-shot frames over USB-Audio out
  with no operator-visible tuning. Phase 5 streaming will need
  `MODE_STREAM` and explicit drain handling, but the
  `USAGE_MEDIA`/`CONTENT_TYPE_MUSIC` routing choice carried through
  cleanly without forcing `USAGE_VOICE_COMMUNICATION`.
- 22,050 Hz mono PCM16 RX/TX rate is now load-bearing on both directions
  of the audio path. Don't change either side without changing both.
- `tx::build_samples` is a clean library entry; it ran one-shot from JNI
  with no surprises. Phase 5's per-frame TX path can lift this directly
  with no DSP refactor.
- WebView → Kotlin direct bridge (`@JavascriptInterface`) is sufficient
  for trigger surfaces; no Go round-trip needed for fire-and-forget
  actions originating in the UI.

## Phase-5 streaming gap analysis

POC-C uses `AudioTrack(MODE_STATIC)`: write the whole buffer once, play,
drain on `playbackHeadPosition`, release. Phase 5 needs continuous
streaming TX with PTT key/unkey timing.

| POC-C surface | Phase-5 disposition |
|---|---|
| `AudioAttributes` (USAGE_MEDIA, CONTENT_TYPE_MUSIC) | Reusable. Stage 2 confirmed routing to USB-Audio out works at this attribute set. |
| `AudioFormat` (PCM16, 22,050 Hz, MONO) | Reusable; locked invariant with RX. |
| Transfer mode `MODE_STATIC` | Throwaway — phase 5 needs `MODE_STREAM`. |
| `track.write(samples, 0, n)` blocking call | Throwaway — phase 5 needs a producer/consumer queue with underrun detection. |
| `playbackHeadPosition` polling for drain | Replace with `setNotificationMarkerPosition` + `OnPlaybackPositionUpdateListener` so PTT-release fires on a precise frame boundary, not a 50 ms poll grid. |
| `tx::canned::build_canned_test_frame_pcm` | Reusable for tests; production builds frames per-frame from operator state via `pkg/ax25` + `tx::build_samples`. |
| Single-flight `AtomicBoolean` guard | Conceptually reusable; phase 5 needs a stronger queue / TX state machine since multiple frames can be in flight back-to-back. |
| AudioTrack post-build state check | Phase 5 must continue to allow `STATE_NO_STATIC_DATA` (or skip the post-build check entirely when using `MODE_STREAM`). |

Phase-5 must add: a sample queue between modulator and AudioTrack; a
marker callback for end-of-frame PTT release; an underrun handler;
coordination with `txgovernor` for RX-pause semantics; and a TX-while-RX
mute path so the demodulator doesn't try to decode our own audio.

## Phase-5 build pipeline implications

POC-B and POC-C commit `android/app/src/main/jniLibs/arm64-v8a/libgraywolfmodem.so`
and `libgraywolf.so` to git. Acceptable for the POCs; not acceptable for
phase 5:

- `libgraywolfmodem.so` is ~1.1 MB, `libgraywolf.so` is ~9.6 MB; both
  rewrite on every Rust/Go change and bloat git history.
- Binary merge conflicts on rebase are unrecoverable without rebuild.
- arm64-only artefacts exclude `armeabi-v7a` and the `x86_64` emulator.
- Cross-compile requires a custom PATH setup (rustup wrapper +
  ANDROID_NDK_HOME + cargo-ndk `-P 26`) that nothing in-repo enforces.

Phase-5 plan must move .so production into one of:

1. Gradle `externalNativeBuild` invoking `cargo-ndk` (Rust) and a Make
   target invoking the NDK clang for the Go binary, both per ABI at
   assemble time.
2. CI workflow that builds `.so` artefacts for all target ABIs and
   publishes to a maven-style artefact repo, pulled at assemble time.

Either way, `android/app/src/main/jniLibs/` becomes `.gitignore`d. The
chosen approach is open; the phase-5 plan must close on it before the
first phase-5 commit.
