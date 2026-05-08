# Graywolf for Android - design spec

**Status:** draft, revised after POC-A
**Date:** 2026-05-01 (revised 2026-05-07)
**Scope:** native Android app providing full standalone TNC + iGate functionality on a phone, sourced from the existing graywolf codebase with no fork.

**Revision log:**
- 2026-05-06: corrected GPS-permission typo in §5.4; added explicit
  `//go:build !android` note for `_unix.go` files; documented Play
  Store `versionCode` ceiling in N5; added invariants N6
  (audio-route-change policy) and N7 (localhost bearer token); split
  PTT phase into 5 / 5b / 5c; added POC-A and POC-B as phase 1 / 1b;
  added §11.1 battery baseline; added Doze and localhost-surface
  risks (#6, #7) to §12; added `AudioRouteChanged` proto message and
  `AudioDeviceCallback` registration to AudioAdapter.
- 2026-05-07: revised after POC-A run report (PR #87). Modem ships as
  a `cdylib` loaded into the Kotlin Service via JNI, **not** as an
  exec'd `lib*.so` ELF (cpal / native code requires `ndk_context`
  populated by an APK JNI runtime). Audio path switched from CPAL
  Oboe to Java `AudioRecord` + JNI sample hand-off (AAudio rail-pins
  USB-Audio class capture and blocks FU_VOLUME). USB hardware-mixer
  gain is unreachable from user-space on Android — software gain
  only, exposed as an operator slider. Process tree drops from 3 to
  2 (Service-with-modem-JNI + Go child). New invariants N8 (JNI
  audio path) and N9 (software-only gain). §12 risk #1 retired,
  replaced with HAL-variance risk #1'. Phase 1 marked complete; POC-B
  rescoped — see new §11.2.

---

## 1. Goal

Ship a Play Store Android app that runs the graywolf Go service and the
graywolf-modem Rust binary on a phone, fronted by the existing web UI in a
WebView. The app is a full TNC and iGate: it captures audio from a USB or
3.5mm-jack sound device, drives PTT through Android-supported methods,
runs all existing graywolf subsystems (AX.25, KISS server/client, AGW,
digipeater, iGate, beacons, livemap, offline maps, history, log buffer,
flare diagnostics), and feeds GPS from `LocationManager`.

Source of truth stays in the `graywolf` repo. The Android app is a new
build target, not a fork. New core graywolf features ride along
automatically; build tags gate code only where Android cannot host it.

## 2. Non-goals (v1)

- Acoustic-coupled audio (phone mic against radio speaker). Wired only.
- GPIO PTT, parallel port PTT, native serial PTT (`/dev/ttyUSB*`),
  hamlib-rigctl over local subprocess. Hamlib-rigctl over TCP retained
  because it is portable.
- iOS port. Different OS, different sandbox, different toolchain.
- F-Droid. Distribution is Play Store only (paid). Self-builders may
  still build their own APK from source under GPLv2; that is accepted.
- In-app updates checker (`pkg/updatescheck`). Play Store handles
  updates.
- Replacing the existing web UI with native Compose screens. WebView
  fronts the SPA.

## 3. Architecture

### 3.1 Process tree on Android

```
Android System
  +- GraywolfService (Kotlin foreground service)
  |    +- loadLibrary: libgraywolfmodem.so   (Rust cdylib, in-process via JNI)
  |    +- exec:        libgraywolf.so        (Go child process, was bin/graywolf)
  |    +- hosts:       platform-services UDS server (Kotlin)
  |    +- runs:        AudioRecord pump (Kotlin) -> JNI sample buffer hand-off -> Rust modem
  +- MainActivity
       +- WebView -> http://127.0.0.1:8080  (Go REST + embedded SPA)
```

Two reasons the modem is **not** an exec'd ELF child:

1. **`ndk_context` requirement.** Cpal's Android backend, ndk-audio,
   and JNI-using crates pull `ndk_context::android_context()` for the
   `JavaVM` + `Activity`/`Context` pointers. That global is populated
   only inside an APK JNI runtime. POC-A confirmed that running the
   modem as an exec'd binary from `/data/local/tmp` panics on first
   audio-stream open with "android context was not initialized."
2. **No HAL audio gain control from outside the Activity context.**
   The audio HAL claims a USB-Audio device once a stream is opened,
   blocking FU_VOLUME control transfers from any other holder. Audio
   capture must therefore live inside the same JNI process as the
   modem code — splitting them across an exec boundary loses the
   shared-context that makes capture work in the first place.

Consequences:

- **`libgraywolfmodem.so` is a Rust `cdylib`**, not an ELF
  executable. The Service calls `System.loadLibrary("graywolfmodem")`
  at startup. The crate exposes a JNI `Java_…_modemStart` entry point
  that boots the modem's runtime + DSP pipelines on a worker thread
  inside the Service process.
- **`libgraywolf.so` is still an exec'd ELF child** of the Service
  (Go binary, `Runtime.exec()`). Go doesn't need `ndk_context` and
  benefits from process isolation: a Rust modem panic doesn't take
  the Go logic with it. The manifest still needs
  `extractNativeLibs="true"` + `useLegacyPackaging=true` so the Go
  ELF is extracted to disk for exec.
- **AudioRecord pump lives in Kotlin** because `android.media.AudioRecord`
  is a Java API. Kotlin owns the `AudioRecord` instance and calls into
  Rust over JNI on each fixed-size sample buffer (~2048 samples typical;
  matches POC-A). The Rust side enqueues into the existing modem
  ingestion path and returns immediately so the audio thread doesn't
  block on DSP.

The exec'd-binary model from earlier revisions of this doc is gone.
Invariant N1 stands for the Go child only; the modem cdylib reaches
the Service via `loadLibrary`, not extraction.

### 3.2 IPC channels

Three channels plus an in-process JNI bridge under the Service. All
proto channels use length-prefixed protobuf, matching the existing
graywolf wire format `[4 BE bytes length][message]`.

1. **Go <-> Rust modem** - existing `proto/graywolf.proto` UDS at
   `${app.cacheDir}/graywolf-modem.sock`. Extended with new PTT-event
   messages and three new `ConfigurePtt.method` enum values
   (`ANDROID_USB_SERIAL`, `ANDROID_USB_HID`, `ANDROID_BT_SPP`). All
   existing message types unchanged. The Rust side is in-process to
   the Kotlin Service (cdylib loaded via JNI) but speaks the same UDS
   protocol — the modem opens a unix socket from inside the JNI worker
   thread; Go connects from the exec'd child process. UDS path is
   under `${app.cacheDir}` and inherited via env var, exactly as on
   desktop. This shape preserves the existing modembridge code on the
   Go side and the existing IPC server on the Rust side; nothing
   changes below the wire.

   The supported Android PTT method set is: existing `NONE` (operator
   relies on the radio's VOX from transmitted audio), plus the three
   new enum values above. Other existing PTT methods (GPIO, native
   serial, hamlib subprocess, CM108 via direct hidraw write, etc.)
   compile out of the Android binary via `cfg(target_os="android")`.
   Hamlib over TCP (`HAMLIB_RIGCTL_TCP`) is portable and remains
   available.

2. **Go <-> Kotlin platform services** - new `proto/platform.proto` UDS
   at `${app.cacheDir}/platform.sock`. Bidirectional. Carries:
   - `GpsFix` (Kotlin -> Go) - location updates
   - `UsbAttach` / `UsbDetach` (Kotlin -> Go) - hot-plug events
   - `UsbDeviceList` request/response (Go -> Kotlin) - settings UI
     enumerator
   - `UsbSelect` request/response (Go -> Kotlin) - user picks device,
     Kotlin opens the fd and retains it
   - `PttKeyRequest` / `PttUnkeyRequest` / `PttAck` (Go -> Kotlin) -
     relayed from Rust modem PTT events
   - `AudioDeviceList` request/response (Go -> Kotlin) - settings UI
     enumerator (Kotlin owns the actual `AudioRecord` pump)
   - `AudioRouteChanged` (Kotlin -> Go) - device added/removed event
     so the Service can stop and restart the AudioRecord pump and the
     modem can abort in-flight TX and reopen its capture-side queue
   - `BatteryState` (Kotlin -> Go) - charging/level for diagnostics

There is a fourth, **in-process** channel that is not over a socket
because both endpoints live in the Service's address space:

4. **Kotlin AudioRecord pump <-> Rust modem** — JNI sample buffer
   hand-off. Kotlin runs `AudioRecord` on a high-priority thread,
   reads PCM16 samples in fixed-size chunks (~2048 samples at
   22 050 Hz), and calls a JNI entrypoint
   (`Java_…_modemPushSamples(ByteBuffer, int)`) on each chunk. The
   Rust side reads from the buffer (no copy where the JNI layer can
   pass a direct ByteBuffer pointer), enqueues into the demod
   pipeline's existing input ring, and returns. The audio thread
   never blocks on DSP. POC-A confirmed this pattern works at
   sustained throughput on a real device.

3. **WebView <-> Go** - existing HTTP on `127.0.0.1:8080`, gated by a
   per-launch 32-byte bearer token (see invariant N7). The Service
   generates the token at cold start and injects it into the WebView
   via `addJavascriptInterface` before the first navigation. The SPA
   is the same one served by desktop graywolf, embedded via
   `go:embed all:dist` per invariant 12; on Android it reads the
   injected token and adds it to every request as
   `Authorization: Bearer <token>`. Other apps on the device can reach
   the loopback port but cannot mint a valid token.

A small `@JavascriptInterface` exposes Kotlin to the WebView for
system-required user prompts only (USB device picker confirmation,
runtime permission requests) and the bearer-token handoff. Functional
API surface stays in Go REST.

### 3.3 PTT relay rationale

Android does not expose a kernel CDC/FTDI/CP2102 driver. USB-serial
control happens entirely in userspace via `usb-serial-for-android`,
which requires a Java/Kotlin owner of the `UsbDeviceConnection`. CM108
USB HID and Bluetooth SPP serial have the same constraint - the fd is
held by Kotlin.

Therefore Rust drivers for these PTT methods do not touch hardware.
They emit `KeyPttRequest` / `UnkeyPttRequest` proto messages on the
existing Go<->Rust UDS, Go relays the message to Kotlin via
platform-services UDS, Kotlin actually toggles the line. The path is
microsecond-scale UDS hops; total budget under 5ms typical.

The TX ordering invariant is unchanged: PTT keyed before audio starts,
unkeyed after audio drains. txgovernor (invariant 16) remains the
single source of truth for the TX path.

### 3.4 Modem readiness signal (invariant 13)

Adapted, not preserved. On Linux desktop the modem ELF writes `\n` to
stdout once it has bound its UDS server; modembridge waits on stdin.
On Android the modem is a cdylib loaded into the Service, so there is
no stdout. Equivalent shape:

- The Service calls `Java_…_modemStart(socket_path, …)` on a worker
  thread.
- The Rust side binds the UDS server, then returns from a separate
  blocking JNI call `Java_…_modemAwaitReady(timeout_ms)` once the
  socket is accepting connections.
- Only after `modemAwaitReady` returns success does the Service exec
  the Go child. Go's modembridge connects to the same UDS path and
  observes the same wire protocol it uses on desktop.

The intent of invariant 13 — "do not let the Go side connect before
the modem can answer" — is upheld. The mechanism (JNI return code vs.
stdout byte) differs only because the Rust code lives in the same
process now. Captures of Rust panics/log output are routed through
`android_logger` into logcat (and via Go relay into
`graywolf-logs.db`).

### 3.5 Version display string (invariant 14)

Adapted. The cdylib exports a JNI `Java_…_modemVersion()` returning
`v<Version>-<GitCommit>`. The Service reads it at startup. Go binary
unchanged: it still emits its own version string at startup. Service
compares both against APK `versionName` (read via
`PackageManager`). Mismatch on any pair → terminate, log, restart.
CI keeps the existing equality check between Go binary, Rust crate,
and APK `versionName`/`VERSION`.

## 4. New code

### 4.1 Kotlin (`android/app/`)

| File | Purpose |
|---|---|
| `MainActivity.kt` | owns the WebView, lifecycle, requests POST_NOTIFICATIONS perm on launch |
| `GraywolfService.kt` | foreground service. Calls `System.loadLibrary("graywolfmodem")` and the JNI `modemStart` / `modemAwaitReady` entry points. Execs the Go child binary (separate process). Owns the platform-services socket and the supervisor restart loop. |
| `binaries/GoLauncher.kt` | exec helpers for the Go child: spawn `libgraywolf.so` from `applicationInfo.nativeLibraryDir`, capture stdout/stderr into logcat, wait for the Go-side readiness byte, supervise crashes with backoff. (Modem is in-process, not exec'd; this launcher is Go-only.) |
| `audio/AudioPump.kt` | `AudioRecord` capture loop on a high-priority thread. PCM16, mono, 22 050 Hz to match POC-A. Pushes each chunk to Rust via JNI `modemPushSamples`. Started/stopped by the Service in response to RX-enable state, audio-route changes (N6), and TX windows (N6: pump pauses during TX so AudioRecord doesn't echo the modem's own audio output back as input). |
| `jni/ModemBridge.kt` | thin Kotlin facade over the Rust JNI surface (`modemStart`, `modemAwaitReady`, `modemPushSamples`, `modemSetGainDb`, `modemVersion`, `modemStop`). Also routes Rust-side log lines to logcat. |
| `platformsvc/PlatformServer.kt` | UDS listener, framing, dispatch to adapters |
| `platformsvc/GpsAdapter.kt` | `LocationManager` listener -> `GpsFix` proto |
| `platformsvc/UsbAdapter.kt` | `UsbManager` enumerator + `usb-serial-for-android` driver + CM108 HID write + `BluetoothSocket` RFCOMM. Owns retained device connections. |
| `platformsvc/AudioAdapter.kt` | `AudioManager.getDevices()` enumerator. Kotlin owns the actual `AudioRecord` pump (see `audio/AudioPump.kt`). Also registers `AudioDeviceCallback` and pushes `AudioRouteChanged` proto on add/remove (drives invariant N6). |
| `platformsvc/BatteryAdapter.kt` | `BATTERY_CHANGED` broadcast -> `BatteryState` proto |
| `webview/WebAppInterface.kt` | minimal JavascriptInterface: USB picker confirmation, runtime perm prompt, gain-slider state, bearer-token handoff |
| `webview/PermissionBridge.kt` | wraps Android runtime permission flow |

Kotlin sources only. No Java. Min SDK 28, target SDK 34.

### 4.2 Proto (`proto/platform.proto`)

New file. Mirrors the envelope pattern from `graywolf.proto` (single
top-level `PlatformMessage` with `oneof body`). Generates Go bindings
into `pkg/platformproto/` (committed, regenerated by `make proto`,
covered by invariant 11's drift guard). Generates Kotlin bindings via
`com.google.protobuf` Gradle plugin during `assembleRelease`.

Message types:
- `Hello` (handshake, schema version)
- `GpsFix`
- `BatteryState`
- `UsbAttach`, `UsbDetach`
- `UsbDeviceListRequest`, `UsbDeviceListResponse`
- `UsbSelectRequest`, `UsbSelectResponse`
- `PttKeyRequest`, `PttUnkeyRequest`, `PttAck`
- `AudioDeviceListRequest`, `AudioDeviceListResponse`
- `AudioRouteChanged`
- `Error` (generic, with code + message)

Schema versioning: `Hello.schema_version` (uint32). Mismatch -> Service
logs warning, terminates Go + modem, restarts. Same retag tolerance as
graywolf.proto.

### 4.3 Go packages

| Path | Build tag | Purpose |
|---|---|---|
| `pkg/platformsvc/` | `android` | UDS client, proto codec, retry + reconnect, exposed as a typed Go API consumed by `pkg/gps`, `pkg/pttdevice`, `pkg/modembridge` |
| `pkg/platformproto/` | none | generated proto bindings (committed) |
| `pkg/gps/android.go` | `android` | consumes `GpsFix` from platformsvc, feeds existing Fix pipeline |
| `pkg/gps/gpsd_unix.go`, `pkg/gps/serial_unix.go` | `!android` | existing gpsd + serial NMEA, hidden on Android. *Note:* Android is `unix` to Go's build constraints, so these files require an explicit `//go:build !android` line at top — the `_unix` filename suffix alone will not exclude them. |
| `pkg/pttdevice/android.go` | `android` | enumerator that reports the four Android PTT methods and queries Kotlin for live USB/BT device list |
| `pkg/pttdevice/linux.go`, `pkg/pttdevice/windows.go`, `pkg/pttdevice/darwin.go` | `!android` | existing enumerators, hidden on Android |
| `pkg/updatescheck/*.go` | `!android` | existing GitHub poll, hidden on Android |
| `cmd/graywolf/main_android.go` | `android` | entry that reads env-injected paths, no `signal.Notify`, no `os.Exit` on clean shutdown (Android Service kills the process) |
| `cmd/graywolf/main_unix.go`, `main_windows.go` | `!android` | existing entries, build-tagged |
| `pkg/diagcollect/review/` | `!android` | existing bubbletea TUI, hidden on Android |
| `pkg/diagcollect/review_android.go` | `android` | new HTTP/WebView review surface; uses the same scrubbed payload, exposes it through a `/api/flare/review` endpoint |

### 4.4 Rust modem changes (`graywolf-modem/`)

The crate gains a new `cdylib` lib target alongside the existing
desktop ELF binary. Same source tree, two output shapes selected by
target triple.

| File | Purpose |
|---|---|
| `Cargo.toml` | adds `[lib] crate-type = ["cdylib", "rlib"]` for the Android build; `cdylib` builds to `libgraywolfmodem.so` placed under `android/app/src/main/jniLibs/<arch>/`. Adds `jni` and `android_logger` deps gated on `cfg(target_os="android")`. **No** CPAL Oboe feature — that path proved unviable; see invariant N8. |
| `src/lib.rs` | exposes a public `run_rx(samples: &[i16], gain_db: f32) -> Vec<DecodedFrame>` (or equivalent stream-style API) so both the desktop binary and the Android JNI layer call into the same DSP. |
| `src/android/mod.rs` | `cfg(target_os="android")`-only module containing JNI entry points (`Java_…_modemStart`, `_modemAwaitReady`, `_modemPushSamples`, `_modemSetGainDb`, `_modemVersion`, `_modemStop`). Owns the in-process IPC server (UDS) and the demod ingest queue. |
| `src/android/audio.rs` | consumer side of the Kotlin->Rust JNI sample hand-off. Wraps the JNI `ByteBuffer` into an `&[i16]` slice, applies the operator-set software gain (N9), enqueues to the demod input ring. |
| `src/tx/ptt_android_usb_serial.rs` | thin driver: emits `KeyPttRequest` / `UnkeyPttRequest` over the existing Go<->Rust UDS, no hardware access. Compiles on Android only. |
| `src/tx/ptt_android_usb_hid.rs` | same pattern |
| `src/tx/ptt_android_bt_spp.rs` | same pattern |
| `src/tx/mod.rs` | dispatches new method enum values |

`build.rs` regenerates from the same `proto/graywolf.proto` (invariant
2 preserved). Cross-compile via `cargo-ndk -t arm64-v8a -t x86_64
build --lib --release`. Output `.so` is staged into the APK's
`jniLibs/<arch>/` tree, where Android's `System.loadLibrary` resolves
it.

Existing PTT drivers (`ptt_serial.rs`, `ptt_cm108.rs`, etc.) and the
desktop CPAL audio source compile out on `cfg(target_os="android")`
so the cdylib stays small. The desktop ELF binary
(`bin/graywolf-modem`) is unchanged — it keeps building from the same
crate via the `[[bin]]` entry, with desktop-only CPAL gated by
`cfg(not(target_os="android"))`.

POC-A's `poc-a-android/` workspace member is folded into this crate
as `src/android/audio.rs` so production audio capture lives next to
its desktop sibling and shares the same `graywolf-demod` library
calls. The standalone `poc-a-android/` directory is removed after
the fold-in.

### 4.5 Build system

`Makefile` adds:

```makefile
# Modem: cdylib (libgraywolfmodem.so), loaded via System.loadLibrary
android-modem:
	cargo ndk -t arm64-v8a -t x86_64 -o $(ANDROID_JNI_LIBS) \
		build --lib --release --manifest-path graywolf-modem/Cargo.toml
	# cargo-ndk drops libgraywolf_demod.so under arch dirs; rename to
	# libgraywolfmodem.so to match System.loadLibrary("graywolfmodem")

# Go: ELF binary, exec'd as a child by the Service
android-graywolf:
	GOOS=android GOARCH=arm64 CGO_ENABLED=0 \
		go build -tags android -o $(ANDROID_JNI_LIBS)/arm64-v8a/libgraywolf.so ./cmd/graywolf
	GOOS=android GOARCH=amd64 CGO_ENABLED=0 \
		go build -tags android -o $(ANDROID_JNI_LIBS)/x86_64/libgraywolf.so ./cmd/graywolf

android: web proto android-modem android-graywolf
	cd android && ./gradlew bundleRelease assembleRelease
```

The Go side keeps the `lib*.so` extension because Android only
extracts `lib*.so` patterns out of the APK at install time
(invariant N1) — this is a packaging trick, not a real shared
library. The Rust side is a genuine shared library.

`make bump-*` extended to rewrite `android/app/build.gradle.kts`:
- `versionName` = VERSION
- `versionCode` = `major * 1000000 + minor * 1000 + patch` (e.g. `0.12.6` -> `12006`, `1.0.0` -> `1000000`). Capped at Play Store's 2^31 limit. Each release is strictly monotonic.

Invariant 3 grows by one file. The bump targets already commit
generated artifacts; the gradle file goes into the same commit.

### 4.6 CI

New workflow `.github/workflows/release-android.yml`:

- Trigger: tag push `v*`, after the existing Rust build matrix completes.
- Stages:
  1. Set up NDK r27, Java 17, Gradle.
  2. `make android-modem android-graywolf` for both arches.
  3. `cd android && ./gradlew bundleRelease assembleRelease` with the
     upload key from `secrets.ANDROID_UPLOAD_KEYSTORE_BASE64`.
  4. Upload `.aab` to Play Store internal testing track via
     `r0adkll/upload-google-play-action`.
- Outputs: `app-release.aab` (Play), `app-release.apk` (signed sideload
  artifact, attached to the GitHub release).

New workflow `.github/workflows/android-build-smoke.yml`:

- Trigger: PR touching `android/**`, `cmd/graywolf/**`,
  `graywolf-modem/**`, `proto/**`, `pkg/platformsvc/**`,
  `pkg/platformproto/**`, `pkg/gps/**`, `pkg/pttdevice/**`,
  `cmd/graywolf/main_android.go`.
- Stages:
  1. NDK + Java + Gradle setup.
  2. `make android-modem android-graywolf` (build only).
  3. `cd android && ./gradlew assembleDebug` (build only).
  4. Run Go unit tests with `-tags android` for the new packages.
- No emulator. Fast (target under 10 min).

Optional nightly `android-instrumented.yml` (separate, not gating)
boots an emulator and runs androidTest assertions.

### 4.7 Docs

| Path | Change |
|---|---|
| `docs/handbook/android.html` | new operator setup page: install from Play, OTG cable, Digirig/AIOC pairing, perm prompts, troubleshooting |
| `docs/handbook/installation.html` | new "Android" section linking to the above |
| `docs/handbook/ptt.html` | new section listing the four Android PTT methods |
| `docs/handbook/audio.html` | new section listing Android audio paths |
| `docs/wiki/system-topology.md` | new "Android" section with the two-process diagram (Service + modem cdylib in-process, Go child), the JNI audio bridge, the two UDS paths, lifecycle notes |
| `docs/wiki/build-pipelines.md` | new row for Android target, new row for the smoke workflow |
| `docs/wiki/code-map.md` | new column or sub-table for Android-specific files |
| `docs/wiki/invariants.md` | new entries (see section 7 below) |

## 5. Data flow

### 5.1 Cold start

1. User taps app icon. `MainActivity.onCreate`.
2. `MainActivity` requests POST_NOTIFICATIONS perm (API 33+).
3. `MainActivity` starts `GraywolfService` via `startForegroundService`.
4. Service immediately calls `startForeground(notification, types)` with
   `connectedDevice | microphone | location` types.
5. Service binds the platform-services UDS at
   `${cacheDir}/platform.sock`.
6. Service calls `System.loadLibrary("graywolfmodem")`. The cdylib's
   global JNI init populates `ndk_context::android_context()` from the
   `JavaVM` Android passes to `JNI_OnLoad`.
7. Service calls JNI `modemStart(modem_socket_path)` on a worker
   thread. The Rust side spawns the modem runtime and binds the
   Go<->Rust UDS server at `${cacheDir}/graywolf-modem.sock`.
8. Service calls JNI `modemAwaitReady(10_000)` (10 s timeout). Rust
   returns once the UDS server is accepting connections; analogous to
   the desktop binary's `\n`-on-stdout readiness signal (invariant 13).
9. Service starts the `AudioPump` Kotlin thread once `RECORD_AUDIO`
   is granted. Pump opens `AudioRecord(AudioSource.MIC, 22050, MONO,
   PCM16, …)` and begins forwarding samples via JNI `modemPushSamples`.
10. Service execs `libgraywolf.so` (Go child) with environment:
    - `GRAYWOLF_DB=${filesDir}/graywolf.db`
    - `GRAYWOLF_HISTORY_DB=${filesDir}/graywolf-history.db`
    - `GRAYWOLF_LOG_DB=${filesDir}/graywolf-logs.db`
    - `GRAYWOLF_TILE_CACHE=${filesDir}/tiles`
    - `GRAYWOLF_MODEM_SOCKET=${cacheDir}/graywolf-modem.sock`
    - `GRAYWOLF_PLATFORM_SOCKET=${cacheDir}/platform.sock`
    - `GRAYWOLF_LISTEN=127.0.0.1:8080`
    - `GRAYWOLF_LISTEN_TOKEN=<32-byte hex>` (per-launch bearer, N7)
    - `GRAYWOLF_PLATFORM=android`
11. Go binary connects to modem UDS (existing modembridge code,
    unchanged). Connects to platform-services UDS (new
    `pkg/platformsvc`).
12. Go binary writes a single newline `\n` to stdout once it has bound
    the HTTP listener and connected both UDS clients. The Service
    blocks until that byte arrives or a 10 s timeout.
13. Service injects the bearer token into the WebView via
    `addJavascriptInterface`, then loads `http://127.0.0.1:8080/`.
    Existing SPA renders; every fetch carries
    `Authorization: Bearer <token>` (N7).

### 5.2 TX with USB-serial PTT (Digirig path)

1. Web UI clicks "Send beacon". POSTs to Go REST.
2. Go txgovernor admits the frame, hands to modembridge.
3. Rust modem prepares the audio frame, then emits
   `KeyPttRequest{method=ANDROID_USB_SERIAL}` on the Go<->Rust UDS.
4. Go modembridge receives the message, forwards to platform-services
   UDS as the same proto type re-wrapped in `PlatformMessage`.
5. Kotlin `UsbAdapter` looks up the retained `UsbSerialPort` for the
   selected device, calls `setRTS(true)`.
6. Kotlin sends `PttAck{success=true}` back. Go relays the ack to the
   modem on the Go<->Rust UDS.
7. Modem starts audio playback. **TX audio path on Android is TBD as
   of POC-A** — output is not exercised yet. Most likely shape: Rust
   modulates, hands a PCM buffer to Kotlin via a JNI callback,
   Kotlin writes through `AudioTrack` to the same default device.
   POC-B (§11.2) is the right place to lock this in. AAudio TX may
   work where AAudio RX did not, but the safe default is symmetry
   with RX (AudioRecord/AudioTrack pair).
8. While TX is active the AudioPump pauses RX capture so the modem's
   own output doesn't echo back through the default mic (N6).
9. Audio drains. Modem emits `UnkeyPttRequest`. Symmetric path.
10. Kotlin clears RTS. ACKs. AudioPump resumes RX capture.

### 5.3 RX

1. Kotlin `AudioPump` thread reads ~2048-sample PCM16 chunks from
   `AudioRecord` (default audio device — whichever the OS has routed
   to: USB DAC, 3.5mm jack, or built-in mic). N6 governs route changes.
2. Each chunk is handed to Rust via JNI `modemPushSamples`. Rust
   applies the operator-set software gain (N9), enqueues into the
   demod input ring, returns immediately.
3. Modem worker thread demodulates, emits `RxFrame` on the Go<->Rust
   UDS (existing message type, unchanged).
4. Go ingress fanout marks `Source: Modem`, distributes to APRS parser,
   KISS server fan-out, AGW server fan-out, igate, etc. (Invariant 17
   preserved.)

### 5.4 GPS

1. `GpsAdapter` registers a `LocationListener` once `ACCESS_FINE_LOCATION`
   is granted (requested at first need).
2. Each fix -> `GpsFix{lat, lon, alt, speed_mps, course_deg, time_unix,
   hdop, num_sats, source}` proto.
3. Kotlin sends on platform-services UDS.
4. Go `pkg/gps/android.go` reads from `pkg/platformsvc`, converts to
   the existing `gps.Fix` struct, hands to existing consumers (beacons,
   livemap, position-history DB).

### 5.5 USB device picker

1. Web UI Settings -> "PTT" tab -> "Choose device".
2. Frontend GETs `/api/ptt/devices?method=ANDROID_USB_SERIAL`.
3. Go REST handler asks `pkg/pttdevice` for the device list.
4. `pkg/pttdevice/android.go` sends `UsbDeviceListRequest{class=serial}`
   on platform-services UDS.
5. Kotlin `UsbAdapter` enumerates `UsbManager.deviceList`, filters by
   class, returns `UsbDeviceListResponse` with `[{vendor_id, product_id,
   device_name, has_permission}]`.
6. Go returns to frontend. UI renders list.
7. User clicks one. Frontend POSTs `/api/ptt/select`.
8. Go REST -> `pkg/pttdevice` -> `UsbSelectRequest` to Kotlin.
9. Kotlin calls `UsbManager.requestPermission()`. System dialog appears
   over the WebView. User taps "OK".
10. Kotlin opens `UsbDeviceConnection`, wraps in `UsbSerialPort`,
    retains in adapter map, returns `UsbSelectResponse{ok=true,
    handle_id="..."}`.
11. Subsequent PTT requests reference this device.

### 5.6 Suspend, screen-off, app-switch

- Foreground service prevents the OS from killing the process while
  graywolf is "running" per the user's notification.
- Wi-Fi sleep policy bumped to `WIFI_MODE_FULL_HIGH_PERF` for the
  service lifetime to keep iGate sessions alive on screen-off.
- App requests battery-optimization whitelist via the standard intent
  on first launch. If the user denies, log a warning and proceed; some
  data may sleep when the screen is off.
- Notification has a "Stop" action. Tapping it broadcasts to the
  Service, which sends SIGTERM to both children, waits 5s, sends
  SIGKILL on remainder, calls `stopSelf()`.

## 6. Error handling

| Failure | Detection | Response |
|---|---|---|
| Modem panic | uncaught Rust panic crosses the JNI boundary; `JNI_OnLoad` installs a panic hook that converts to a Java exception | Service catches it, logs to `graywolf-logs.db` via Go relay, calls `modemStop`, then `loadLibrary` is already done so it just calls `modemStart` again with backoff (1s, 2s, 5s, 10s, capped). 3 crashes in 60s → halt restarts, surface via WebView WS event channel. (Modem is **in-process**; the Service does not survive a libc-level abort, so genuine SIGABRT takes the whole APK process down — Android then restarts the foreground service per `START_STICKY`.) |
| Go child crash | Service `Process.waitFor()` returns non-zero | Service stops the modem (`modemStop`), restarts both, same backoff policy. Restarting the Go child without restarting the modem would leave the modem's UDS server bound but unconsumed; cleaner to bounce both. |
| Platform UDS disconnect (Kotlin server died — implausible since it's in the same process — or Go client got EOF mid-read) | `pkg/platformsvc` read error | retry with backoff; modem PTT method marked offline by `pkg/pttdevice` so txgovernor refuses TX; GPS source goes idle, beacons fall back to manual fix |
| USB device unplugged mid-QSO | `ACTION_USB_DEVICE_DETACHED` broadcast in `UsbAdapter` | send `UsbDetach{handle_id}` event; `pkg/pttdevice` marks the active method offline; in-flight TX gets `PttAck{success=false, error="device gone"}`; txgovernor logs and aborts the frame |
| Audio route change mid-QSO (USB DAC unplugged, BT headset attached, jack pulled, default device flipped) | `AudioDeviceCallback.onAudioDevicesRemoved` / `onAudioDevicesAdded` in `AudioAdapter`, plus an `AudioRecord` read returning `ERROR_DEAD_OBJECT` | Kotlin pushes `AudioRouteChanged` proto; Service stops the AudioPump, opens a fresh `AudioRecord` against the new default device, restarts the pump. Modem aborts in-flight TX with `PttAck{success=false, error="audio route changed"}` and resumes RX once samples flow again. UI surfaces a single non-modal toast. (Invariant N6.) |
| RECORD_AUDIO denied | runtime perm flow returns DENY | modem reports "no audio device" via existing config error path; UI shows recoverable error with "grant permission" CTA |
| ACCESS_FINE_LOCATION denied | runtime perm flow returns DENY | GPS source unavailable; manual position-config UI still functional |
| POST_NOTIFICATIONS denied (API 33+) | runtime perm flow returns DENY | Service still runs (foreground services do not require this permission to run, only to show their notification). UI shows a degraded-experience banner |
| Doze / App Standby bucket throttling on screen-off > ~1h | iGate session goes silent, beacons miss schedule | Service holds a `PARTIAL_WAKE_LOCK` for the duration of an active TX or pending igate post; outside those windows the OS may sleep CPU. Operator is prompted on first launch to whitelist graywolf from battery optimization (standard `ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS` intent); declining is logged but accepted. |
| Token-auth failure on `127.0.0.1:8080` from non-WebView client | Go REST middleware returns 401 | event logged with peer fd info (where available) so abuse from another local app is visible in flare diagnostics. (Invariant N7.) |
| Schema-version mismatch on Hello | `pkg/platformsvc` rejects | Service logs error, terminates child binaries, restarts (likely a half-installed APK upgrade scenario) |
| `cargo-ndk` toolchain missing on dev machine | `make android` early failure | clear error message in Makefile target with link to setup docs |

The supervisor logic in `GraywolfService` mirrors the existing systemd
restart semantics for the desktop service, just expressed in Kotlin.

## 7. New invariants (added to `docs/wiki/invariants.md`)

These join the existing list. They are not yet in the wiki; this spec
specifies the additions for the implementation plan to write.

- **N1.** The Go child binary ships as a `lib*.so` file in
  `nativeLibraryDir`. Manifest must keep `useLegacyPackaging=true` and
  `extractNativeLibs="true"` or the Go binary cannot be exec'd.
  *Why:* Android only extracts files matching `lib*.so` to disk at
  install time, and Android only allows exec of files extracted to
  disk. Naming the Go ELF `libgraywolf.so` is a packaging trick to
  satisfy that filter; nothing actually `dlopen`s it. The Rust modem
  is a genuine cdylib (not exec'd) and is not subject to this rule.
- **N2.** Two UDS sockets plus one HTTP listener under the Service.
  Renaming or relocating any of `graywolf-modem.sock`, `platform.sock`,
  or moving the HTTP listen off `127.0.0.1:8080` requires updates in
  Kotlin (Service env injection + WebView URL), Go (env parsing +
  listener), Rust modem (UDS server bind path), and the manifest
  (`usesCleartextTraffic=true` for localhost). *Why:* The Service,
  the Rust modem (in-process to the Service), and the Go child
  process all assume those identities at startup. Note: Rust modem
  ↔ Kotlin happens via JNI in-process, no socket — see N8.
- **N3.** USB-class hardware (serial, HID, CM108, BT SPP) is owned by
  Kotlin on Android. Rust modem PTT drivers for these methods do not
  touch hardware - they emit proto events only. Any new Android PTT
  method follows this split. *Why:* Android does not expose
  `/dev/ttyUSB*` or HID-write paths to userspace; only Kotlin can hold
  the `UsbDeviceConnection`.
- **N4.** `pkg/platformproto/` is the single Go<->Kotlin contract.
  Schema bump + Hello version bump are coordinated. CI drift guard
  parallels invariant 11.
- **N5.** APK `versionName` matches `VERSION`; `versionCode` is derived
  numerically as `major * 1_000_000 + minor * 1_000 + patch`. Both
  rewritten by `make bump-*`. The formula's hard ceiling is Play
  Store's `versionCode` limit of 2,100,000,000, which caps usable major
  at 2099 — recorded here so a far-future bump cannot silently truncate.
  *Why:* Invariant 14's version-equality check between Go and Rust
  extends to the APK so Play-installed users cannot run mismatched
  halves.

- **N6.** Audio routing changes mid-QSO (USB DAC unplug, BT headset
  connect, jack pulled) abort the in-flight TX frame and reopen the
  CPAL stream against the new default device. RX resumes on the new
  device once the stream is back. txgovernor surfaces the abort to the
  ingress pipeline as a normal failed-frame event. *Why:* Android
  re-routes audio without notice; silently continuing on a stale stream
  produces dead air or wrong-device capture.

- **N7.** The Go HTTP listener binds `127.0.0.1:8080`, not the abstract
  Linux namespace, but every endpoint requires a per-launch bearer
  token (32-byte random, regenerated each cold start) injected into the
  WebView via `WebView.addJavascriptInterface` at startup. Other apps
  on the device can reach `127.0.0.1:8080` but cannot mint a valid
  token. *Why:* Loopback is shared across the device's app sandboxes;
  unauthenticated REST would let any installed app trigger TX, read
  position history, or drain the log buffer.

- **N8.** On Android the Rust modem ships as a `cdylib`
  (`libgraywolfmodem.so`) loaded into the Kotlin Service via
  `System.loadLibrary`, **not** as an exec'd ELF child. Audio capture
  uses Java `AudioRecord` (Kotlin-owned) with samples passed to Rust
  via JNI on each chunk. CPAL Oboe and direct AAudio access are not
  used: POC-A (PR #87) confirmed that AAudio rail-pins USB-Audio class
  capture at full scale and that cpal needs an `ndk_context` populated
  by an APK JNI runtime, which a `/data/local/tmp` exec lacks. *Why:*
  Reverting to either path resurrects POC-A's bring-up failures.

- **N9.** USB audio gain on Android is software-only. The audio HAL
  claims USB-Audio class devices on stream open and refuses
  user-space FU_VOLUME control transfers thereafter. The operator-
  facing gain slider applies a software multiplier inside Rust before
  the demod input ring (`-30 dB` to `+20 dB`, persisted in
  SharedPreferences and surfaced to Rust via JNI `modemSetGainDb`).
  *Why:* POC-A burned several hours trying to drive FU_VOLUME and
  the result was unrecoverable; this invariant locks in the lesson.

## 8. Testing strategy

### 8.1 Cross-compile smoke (per-PR, gating)

`android-build-smoke.yml`:
- `make android-modem android-graywolf` - both arches build clean.
- `cd android && ./gradlew assembleDebug` - Kotlin compiles, manifest
  validates, jniLibs path right.
- `go test -tags android ./pkg/platformsvc/... ./pkg/platformproto/...
  ./pkg/gps/... ./pkg/pttdevice/...` - in-process unit tests with a
  fake Kotlin server.

Goal: no Android regressions slip into main from non-Android PRs.

### 8.2 Go unit tests

`pkg/platformsvc/`: round-trip every proto message type through a
loopback fake server. Reconnect logic. Hello handshake. Schema mismatch
abort.

`pkg/gps/android_test.go`: feed a synthetic `GpsFix` stream from a
fake platformsvc, assert the Fix pipeline sees expected outputs.

`pkg/pttdevice/android_test.go`: enumerate against a fake
`UsbDeviceListResponse`, assert filtered method/device pairings.

### 8.3 Kotlin unit tests (`android/app/src/test/`)

`PlatformServerTest`: framing, dispatch.
`BinaryLauncherTest`: readiness-byte wait, restart backoff (with mock
`Process`).
`UsbAdapterTest`: filter logic (hardware mocked).

### 8.4 Instrumented tests (`android/app/src/androidTest/`)

`BootSmokeTest`: launches the Service in an emulator, waits for the Go
binary to bind `:8080`, GETs `/api/version`, asserts the
`Go-Rust-APK` version triple matches.

`UdsRoundTripTest`: launches Service, sends a synthetic `GpsFix` from
test code, asserts it surfaces in the Go test endpoint
`/api/_internal/last-gps-fix` (test-only, build-tagged
`android,testintegration`).

Run nightly + on `android/**` changes. Not gating PR merge.

### 8.5 Hardware matrix (manual, pre-release)

Tracked in beta release notes:
- Pixel 8 + Digirig + Baofeng UV-5R
- Pixel 6a + AIOC + Yaesu FT-65R
- Samsung A54 + USB-C-to-3.5mm dongle + radio with VOX
- (Bluetooth) any phone + BT SPP serial adapter + radio

Each release blocks on a green pass for at least two of these. No
physical CI lab in v1.

## 9. Distribution and signing

- Single distribution channel: Google Play Store (paid).
- Signing: Play App Signing. Google holds the app signing key. The
  upload key is held in `secrets.ANDROID_UPLOAD_KEYSTORE_BASE64` plus
  `secrets.ANDROID_UPLOAD_KEY_PASSWORD` in GitHub Actions.
- F-Droid not pursued. Self-builders may build their own APK from
  source under GPLv2; that is acceptable. The unsigned APK is not
  hosted as an official artifact.
- Upgrade path: Play Store handles updates. The in-app
  `pkg/updatescheck` is build-tagged off on Android.

## 10. Permissions list

Manifest declarations:

| Permission | Why | Runtime prompt? |
|---|---|---|
| `INTERNET` | iGate, online tile fetch, REST | no (normal) |
| `ACCESS_NETWORK_STATE` | Wi-Fi vs cell awareness for igate retry policy | no |
| `RECORD_AUDIO` | RX from default audio device | yes (on first RX enable) |
| `ACCESS_FINE_LOCATION` | GPS for beacons, livemap | yes (on first GPS enable) |
| `POST_NOTIFICATIONS` (API 33+) | foreground service notification | yes (on first launch) |
| `BLUETOOTH_CONNECT` (API 31+) | BT SPP PTT method | yes (only when method selected) |
| `FOREGROUND_SERVICE` | base | no |
| `FOREGROUND_SERVICE_CONNECTED_DEVICE` (API 34+) | USB devices | no |
| `FOREGROUND_SERVICE_MICROPHONE` (API 34+) | audio capture | no |
| `FOREGROUND_SERVICE_LOCATION` (API 34+) | GPS in background | no |
| `WAKE_LOCK` | Wi-Fi keep-alive | no |

Per-USB-device permission is granted via the system dialog that
`UsbManager.requestPermission()` invokes; not declared in manifest.

## 11. Effort and phasing

Six to ten weeks focused. Suggested phase split for the implementation
plan:

| Phase | Status | Duration | Deliverable |
|---|---|---|---|
| 1 (POC-A) | **complete** (PR #87) | actual: ~3 days | Cross-compile graywolf-modem to arm64. NativeActivity APK shell. Java AudioRecord + JNI sample hand-off. Decoded 9 live off-air frames in 50 s on UV5R + Digirig + Topicon T865 tablet. Run report at `.context/2026-05-07-android-poc-a-results.md`. |
| 1b (POC-B-revised) | next | 1 wk | See §11.2. End-to-end production topology proof: foreground Service + cdylib modem + AudioPump + Go child + WebView + bearer token, on a single APK, decoding live frames and rendering in the WebView. |
| 2 | pending | 1 wk | `proto/platform.proto`, `pkg/platformproto/`, `pkg/platformsvc/`, Kotlin `PlatformServer` skeleton. Round-trip Hello + GpsFix in unit tests. |
| 3 | pending | 1 wk | `GraywolfService` hardening + `MainActivity` + `GoLauncher` + production-grade WebView wiring. End-to-end "tap icon, see graywolf SPA running on the phone" with no PTT or GPS yet. (Phase 3 is now smaller because POC-B-revised already proved the integration shape.) |
| 4 | pending | 1 wk | `GpsAdapter` + `pkg/gps/android.go`. First useful end-to-end feature: phone position beacons over RF. |
| 5 | pending | 1 wk | USB-serial PTT (Digirig path) + VOX (`NONE`). Hardware matrix coverage for those two paths. **TX audio path locked in here** (likely Kotlin `AudioTrack` symmetric to AudioPump RX). |
| 5b | pending | 1 wk | USB-HID PTT (CM108 / AIOC path). Hardware matrix coverage. |
| 5c | pending (may slip to v1.1) | 1 wk | Bluetooth SPP PTT. Pairing UX, RFCOMM lifecycle, PTT timing measurement. |
| 6 | pending | 1 wk | `make android` target, `release-android.yml`, signing, Play Console upload to internal track. |
| 7 | pending | 1-2 wk | Polish, handbook page, wiki updates, beta cycle, Play Store first-submission review. |

### 11.2 POC-B-revised — end-to-end production topology

POC-A proved the audio + DSP path. POC-B-revised proves the rest of
the §3 architecture works end-to-end on a single APK before the
plan-driven build-out commits to it.

**Scope (single APK, single phase):**

- Kotlin foreground Service.
- `System.loadLibrary("graywolfmodem")` for the Rust cdylib.
- Kotlin AudioPump (lifted from POC-A's NativeActivity logic, moved
  into a Service-owned thread).
- JNI `modemStart` / `modemAwaitReady` / `modemPushSamples` /
  `modemSetGainDb` / `modemStop` entry points.
- Service execs a stub Go binary as a child process. Stub Go opens
  the Go↔Rust UDS, accepts one connection from the Rust modem,
  receives a stream of `RxFrame` protos, exposes a tiny REST
  endpoint at `127.0.0.1:8080/api/_internal/last-frame` returning the
  most recent decoded frame as JSON, gated by a per-launch bearer
  token (N7).
- Service holds a WebView. WebView is a single hand-written HTML/JS
  page (not the production SPA — that comes in phase 3) that polls
  `last-frame` every second and shows it in a `<pre>`.
- Operator-facing software gain slider stored in `SharedPreferences`,
  pushed to Rust via `modemSetGainDb` (N9).

**Success criterion:**

A live off-air APRS frame, captured by AudioRecord, JNI-pushed to
Rust, decoded by `MultiAfskDemodulator`, sent over UDS to the Go
child, returned over HTTP to the WebView, rendered in the `<pre>`,
within ~2 s of the audio crossing the demod threshold. Demonstrated
on the same Topicon T865 + Digirig + UV5R chain POC-A used.

**Out of scope (deferred to later phases):**

- TX of any kind. RX-only POC.
- GPS, USB device picker, BT, hardware-PTT methods.
- The real graywolf SPA. Phase 3 swaps in `go:embed all:dist`.
- Hot-reload of the cdylib, supervisor backoff for the Go child.
- The real `pkg/platformsvc` / `proto/platform.proto` (phase 2).

**Stop conditions** — escalate, don't iterate, if any of:

- The Service can't `loadLibrary` the modem cdylib (manifest /
  packaging fight bigger than expected).
- JNI panics or aborts when the AudioPump pushes its first chunk
  (likely an `ndk_context` bring-up issue in the cdylib path —
  POC-A's NativeActivity populated context "for free"; the Service
  may need an explicit `JNI_OnLoad` hook).
- Go-as-exec'd-child can't bind a UDS in `${cacheDir}` (SELinux
  domain restriction); fallback is to keep Go in-process via
  `gomobile`, which is a much bigger design pivot.
- WebView can't reach `127.0.0.1:8080` (manifest cleartext gate not
  set, or Android 14 cleartext rules tighter than expected).

**Why this POC and not just rolling into phase 2/3:** every item in
the list above is a single-binary cross-component integration risk.
Hitting them inside a multi-week phase produces tangled rework;
hitting them in a one-week scope-locked POC produces clean lessons
that shape the phase plan.

### 11.1 Battery / power baseline

Before beta, measure on a Pixel 8 or equivalent reference device:
- **Idle RX** (squelched, screen off, USB DAC attached): target < 200 mA
  average, equivalent to ~5% battery/hr on a 4500 mAh phone.
- **Active igate** (continuous decode, occasional Wi-Fi POST): target
  < 350 mA average.
- **TX 30s burst**: incremental cost recorded, not gated.

Numbers above are targets, not guarantees. They become the baseline
against which beta feedback is judged ("graywolf killed my battery"
needs a number to compare to). Recorded in handbook.

## 12. Risks tracked

1. **~~CPAL Oboe backend maturity.~~** **Retired by POC-A.** Neither
   CPAL Oboe nor direct AAudio is on the production path; audio
   capture is Kotlin `AudioRecord` with JNI sample hand-off (N8). The
   risk that replaced it is below.

1'. **`AudioRecord` HAL behavior varies across vendors.** POC-A
   validated one phone (Topicon T865, API 34) with one DAC (Digirig).
   Per-device gain shaping, default sample-rate selection, and route-
   change semantics differ across vendors and Android versions.
   Mitigation: hardware matrix in §8.5; per-device gain defaults
   surfaced through SharedPreferences; route-change handling
   (N6) tested explicitly by physically swapping audio devices during
   QSO as part of phase 5.
2. **Android exec-from-`nativeLibraryDir` policy drift.** Google has
   tightened this several times. Set `useLegacyPackaging=true` and
   `extractNativeLibs="true"` explicitly. Track Android 16+ release
   notes during impl.
3. **Play Store first-submission review.** Apps that capture audio,
   hold USB host fds, and transmit RF receive manual review. Submit
   with a thorough Data Safety form and a privacy-policy URL.
4. **PTT relay latency.** Rust -> Go -> Kotlin -> RTS toggle. Each UDS
   hop is single-digit microseconds. Total budget under 5ms typical.
   Measured in phase 5 before claiming TX timing is acceptable for
   1200-baud AFSK.
5. **GPLv2 + paid Play Store leakage.** Anyone with NDK can self-build.
   Accepted. Decision is business, not technical.
6. **Doze and App Standby buckets.** Android 12+ throttles wakelocks
   even for foreground services after long screen-off intervals.
   iGate sessions on a phone left charging overnight may go silent.
   Mitigation: short `PARTIAL_WAKE_LOCK` around active TX and pending
   igate posts only; battery-optimization whitelist requested at first
   launch (operator may decline). Validated by overnight beta on at
   least one charging and one un-charging device before v1.
7. **Localhost surface.** `127.0.0.1:8080` is reachable by any other
   app on the device. Per-launch bearer token (N7) prevents abuse but
   the threat is non-zero (e.g., a malicious app that scrapes the
   WebView's `Authorization` header through a screen-record exploit).
   Mitigation: token rotation per cold start, log unauthenticated
   requests in flare diagnostics. Re-evaluate before public listing.

## 13. Out of scope, recorded for later

- iOS port. Different sandbox; revisit only after Android v1 ships.
- Native Compose UI replacing the WebView. Possible but would fork the
  UX surface from desktop; not worth it until the WebView path proves
  inadequate.
- In-app purchase for "premium" features. Conflicts with GPLv2 (free
  features in source).
- Watch / Wear OS companion. No.
- Tasker / shortcut intents. Could be added later; not v1.
- Separate F-Droid build. User explicitly excluded.
- Offline maps tile *creation* on phone. The phone is a download client
  only; tiles are produced in `~/dev/graywolf-maps` per invariant 7.

---

## Appendix A: directory layout under graywolf root after impl

```
graywolf/
  android/                          # NEW
    app/
      build.gradle.kts
      src/main/
        AndroidManifest.xml
        jniLibs/
          arm64-v8a/
            libgraywolfmodem.so     # Rust cdylib (loaded via System.loadLibrary)
            libgraywolf.so          # Go ELF (exec'd as child; .so name is a packaging trick, see N1)
          x86_64/
            libgraywolfmodem.so
            libgraywolf.so
        kotlin/com/nw5w/graywolf/
          MainActivity.kt
          GraywolfService.kt
          audio/
            AudioPump.kt            # AudioRecord -> JNI sample hand-off
          jni/
            ModemBridge.kt          # Kotlin facade over Rust JNI surface
          binaries/
            GoLauncher.kt           # exec helpers for the Go child only
          platformsvc/
            PlatformServer.kt
            GpsAdapter.kt
            UsbAdapter.kt
            AudioAdapter.kt
            BatteryAdapter.kt
          webview/
            WebAppInterface.kt
            PermissionBridge.kt
        res/
        assets/
      src/test/kotlin/
      src/androidTest/kotlin/
    gradle/
    build.gradle.kts
    settings.gradle.kts
  proto/
    graywolf.proto                  # extended (PTT events, new method enum values)
    platform.proto                  # NEW
  pkg/
    platformproto/                  # NEW (generated)
    platformsvc/                    # NEW
    gps/
      android.go                    # NEW
      gpsd_unix.go                  # //go:build !android
      serial_unix.go                # //go:build !android
    pttdevice/
      android.go                    # NEW
      linux.go, windows.go, darwin.go  # build-tagged
    diagcollect/
      review/                       # build-tagged !android
      review_android.go             # NEW
    updatescheck/                   # build-tagged !android
  cmd/graywolf/
    main_unix.go                    # extracted
    main_windows.go                 # extracted
    main_android.go                 # NEW
  graywolf-modem/
    Cargo.toml                      # adds [lib] crate-type=["cdylib","rlib"], jni dep
    src/
      lib.rs                        # public run_rx API shared by desktop bin + Android cdylib
      android/
        mod.rs                      # JNI entry points (cfg target_os="android")
        audio.rs                    # consumer of Kotlin->Rust sample buffers (was poc-a-android/audio_record.rs)
      tx/
        ptt_android_usb_serial.rs   # NEW
        ptt_android_usb_hid.rs      # NEW
        ptt_android_bt_spp.rs       # NEW
  poc-a-android/                    # REMOVED after fold-in (was POC-A workspace member)
  Makefile                          # NEW targets: android-modem, android-graywolf, android
  .github/workflows/
    release-android.yml             # NEW
    android-build-smoke.yml         # NEW
  docs/handbook/
    android.html                    # NEW
  docs/wiki/                        # updated (system-topology, build-pipelines, code-map, invariants)
```
