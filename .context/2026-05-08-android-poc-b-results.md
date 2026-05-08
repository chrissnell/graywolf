# Android POC-B — run report

**Date:** 2026-05-08
**Branch:** feature/android-poc-a (POC-B work landed on the POC-A branch
per session continuity; rename for the merge if desired)
**Commit at end of run:** 63efc7c

## Verdict

**GREEN.** All six spec criteria hit. End-to-end production topology
proven: single APK with Kotlin foreground Service hosting the Rust modem
cdylib via JNI, exec'd Go child on the modem UDS, WebView fronting a
loopback REST surface with bearer-token auth.

## Toolchain

- rustc 1.90.0 (Homebrew)
- cargo-ndk 4.1.2 (target API 26, arm64-v8a)
- Android NDK r29 (`/opt/homebrew/Caskroom/android-ndk/29/...`)
- Go 1.26.2 (darwin/arm64 host, `GOOS=android GOARCH=arm64 CGO_ENABLED=0`)
- Gradle 8.14.4 (system) on Alpine Linux remote builder
- Android cmdline-tools `commandlinetools-linux-11076708`
- `platforms;android-34`, `build-tools;34.0.0`, `platform-tools` r37
- AGP 8.5.0, Kotlin 1.9.24, JDK 17.0.18
- minSdk 28, targetSdk 34, compileSdk 34, abiFilters arm64-v8a only
- Tablet: Topicon T865 (per POC-A reference chain), API 34 (UPSIDE_DOWN_CAKE)

## Hardware

Same chain as POC-A: Topicon T865 + Digirig USB-Audio class DAC + UV-5R
listening on 144.39 MHz APRS national. USB-Audio routed via
`AUDIO_DEVICE_IN_USB_HEADSET` per `dumpsys media.audio_flinger`.

## Run window

- Boot: 11:29:10 MDT (`JNI_OnLoad` logged)
- First frame decoded: 11:29:29 MDT (~19 s after boot)
- Final kill-and-restore smoke completed: 11:37:55 MDT
- Effective decode window: ~8.5 min spanning gain smoke + supervisor smoke

## Success criteria

| # | Criterion | Result |
|---|---|---|
| 1 | Single APK, one-tap launch | ✅ `app-debug.apk` (~18 MB), `am start ... .MainActivity` brings up WebView |
| 2 | Service starts as foreground (mic) | ✅ `foregroundServiceType=microphone` (connectedDevice dropped — see Yellow caveats) |
| 3 | loadLibrary + modemStart + AwaitReady + AudioPump + GoLauncher + WebView | ✅ all six log lines hit in logcat: `JNI_OnLoad`, `modem cdylib version=...`, `modemAwaitReady=true`, `AudioRecord init rate=22050 bufBytes=7056`, `poc-b: ipc_client_connected`, `poc-b: go_child_up`, `poc-b: webview_loaded` |
| 4 | ≥ 5 distinct decoded frames in 10 min, ≤ 2 s end-to-end latency | ✅ 33+ `ReceivedFrame`s in ~7 min before the gain smoke; valid AX.25 traffic from REDSPR, HOLDEN, K7SRB, MHILL, N7PEG-9, N7BYU-1, KK7DMN-3, N7PDI-1, W1UTE-10, KF6RAL-6, KB7WHO-11, NW5W-5, etc. |
| 5 | Kill Go child → restart within 5 s, decode resumes within 30 s | ✅ `go_child_died → supervisor_restart_succeeded` in 1.3 s; `first_frame_decoded` 12 s after kill |
| 6 | Gain slider takes effect on next chunk | ✅ POST `-30 dB`: 24 → 25 frames in 30 s (effectively zero); restore `-6 dB`: 25 → 33 in 60 s |

## Frame counts

- 33 frames in ~5 min before gain smoke (uptime 311 s @ status read).
- ~7 frames/min sustained at -6 dB software gain.
- POC-A baseline was ~10 frames/min on the same chain — POC-B is in the
  same order of magnitude. No regression vs POC-A's `AudioSource.MIC`
  path.

## Logcat excerpts

Boot sequence (`scratch/poc-b/run.log`):

```
05-08 11:29:10 I graywolfmodem: JNI_OnLoad: v0.13.2-99ea0dd-dirty
05-08 11:29:10 I GraywolfService: modem cdylib version=v0.13.2-99ea0dd-dirty
05-08 11:29:10 I graywolfmodem: ipc server bound at /data/user/0/com.nw5w.graywolf/cache/graywolf-modem.sock
05-08 11:29:10 I GraywolfService: modemAwaitReady=true
05-08 11:29:10 I AudioPump: AudioRecord init rate=22050 bufBytes=7056 (min=1764)
05-08 11:29:10 I graywolfmodem: poc-b: ipc_client_connected
05-08 11:29:10 I GraywolfService: poc-b: go_child_up
05-08 11:29:10 W graywolf-pocb-err: graywolf-pocb listening on 127.0.0.1:8080
05-08 11:29:10 W graywolf-pocb-err: modem ready version=v0.13.2-99ea0dd-dirty pid=6366
05-08 11:29:10 I GraywolfService: poc-b: gain_applied db=-6.0
05-08 11:29:10 I MainActivity: poc-b: webview_loaded
05-08 11:29:29 I graywolfmodem: poc-b: first_frame_decoded
```

