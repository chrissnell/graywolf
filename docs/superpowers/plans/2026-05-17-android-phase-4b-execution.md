# Android Phase 4b — Execution Plan

**Date:** 2026-05-17
**Author:** Chris Snell (executed by Claude subagent-driven-development)
**Spec:** `docs/superpowers/specs/2026-05-16-android-phase-4b-tx-ptt-design.md`
**Base branch:** `feature/android-phase-4a-followup`
**Branch:** `feature/android-phase-4b`

---

## Overview

Decomposes spec §5 (file structure) + §6 (test plan) into 14 tasks ordered
for subagent-driven-development. Each task references its anchor section in
the spec; subagent prompts must quote the spec verbatim for the relevant
section instead of paraphrasing.

Task review gate (per skill): each task gets two reviews after implementer
self-review — (1) spec compliance, (2) code quality. No "close enough" on
spec compliance.

## Task ordering rationale

- **T1 enum keep-in-sync first** — every later task (Rust, Kotlin, SPA)
  references the four PTT method int values. Locking them in T1 prevents
  drift.
- **T2 Rust JNI infra before T3/T4** — AndroidPtt + tx_emit_samples both
  depend on the cached-callback machinery and the android-test-stub
  feature flag. Cut it once.
- **T3, T4 can parallelize** after T2 lands. (Kept sequential here because
  same Rust crate; conflict risk on `Cargo.toml`.)
- **T5 ModemBridge.kt before T6/T7** — both AudioTxPump (T6) and
  UsbPttAdapter.pttSet (T7) reference the new external symbols / callback
  interfaces.
- **T8 manifest before T10** — Service references the
  FGS_TYPE_CONNECTED_DEVICE constant; declaring it requires the manifest
  perm pairing or `startForeground` throws SecurityException.
- **T11 Go REST before T12 SPA** — SPA's POST target has to exist.
- **T13 integration test last among code tasks** — needs all three
  language sources written.
- **T14 cross-compile + live device** is the on-device verification gate.
  Defers JDK 17 / NDK install if local Mac lacks them.

## Tasks

| # | Subject | Spec sections | Key files | Test approach |
|---|---|---|---|---|
| T1 | Rust + Kotlin + proto PTT method constants locked to Appendix B | §3.3, Appendix B | `graywolf-modem/src/tx/ptt_android.rs` (constants block), `android/app/src/main/kotlin/com/nw5w/graywolf/usb/PttMethodConsts.kt` (new) | inspect; T13 enforces |
| T2 | Rust JNI bridge — install + invoke PTT and TX-audio callbacks; add `android-test-stub` feature | §3.3, §3.4 | `graywolf-modem/src/android/mod.rs`, `graywolf-modem/Cargo.toml` | host-side smoke test with stub |
| T3 | Rust `AndroidPtt` PttDriver impl + `PttMethod::Android` variant + `build_driver` dispatch | §3.3, §5.1, §5.2 | `graywolf-modem/src/tx/ptt_android.rs` (new), `graywolf-modem/src/tx/ptt.rs` | `cargo test` |
| T4 | Rust `tx_emit_samples` that invokes Kotlin `AudioTxPump.pushSamples` via cached callback | §3.2, §5.2 | `graywolf-modem/src/android/audio.rs`, `graywolf-modem/src/android/mod.rs` | `cargo test` |
| T5 | Kotlin `ModemBridge` JNI surface — install callbacks; `UsbPttCallback` + `AudioTxCallback` interfaces | §3.3, §3.4 | `android/app/src/main/kotlin/com/nw5w/graywolf/jni/ModemBridge.kt` | type-check only |
| T6 | Kotlin `AudioTxPump` — mirror of `AudioPump` RX; AudioTrack STREAM; auto-route to USB output; rebind on hot-swap | §3.2, §6.1 | `android/app/src/main/kotlin/com/nw5w/graywolf/audio/AudioTxPump.kt` (new) + `AudioTxPumpTest.kt` (new) | unit |
| T7 | Kotlin `UsbPttAdapter` — implement `UsbPttCallback.pttSet(method, keyed)` dispatcher | §3.4, §6.1 | `android/app/src/main/kotlin/com/nw5w/graywolf/usb/UsbPttAdapter.kt` | unit |
| T8 | Android manifest re-adds `FOREGROUND_SERVICE_CONNECTED_DEVICE`, USB intent-filter, FGS bitmap; `res/xml/device_filter.xml` with Appendix A payload | §3.6, Appendix A | `android/app/src/main/AndroidManifest.xml`, `android/app/src/main/res/xml/device_filter.xml` (new) | manifest schema verify |
| T9 | Kotlin `WebAppInterface` — `listUsbDevices()` + `requestUsbPermission(vid,pid,cbId)` JS-bridge methods | §3.6 | `android/app/src/main/kotlin/com/nw5w/graywolf/webview/WebAppInterface.kt`, `android/app/src/main/kotlin/com/nw5w/graywolf/usb/UsbPttAdapter.kt` (add `requestPermission(vid,pid,cb)`, `enumerateForJs(): JSONArray`) | unit |
| T10 | Kotlin `GraywolfService` lifecycle wiring — onCreate `audioTxPump.start()`, `usbPttAdapter.init/enumerate()`, install callbacks; onDestroy reverse; FGS bitmap conditional on USB_DEVICE perm grant | §3.1, §3.6 | `android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt` | logcat assertion via test plan §6.5 |
| T11 | Go modembridge `ManualPtt` RPC + `/api/channels/{id}/ptt` REST + 10s watchdog | §3.5, §6.3 | `pkg/modembridge/...`, `pkg/app/...`, `graywolf-modem/src/tx/` (governor settings if applicable) | Go unit |
| T12 | SPA `Channels.svelte` — PTT method dropdown, Test-PTT press-and-hold toggle, USB hardware status, audio routing display, un-hide TX-delay/tail-delay/repeats/packets_tx; `postChannelPtt(id, keyed)` API | §3.7, §6.4 | `web/src/routes/Channels.svelte`, `web/src/lib/api.js` | `node --test` |
| T13 | Cross-language PTT method enum keep-in-sync integration test | §6.6 last note, Appendix B last paragraph | new test files; references Rust + Kotlin + Go | integration test |
| T14 | Cross-compile (cargo ndk arm64-v8a + x86_64), gradle `:app:assembleDebug`, install on T865, execute §6.5 live device tests | §6.5, §6.6, §7 | n/a — verification only | live |

## Build environment

- **Local Mac**: has Go 1.26, Rust + android targets, adb, gradlew script. Missing: JDK 17, Android NDK r27c+, cargo-ndk. Sufficient for tasks T1–T13 (Rust unit tests via host target with `android-test-stub`; Go via `GOWORK=off`; Kotlin code edits don't require Gradle compile until T14).
- **block.local Linux VM**: has JDK 21 (not 17), gradle, protoc, adb, cargo. Missing: NDK, Go, cargo-ndk. **Not** the primary build host.
- **T14 install plan**: brew install openjdk@17, sdkmanager "ndk;27.2.12479018", `cargo install cargo-ndk --locked`. Then `gradle :app:assembleDebug`. Adb install on T865.

## Out-of-scope reminders (from spec §2)

- iGate counter dashboard (→ 4c)
- Modem-optional boot (→ 4d)
- Multi-dongle concurrent operation
- Per-channel output-device picker
- Bluetooth SPP serial PTT (→ 5c)
- `AudioManager.setCommunicationDevice` system routing
- Hot-swap of PTT method while channel is active
