# Android — Digirig Lite Tone PTT — Design / Plan

**Date:** 2026-06-19
**Issue:** GRA-169 (plans the Android side of GRA-168 / PR #358)
**Builds on:** `docs/superpowers/specs/2026-05-16-android-phase-4b-tx-ptt-design.md`

---

## 1. Goal

Bring the `digirig_tone` PTT method to the Android app. The Digirig Lite has
no serial/HID PTT wire — its onboard circuit keys the radio whenever it sees
an audio tone on the **right** channel, while the AFSK packet plays on the
**left** channel. PR #358 implemented this on desktop. This plan describes the
Android-specific work, because the Android TX audio path is entirely different
from desktop's.

When this lands: an operator with a Digirig Lite + radio on Android picks
"Digirig Lite (tone PTT)" for a channel, hits go, and the radio keys from the
right-channel tone with the packet on the left — no PTT wire configured.

## 2. Why desktop's implementation does not carry over

On **desktop**, the tone is synthesised inside the Rust cpal output callback
(`audio/soundcard.rs` `PttTone`). The sink renders an interleaved
multi-channel buffer: AFSK on `audio_channel`, a steady `mark_freq` sine on the
*companion* channel (`ptt_tone_channel`), with a 500 ms silent lead-in
(`modem/mod.rs` `digirig_tone_lead_in`) so the tone leads the packet. PTT
keying itself is a `NonePtt` no-op — the tone does the work.

On **Android**, none of that runs:

- The Rust modem does **not** use the cpal sink. It pushes **mono** PCM16 to
  Kotlin `AudioTxPump` via JNI (`android/audio_tx.rs` `AndroidTxSink` →
  `ModemBridge.installAudioTxCallback` → `AudioTxPump.pushSamples`).
  `AudioTxPump` writes to a **`CHANNEL_OUT_MONO`** `AudioTrack`.
- The Android TX build arm (`android/mod.rs`) calls `tx::build_samples`
  directly and hard-codes `channels: 1, audio_channel: 0, ptt_tone_hz: 0`
  with the comment *"Android keys PTT via JNI, never via a companion-channel
  tone."* It never invokes `handle_transmit_frame`, so neither the VOX lead-in
  nor the digirig silent lead-in is prepended today.
- PTT methods are integer-coded (`PttMethod` proto enum 1..4) and actuated
  through `UsbPttCallback.pttSet(method, keyed)` → `UsbPttAdapter`. There is no
  value for `digirig_tone`; the highest is `PTT_METHOD_VOX = 4`.

So the core problem is purely Android-side: **the only audio sink is the Kotlin
mono `AudioTrack`, and it has no second channel to put the tone on.**

## 3. Design decision — where the tone is generated

Whatever produces the final buffer must emit **two** channels, and the
`AudioTrack` must run stereo. Two options:

**Option A — Rust builds interleaved stereo, Kotlin plays it.**
The Android arm interleaves L=AFSK / R=tone and pushes a stereo buffer over the
existing flat-`i16` JNI call. *Rejected:* it doubles the sample count the TX
governor uses for its drain-timeout math (`drive_tx_cycle` derives expected
play time from `samples.len() / sample_rate`, channel-agnostic and mono by
design), so timing would be wrong unless the whole governor learns about
channels. It also drags stereo through the shared cross-platform TX path for an
Android-only feature.

**Option B — Kotlin sink synthesises the tone (RECOMMENDED).**
Keep the Rust/JNI path **mono and unchanged**. `AudioTxPump`, when the active
channel uses digirig tone PTT, runs a **`CHANNEL_OUT_STEREO`** `AudioTrack` and,
for each incoming mono AFSK sample, writes a stereo frame `{L = afsk,
R = tone}`, where `tone` comes from a phase-accumulator oscillator at
`mark_freq` (~0.6 FS, phase reset between transmissions) — a direct Kotlin port
of Rust's `PttTone`. This mirrors desktop's architecture exactly (modem buffer
carries AFSK, the *sink* synthesises the keying tone) and confines all the new
complexity to `AudioTxPump`, where the `AudioTrack` already lives. The TX
governor's drain math stays mono and untouched.

**Tone on/off bracketing.** Reuse the governor's existing key-before-audio /
unkey-after-tail sequencing instead of inventing a new signal. On Android,
`digirig_tone` builds an `AndroidPtt(PTT_METHOD_DIGIRIG_TONE)` driver (not a
`NonePtt`). `UsbPttAdapter.pttSet(DIGIRIG_TONE, true)` → `AudioTxPump.startTone(markHz)`
(returns true); `pttSet(DIGIRIG_TONE, false)` → `AudioTxPump.stopTone()`. The
tone then runs from key to unkey, bracketing the whole transmission — which is
precisely what desktop achieves with the silent lead-in + companion tone. The
`mark_freq` is carried into `startTone` (Rust has it in `config_state`; pass it
across, see Task 5).

## 4. Hardware channel mapping — hard-pin on Android

Desktop kept the per-channel output-channel selector and put the tone on its
companion, requiring the operator to set output to Left (the GRA-168 author
flagged hard-pinning as a possible follow-up). Android has **no per-channel
output-device/channel picker** — it auto-routes to the first USB audio output.
So Android should **hard-pin L = AFSK, R = tone** for this method. No operator
channel selection; the mapping is fixed to match the Digirig Lite wiring.

## 5. Changes by layer

| # | Layer | File(s) | Change |
|---|---|---|---|
| 1 | Proto enum | `proto/platform.proto` | Add `PTT_METHOD_DIGIRIG_TONE = 5;`. Update the `ConfigurePtt.ptt_method` comment in `proto/graywolf.proto` (currently lists 1..4). |
| 2 | Rust consts | `tx/ptt_android_consts.rs` | Add `pub const PTT_METHOD_DIGIRIG_TONE: i32 = 5;` and extend the appendix-B assertion test. |
| 3 | Kotlin consts | `usb/PttMethodConsts.kt` | Add `const val PTT_METHOD_DIGIRIG_TONE = 5`. |
| 4 | Android PTT driver build | `tx/ptt.rs` / `android/mod.rs` | When `method=="android"` and `ptt_method==5`, build `AndroidPtt(5)` (it actuates the tone via the callback) rather than `NonePtt`. Confirm `register_driver`/`build_driver` routes the int through. |
| 5 | Tone synthesis + stereo sink | `audio/AudioTxPump.kt` (+ a small `ToneOscillator`) | Add a stereo "tone mode": `startTone(hz)` / `stopTone()`; when active, open/route a `CHANNEL_OUT_STEREO` `AudioTrack`, write `{L=afsk, R=tone}` per frame; phase-reset on each `startTone`. Detect mono-only USB negotiation and surface it (don't silently drop the tone — desktop only `eprintln`s; Android should report via the status/UI path). |
| 6 | PTT dispatch | `usb/UsbPttAdapter.kt` | `pttSet(PTT_METHOD_DIGIRIG_TONE, keyed)` → `AudioTxPump.startTone/stopTone`, return true. Needs an `AudioTxPump` reference (both are Service-scoped singletons; wire in `GraywolfService.onCreate`). |
| 7 | Silent lead-in | `android/mod.rs` | For a digirig-tone channel, prepend the `DIGIRIG_TONE_LEAD_MS` silent lead-in (reuse `digirig_tone_lead_in`) before submitting, so the left/AFSK channel is silent while the tone leads. (Belt-and-suspenders with the key-before-audio delay; matches desktop.) |
| 8 | Web picker | `web/src/routes/ptt/devices/methodOptions.android.js` | Add `{ method: 'android', ptt_method: 5 }`, label "Digirig Lite (tone PTT)", meta explaining no PTT wire / needs Digirig Lite as USB audio out, `deviceClass` for the USB-audio dongle (no serial device required). |
| 9 | Go/DTO validation | `pkg/webapi/dto/channel.go`, `pkg/configstore/models.go` | Accept `digirig_tone` / android method 5 in the PTT method validation + persistence (desktop already lists `digirig_tone`; confirm the android numeric path allows 5). |
| 10 | Docs | `docs/handbook/ptt.html`, `docs/wiki/` | Document the Android tone-PTT method, the L=AFSK/R=tone fixed mapping, and the stereo-USB requirement. |

## 6. Risks / open questions

1. **USB stereo channel ordering (biggest unknown).** Does the Digirig Lite,
   over Android USB-audio, honour `CHANNEL_OUT_STEREO` frame order as
   L→AFSK / R→tone end-to-end? This must be validated on-device before the
   rest is trusted. If Android/USB swaps or down-mixes channels, the tone lands
   on the wrong wire and the radio never keys.
2. **Mono negotiation fallback.** If the dongle negotiates a mono output on
   Android, the tone cannot be separated. Detect and surface an actionable
   error (the desktop code suppresses with a warn the UI never sees — Android
   should do better and report it).
3. **Stereo vs mono `AudioTrack` switching.** `AudioTxPump` is built once at
   Service boot as mono. Decide: (a) always run stereo and duplicate mono →
   both channels for non-tone methods, or (b) rebuild the `AudioTrack` when the
   channel's PTT method changes. (a) is simpler and avoids a reconfigure race;
   confirm it doesn't disturb the existing mono methods or USB routing.
4. **`mark_freq` to Kotlin.** Carried via `startTone(hz)` from the Rust
   `pttSet` path; confirm `config_state` has the channel's mark freq at key
   time (it is set in the `ConfigureChannel` arm, `set_channel_dsp`).
5. **Multi-channel / hot method-swap.** Out of scope — single channel,
   stop/start to change method (same constraints as phase 4b).

## 7. Testing

- **Rust unit:** appendix-B const test includes `5`; `ptt_uses_digirig_tone`
  recognises `android`+5 if that helper is reused; android arm prepends the
  silent lead-in for method 5 only.
- **Kotlin unit:** `ToneOscillator` phase math (zero-crossing start, continuity,
  reset); `AudioTxPump` writes interleaved `{L=afsk, R=tone}` in tone mode and
  unchanged mono otherwise (extend `AudioTxPumpTest`); `UsbPttAdapter.pttSet(5,…)`
  drives start/stop and returns true (extend `UsbPttAdapterTest`).
- **Web unit:** `methodOptions.test.js` covers the new android entry.
- **Cross-language:** the T13 const-sync test (Kotlin ↔ Rust ↔ proto) gains the
  fifth value.
- **On-device (manual, gated):** Digirig Lite + radio — confirm the radio keys,
  the packet decodes on a reference station, and the tone is on the correct
  (right) wire. This validates risk #1 and is the real acceptance gate.

## 8. Out of scope

- Desktop changes (done in PR #358).
- Per-channel output-device/channel picker on Android (hard-pinned instead).
- Multiple concurrent dongles; hot-swap of PTT method on a live channel.