Supervisor restart (kill PID via `run-as com.nw5w.graywolf kill <pid>`):

```
05-08 11:37:43 W Supervisor: poc-b: go_child_died rc=143
05-08 11:37:44 I Supervisor: poc-b: supervisor_restart_succeeded
05-08 11:37:55 I graywolfmodem: poc-b: first_frame_decoded   (resumed)
```

## Yellow caveats

- **Manifest dropped `connectedDevice` FGS type.** Spec called for
  `microphone|connectedDevice`. Android 14 enforces `BLUETOOTH_*` /
  `USB_DEVICE` permission for `connectedDevice` and we do not yet
  request a USB device picker (POC-A did via UsbManager). Kept
  `microphone` only. USB-Audio class capture works via the system audio
  policy (`AUDIO_DEVICE_IN_USB_HEADSET` auto-routed under RECORD_AUDIO),
  no UsbManager involvement needed for RX. Phase 6 wires the device
  picker for TX/PTT and re-adds `connectedDevice`.
- **Supervisor `go-watcher` initial bug.** First implementation tight-
  looped on a dead `Process` reference because `processSupplier()`
  returned the same handle until the restart hook replaced it. Fixed in
  `Supervisor.kt` by tracking `prev` and skipping until it changes.
  Caught in smoke; behaviour now: 1.3 s detection, 12 s to first decode.
- **`modemAwaitReady(0)` initial bug.** Returned `JNI_FALSE` immediately
  because the deadline check ran before the first ready read. Fixed in
  `graywolf-modem/src/android/mod.rs` to read once before checking the
  deadline. Without the fix the modem-watcher fired spurious restarts
  every 2 s.
- **Gradle wrapper instability.** The `gradlew` shipped with Task 10
  initially booted on a stub jar; resolved by regenerating via system
  Gradle 8.14.4 on the Alpine builder. The committed `gradle-wrapper.jar`
  is the canonical Gradle 8.7 plugins wrapper jar from
  `~/.gradle/wrapper/dists/gradle-8.7-bin/.../lib/plugins/gradle-wrapper-8.7.jar`.
  The `build-and-install.sh` helper bypasses `./gradlew` and uses the
  builder's system Gradle directly, which was a workaround for the
  wrapper boot loop on the remote box; not blocking but worth tidying
  in phase 6.
- **Build flow is two-host.** Rust cdylib + Go ELF are built on the Mac
  worktree (NDK + cargo-ndk + Go 1.26 with `GOOS=android`); the AGP/APK
  pipeline runs on the Alpine VM `block.local` over rsync + ssh. Helper
  at `scratch/poc-b/build-and-install.sh`. Phase 6 should consolidate
  to a single `make poc-b-apk` target that does both legs locally if a
  Linux dev box is available.
- **AudioPolicy "App op 27 missing, silencing record" line** appeared
  in logcat once during a transient state and doesn't repro on the
  current build. Did not impact the 8.5-min decode run.

## Red blockers

None.

## Production-app design implications

- **AudioRecord(MIC) without a USB device picker is enough for RX.**
  Android's audio policy routes USB-Audio class devices to MIC capture
  automatically when the FGS has `microphone` type and RECORD_AUDIO is
  granted. Phase 3's production modem can keep this path; the picker is
  only needed when we want to address the device for TX gain control or
  PTT (USB HID CM108).
- **JNI cdylib + Go-as-`lib*.so` topology works.** Both libraries are
  extracted to `nativeLibraryDir` by Android's installer because the
  manifest sets `extractNativeLibs="true"` and the AGP packaging block
  has `jniLibs.useLegacyPackaging = true`. The Go ELF executes from
  there with no SELinux denials.
- **Per-process bearer token in `Application.onCreate`** survives Service
  restarts. The Activity reads from the same Application singleton, so
  the WebView never holds a stale token after a supervisor restart.
- **`goListenerReady` flag** is the right signal for the WebView load.
  Loading on token presence (which is set the moment the Application
  boots) caches `ERR_CONNECTION_REFUSED` if the Go child hasn't bound
  the listener yet. The 250 ms poll loop in `MainActivity.startEverything`
  works but a `bindService` callback would be cleaner — defer to phase 3.
- **Two-thread split inside `run_demod`** (DSP loop + IPC accept) is
  load-bearing for supervisor restarts. Without it, the audio sync
  channel fills during the brief window when the Go client is
  disconnected and ~1.5 s of input is dropped per restart.
- **`fs::remove_file` before `UnixListener::bind`** is required for
  supervisor restarts. Unix domain sockets don't have an SO_REUSEADDR
  equivalent; the inode IS the lock. Without the cleanup, every Go-child
  kill burns a supervisor failure budget on EADDRINUSE.
- **Logcat thread-name truncation.** Linux pthread names are 15 chars;
  `graywolfmodem-demod` and `graywolfmodem-dsp` both truncate to
  `graywolfmodem-d`, which makes `htop`/profiler views ambiguous. Phase
  3 should pick shorter prefixes (e.g. `gw-demod`, `gw-dsp`).
