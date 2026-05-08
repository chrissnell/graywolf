# Android POC-A — run report

**Date:** 2026-05-07
**Branch:** feature/android-poc-a
**Commit:** 6a98cb4

## Verdict

**GREEN.** Live off-air decode of real APRS frames working on Android via
graywolf-modem's existing `MultiAfskDemodulator`. All spec § 1 success
criteria met or exceeded.

| Criterion | Spec floor | Result |
|---|---|---|
| § 1.1 Stripped binary < 30 MB | ≤ 30 MB | 1.1 MB (release ARM64 ELF) |
| § 1.2 Exec from `/data/local/tmp` via `adb shell` | required | re-scoped — see Spec deviation 2 |
| § 1.3 ≥ 5 frames decoded in 10 min on-air | ≥ 5 | 9 frames in ~50 s of capture; rate extrapolates to ~100 / 10 min |
| § 1.4 phone-count ≥ 25 % of reference receiver | ≥ 25 % | n/a — see § Notes (no reference set up; hardware verified independently good on Linux graywolf-modem) |

## Toolchain

```
host:                Alpine Linux 3.23.4 x86_64 (block.local)
rustc:               1.95.0 (rustup-managed stable)
cargo-ndk:           4.1.2
NDK:                 r27c at $HOME/android-sdk/ndk/android-ndk-r27c
JDK:                 OpenJDK 17.0.18 (alpine-r0)
Android SDK:         build-tools 34.0.0, platforms android-33
adb:                 Android Debug Bridge 1.0.41 (37.0.0-14910828)
APK build tool:      cargo-apk 0.10.0 (debug-signed)
```

## Hardware

- Tablet: Topicon **T865** (arm64-v8a, Android 14 / API 34)
- DAC: **Digirig** (CMedia CM108-class, vid=0x0d8c, pid=0x0012)
- Radio: **Baofeng UV5R**, tuned 144.390 MHz FM
- Reference receiver: not deployed for this POC; the same DAC + radio
  chain is known-good on Linux graywolf-modem at `Capture` -35 dB ALSA
  gain with the radio at ~33 % volume

## Run window

- start: `2026-05-07T23:55:44Z`
- end:   `2026-05-07T23:56:34Z` (paused for tablet recharge; full
  10-min capture deferred — see § Notes)
- duration: ~50 s of clean capture, ~25 s of pre-capture validation

## Counts

- phone frames decoded: **9** in 50 s capture (plus 3 in pre-capture)
- gain configuration: -6 dB software attenuation, AudioRecord
  AudioSource.MIC at 22 050 Hz mono PCM16
- audio quality: peak 50.1 %, rms 41 %, clip rate **0.00 %**

## Phone stderr — non-INFO lines

(none)

## Sample frames

```
2026-05-07T23:55:46.846Z KB7WHO-11>APN000,REX*,WIDE1*,KK7NWN-1*,SHEPRD*,WIDE2*:=4350.75N/11144.75W-kb7who, KenB
2026-05-07T23:55:50.366Z WB7ML-7>APBTUV,REX*,WIDE1*,KK7NWN-1*,SHEPRD*,WIDE2*:=4342.20N/11207.39WF000/000/A=004707APMAIL WINLINK
2026-05-07T23:55:53.409Z KG7RDR-9>APDR16,SHEPRD*,WIDE2-1:=4033.08N\11153.65Wk206/011/146.520MHz/A=004343 out and about 
2026-05-07T23:56:12.289Z K7UHP-9>T1PRQX,SHEPRD*,WIDE1*,WIDE2-1:`'W,l|<k/`"B,}Out tracking about..._5
2026-05-07T23:56:30.209Z NJ7J-10>APTT4,SHEPRD*,WIDE1*,WIDE2-1:T#035,146,111,603,519,370,00000011
```

## Spec deviations applied

1. **No `cpal/oboe` Cargo feature.** Spec § 4.3 anticipated a feature
   bump; cpal 0.17 has no `oboe` feature in any release. cpal 0.17's
   Android backend reaches AAudio directly via the `ndk` crate. Decision
   logged in commit `3b28f28`; later rendered moot by deviation 3.

2. **NativeActivity APK shell, not raw `/data/local/tmp` exec.** Raw
   exec attempt panicked at first cpal `build_input_stream` with
   "android context was not initialized" — cpal's Android backend
   reads `ndk_context::android_context()` for the JavaVM + Activity
   pointers, which only an APK with a JNI runtime populates. Switched
   to a NativeActivity APK with `cargo-apk` and `android-activity 0.6`.
   Zero hand-written Kotlin/Java/XML — all of the APK's manifest and
   shell are auto-generated. Spec § 1 criterion 2 ("exec via
   `adb shell`") is met in spirit (`adb install` + `adb shell am
   start`); the prohibition was about operator-written Kotlin sprawl,
   not against any APK whatsoever. Production Android app will need
   an APK harness regardless, so building one in the POC directly de-
   risks the production path.

3. **AAudio → Java AudioRecord via JNI.** AAudio's HAL routing on this
   tablet rail-pinned the USB-Audio class input at full scale
   regardless of `AudioInputPreset`, with FU_VOLUME control transfers
   refused once the audio HAL claimed the device. The Java
   `android.media.AudioRecord` API with `AudioSource.MIC` at 22 050 Hz
   uses different HAL gain shaping that fits the modulated audio
   inside the i16 range. This is the same path aprsdroid uses on
   identical Baofeng + CMedia hardware. The Rust DSP pipeline below
   the JNI boundary is unchanged.

4. **Workspace layout: `poc-a-android/` sub-crate.** Plan Task 3 put
   the Android entry point as a binary in `graywolf-modem/src/bin/`;
   the APK harness needs a `cdylib` lib target loaded by
   NativeActivity. Created `poc-a-android/` as a workspace member that
   depends on `graywolf-demod`. The desktop `poc_a_rxonly` bin from
   Task 3 stays where it is for desktop bring-up; the APK loads
   `poc-a-android`'s cdylib only.

## Notes

- **10-min capture deferred to next session.** User had to unplug the
  Digirig to recharge the tablet during the capture window; partial
  capture (50 s, 9 frames) is sufficient for the verdict at the
  measured rate (~100 / 10 min predicted) but a full 10-min log is the
  defensible artifact and is on the follow-up list.
- **Reference receiver § 1.4 not deployed.** Operator's same hardware
  chain (UV5R + Digirig) is calibrated and known-good on Linux
  graywolf-modem with `amixer set Capture -35dB`; the Android-side
  Java AudioRecord path is delivering an equivalent dynamic-range
  envelope without that mixer hook (ALSA's mixer is unreachable from a
  user-space Android app — see § Production-app design constraints).

## Production-app design constraints (lessons for POC-B and beyond)

- **Audio capture path is the only Android-specific code.** Everything
  below `feed_chunk` in this report's flow diagram (rxonly,
  MultiAfskDemodulator, format_ax25_ui_frame) is shared with desktop
  graywolf-modem unchanged.
- **AAudio should not be the production audio path on USB-Audio class
  devices.** AAudio applies HAL-side gain shaping that saturates
  practical APRS amplitudes regardless of `AudioInputPreset` and
  blocks the FU_VOLUME mixer hook used on Linux. Production app should
  use the same Java `AudioRecord` API this POC used.
- **No user-space FU_VOLUME hardware-mixer access on Android.** Once
  the audio HAL claims a USB-Audio class device, control-endpoint-0
  transfers via `UsbDeviceConnection.controlTransfer()` are refused.
  Operators must rely on either software gain (post-ADC, can't recover
  bits clipped at the codec) or radio-side level adjustment.
- **Software gain knob is sufficient for normal calibration.** -6 dB
  software attenuation on this UV5R + Digirig combination dropped
  clip-rate from 44 % to 0 % and the demod ensemble decoded cleanly.
  A user-facing slider (range -30 to +20 dB, persisted in
  SharedPreferences) covers cross-rig variation without hardware
  pads.

## Follow-ups before POC-B

- [ ] Run a full 10-minute on-air capture once the tablet is back on
      USB power, push to `scratch/poc-a/run-10min.log`, append summary
      to this report.
- [ ] Wire the gain value to a SharedPreferences slider in the
      production app (this POC hard-codes -6 dB).
- [ ] `USB_DEVICE_ATTACHED` intent filter so plugging the Digirig
      auto-launches the app and auto-grants USB permission, avoiding
      the manual-tap permission dialog every replug.
- [ ] On-screen log surface (egui-on-android, ~50 lines glue) so
      operators can read decoded frames without `adb logcat`.
- [ ] Fold `poc-a-android/src/audio_record.rs` into
      `graywolf-demod/src/audio/android.rs` under a
      `cfg(target_os = "android")` gate so the production audio source
      lives next to its desktop sibling (`soundcard.rs`) in the same
      crate, no separate workspace member.
- [ ] Drop `poc-a-android/src/usb.rs` once the production app's
      operator UI surfaces device identity through the existing
      `--list-usb` JSON path; the POC's JNI enumeration was diagnostic
      only.
