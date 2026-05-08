# Android POC-B-revised — production topology end-to-end, single APK

**Status:** spec, ready for `/superpowers:writing-plans`
**Date:** 2026-05-07
**Repo:** all work lands in `~/dev/graywolf` on a feature branch off
`main`. No new repo, no fork, no submodule. All paths in this spec are
relative to the graywolf repo root.
**Scope:** prove the §3 architecture from
`.context/2026-05-01-android-app-design.md` (revised after POC-A)
works end-to-end on a single APK. One Kotlin foreground Service
hosting a Rust modem cdylib, an `AudioRecord`-driven JNI sample pump,
an exec'd Go child binary, a UDS between Rust and Go, a tiny Go REST
endpoint, and a WebView that renders decoded frames. **No production
SPA. No TX. No GPS. No PTT. No real `pkg/platformsvc`.** All those
arrive in subsequent phases; POC-B is the integration smoke that
proves they have a foundation to build on.

POC-A (PR #87) proved the Rust DSP works on Android via a
NativeActivity APK. POC-B proves the production topology works by
swapping the NativeActivity scaffold for a real Service + Go child
+ WebView while keeping the same audio path and decoder.

If any production-topology assumption from the revised design
(`§3.1`, `§3.2`, `§5.1`, invariants N1, N2, N7, N8, N9) doesn't
survive contact with a real device, **stop and revise the design
again before kicking off phases 2-7.**

---

## 1. Success criteria

POC-B is **green** when all six hold on the reference tablet:

1. Single APK installs from `adb install` and launches with one tap on
   its home-screen icon. No NativeActivity. No `adb shell am start`
   required after install.
2. Tapping the icon brings up `MainActivity`, which starts
   `GraywolfService` as a foreground service with a visible
   notification carrying `microphone` + `connectedDevice` types.
3. The Service successfully:
   - calls `System.loadLibrary("graywolfmodem")` against the cdylib
     packaged in `jniLibs/arm64-v8a/`,
   - boots the modem via JNI `modemStart`, blocks on
     `modemAwaitReady` (≤ 10 s),
   - opens an `AudioRecord` capture session against the default
     audio device and starts the `AudioPump` thread pushing samples
     via JNI `modemPushSamples`,
   - execs the Go child binary from `nativeLibraryDir`
     (`libgraywolf.so` packaging trick per N1) with the bearer-token
     env var (N7),
   - waits for the Go child's `\n`-on-stdout readiness signal,
   - injects the bearer token into the WebView and loads
     `http://127.0.0.1:8080/`.
4. With the radio + DAC chain from POC-A, the WebView renders **at
   least 5 distinct decoded AX.25 frames** within 10 minutes,
   updating in near-real-time (≤ 2 s end-to-end from audio crossing
   the demod threshold to text appearing in the WebView).
5. Killing the Go child manually (`adb shell kill <pid>`) results in
   the Service noticing within 5 s, restarting both modem and Go
   child with backoff, and the WebView resuming decode within 30 s
   without a manual reload.
6. Toggling the gain slider in the WebView (`-30 dB` to `+20 dB`)
   takes effect on the next sample chunk Rust processes (verifiable
   by adjusting until decode stops, then back until it resumes).

POC-B is **red** if any of:

- `loadLibrary` fails (manifest / packaging issue bigger than
  expected — almost certainly an `extractNativeLibs` /
  `useLegacyPackaging` interaction or an ABI mismatch).
- Service can't exec the Go child (SELinux domain restriction; this
  is the design-invalidating case — Go would have to move
  in-process via `gomobile`, which is a much bigger pivot than the
  current revised design contemplates).
- WebView can't reach `127.0.0.1:8080` (manifest cleartext gate
  missing or Android-version policy tighter than expected).
- Audio decode rate drops more than 50% vs. POC-A's NativeActivity
  baseline on the same hardware. (Indicates the Service-hosted
  AudioRecord path has a scheduling, priority, or thread-affinity
  regression vs. NativeActivity. Don't paper over with reactive
  tweaks; understand the cause before continuing.)
- Restart / supervisor logic deadlocks the Service or leaks
  AudioRecord handles across restart cycles.

POC-B is **yellow** (proceed with documented caveats) if:

- Decode works but only with non-default `AudioRecord` parameters
  (sample rate, buffer size) that POC-A didn't need. Lock the
  parameters in the run report.
- The first WebView load needs a manual reload to render frames
  (token injection race). Document the fix path; don't ship it.
- Go child supervisor backoff parameters need tuning. Acceptable;
  record what worked.

---

## 2. Out of scope (do not pad the POC)

- **TX of any kind.** RX-only. The TX audio path on Android is still
  TBD (see design §5.2); locking it in is phase 5 work, not POC-B.
- **GPS.** No `LocationManager`. No `pkg/gps/android.go`.
- **PTT.** No USB-serial, USB-HID, or BT SPP wiring. No
  `pkg/pttdevice/android.go`. No PTT relay path.
- **Real `proto/platform.proto` / `pkg/platformsvc/`.** Phase 2
  builds these. POC-B only needs the Go↔Rust UDS (which already
  exists for desktop and is unchanged on Android).
- **Real graywolf SPA.** A single hand-written HTML/JS page is
  enough. The `go:embed all:dist` switchover comes in phase 3.
- **USB device picker.** `UsbManager` enumeration is phase 5 work.
- **Battery whitelist / Doze handling.** Operator can leave the
  screen on for the test window.
- **Multiple architectures.** `arm64-v8a` only. x86_64 is a CI
  concern, not a POC-B concern.
- **Hot-reload of the cdylib.** `dlclose` semantics and library-
  reuse across restarts are deferred.
- **Frame persistence.** No DB. No history. The WebView shows the
  most recent N frames in memory; restart clears it.
- **Settings UI beyond the gain slider.** Hardcoded everything else.

If the executor finds themselves writing PTT code, opening a
`UsbManager` enumerator, generating proto schemas, or rewiring the
desktop SPA, they have left POC-B scope.

---

## 3. Reference hardware

Same kit as POC-A. Substitutions invalidate the apples-to-apples
audio comparison referenced in success criterion #4.

| Role | Choice |
|---|---|
| Tablet | Topicon T865, arm64-v8a, Android 14 / API 34 |
| USB cable | USB-C to USB-A OTG adapter from the Digirig kit |
| DAC | Digirig (CMedia CM108-class, vid=0x0d8c pid=0x0012) |
| Radio | Baofeng UV5R, tuned 144.390 MHz FM |
| ADB transport | adb-over-Wi-Fi recommended (USB-C port goes to the DAC) |

POC-A established baseline decode rate on this chain. POC-B's
success criterion #4 references that baseline; "same chain" matters.

---

## 4. Build setup

### 4.1 Toolchain

Same as POC-A plus a Go cross-compile path:

- Rust stable, current channel.
- `cargo-ndk` 4.x.
- Android NDK r27c at `$ANDROID_NDK_ROOT`.
- Android SDK build-tools 34.0.0, platform `android-34`.
- JDK 17 (Gradle requirement).
- Gradle wrapper checked into the repo at `android/gradlew`.
- `adb` from current platform-tools.
- Go 1.23+ with `GOOS=android GOARCH=arm64` cross-compile available
  (no extra toolchain — Go's standard cross-compile is enough since
  graywolf has `CGO_ENABLED=0` on the Go side).

### 4.2 New / changed code

Three coordinated tracks. Each lives at its production path so POC-B
output rolls into phase 2-7 without renames.

#### 4.2.1 Rust (`graywolf-modem/`)

- `Cargo.toml`: add
  ```toml
  [lib]
  crate-type = ["cdylib", "rlib"]
  ```
  Add `jni` and `android_logger` deps gated on
  `cfg(target_os="android")`.
- `src/lib.rs`: lift POC-A's RX entrypoint (currently in
  `poc-a-android/src/lib.rs` and a duplicate in `bin/poc_a_rxonly.rs`)
  into a public `pub fn run_rx_pipeline(…)` here. Both the desktop
  bin and the Android cdylib call it.
- New `src/android/mod.rs` (cfg-gated): JNI entry points
  `Java_…_modemStart`, `_modemAwaitReady`, `_modemPushSamples`,
  `_modemSetGainDb`, `_modemVersion`, `_modemStop`. Owns the
  Go↔Rust UDS server (existing IPC code reused) and the demod
  ingest queue. `JNI_OnLoad` populates `ndk_context` from the
  `JavaVM` so any deeper JNI users (none in POC-B, but the
  scaffolding lands here).
- New `src/android/audio.rs` (cfg-gated): consumer of
  Kotlin->Rust JNI sample buffers. Lift POC-A's
  `poc-a-android/src/audio_record.rs` here. Apply software gain
  (N9) before enqueueing.
- **Remove `poc-a-android/`** as a workspace member after the
  fold-in is verified (`cargo build --release` desktop side still
  green; cdylib builds clean for arm64).
- Keep `bin/poc_a_rxonly.rs` for desktop bring-up if useful, or
  remove if redundant. Executor's call.

#### 4.2.2 Kotlin (`android/app/`)

New Gradle project at the production path. Min SDK 28, target SDK
34. Kotlin only (no Java). The list below matches design §4.1 so
phase 3 doesn't have to rename anything POC-B creates.

```
android/app/src/main/
  AndroidManifest.xml
  jniLibs/arm64-v8a/
    libgraywolfmodem.so       # Rust cdylib, output of cargo-ndk
    libgraywolf.so            # Go ELF, output of `go build` (packaging trick per N1)
  kotlin/com/nw5w/graywolf/
    MainActivity.kt           # WebView, lifecycle, starts the Service
    GraywolfService.kt        # foreground Service, owns modem JNI + AudioPump + Go child supervisor
    audio/AudioPump.kt        # AudioRecord -> JNI sample hand-off
    jni/ModemBridge.kt        # Kotlin facade over JNI surface
    binaries/GoLauncher.kt    # exec helpers for the Go child
    webview/WebAppInterface.kt # bearer-token handoff + gain-slider state
```

POC-B does **not** create the full `platformsvc/` adapter set
(that's phase 2). The single `WebAppInterface.kt` JS bridge is
enough.

#### 4.2.3 Go (`cmd/graywolf-pocb/`)

Stub Go binary (separate from `cmd/graywolf/`) so POC-B doesn't have
to fight every desktop subsystem to start up on Android. Production
will use the real `cmd/graywolf` with `main_android.go`; that's
phase 3. POC-B's stub is throwaway.

```
cmd/graywolf-pocb/main.go     # POC-B only. Removed in phase 3.
```

The stub:

1. Reads env vars: `GRAYWOLF_MODEM_SOCKET`, `GRAYWOLF_LISTEN`,
   `GRAYWOLF_LISTEN_TOKEN`.
2. Connects to the Rust modem UDS using the existing modembridge
   client code (or a minimal subset thereof — the smallest amount
   of `pkg/modembridge` that compiles on Android).
3. Reads `RxFrame` proto messages from the UDS, keeps the most
   recent 50 in a ring.
4. Binds an HTTP listener on `GRAYWOLF_LISTEN`, exposes:
   - `GET /api/_internal/last-frames` → JSON array of recent
     decoded frames, gated by `Authorization: Bearer
     ${GRAYWOLF_LISTEN_TOKEN}`.
   - `GET /api/_internal/status` → JSON
     `{"frames_decoded": <int>, "uptime_seconds": <int>}`. Counter
     monotonically increments per `RxFrame` received from the modem
     UDS (no dedup, no filtering). `uptime_seconds` is wall-clock
     since the Go stub bound its listener. Same bearer-token gate.
   - `POST /api/_internal/gain` (body `{"db": -6.0}`), pushes the
     value to Rust over the existing modem-config message, gated by
     the same bearer token.
5. Writes a single `\n` to stdout once both the UDS connect and the
   HTTP bind have succeeded (matches design §5.1 step 12 and
   invariant 13).

The stub is plain Go, no Android-specific code. It must build with
`GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build`. Verify with a
Linux build first, then Android.

#### 4.2.4 WebView page

Single hand-written HTML file embedded in the Go stub:

```go
//go:embed pocb_index.html
var pocbIndex []byte
```

The page renders four things:

1. **Status row at top.** Two read-outs, refreshed every 1 s from
   `GET /api/_internal/status`:
   - **Frames decoded:** monotonic count since the Go stub started.
   - **Uptime:** elapsed time since the Go stub bound its listener,
     formatted `Hh Mm Ss`.
   These act as the "is anything happening?" signal during a 10-min
   capture without staring at the frame log. A static counter +
   non-incrementing uptime points at a Go-side hang; an
   incrementing uptime with a flatline frame counter points at
   silence on-air or an audio-path break.
2. **Gain slider.** `<input type="range" min="-30" max="20"
   step="1">` POSTing to `/api/_internal/gain` on change.
3. **Frame log.** `<pre>` polling `last-frames` every 1 s,
   newest-on-top.
4. **Bearer-token-gated `fetch`.** All three endpoints use
   `Authorization: Bearer <token>`; token comes from
   `WebAppInterface` at page load.

Stylistic minimum. No SPA framework. No build step. Plain HTML +
two short `<script>` blocks (one for status polling, one for
frames + gain). This page is thrown away in phase 3.

### 4.3 Build invocations

```sh
# Rust cdylib for Android
cargo ndk -t arm64-v8a -o android/app/src/main/jniLibs/ \
  build --lib --release \
  --manifest-path graywolf-modem/Cargo.toml
# rename target's libgraywolf_modem.so → libgraywolfmodem.so (or
# adjust [lib] name in Cargo.toml so cargo-ndk drops the right name)

# Go child for Android
GOOS=android GOARCH=arm64 CGO_ENABLED=0 \
  go build -o android/app/src/main/jniLibs/arm64-v8a/libgraywolf.so \
  ./cmd/graywolf-pocb

# APK
cd android && ./gradlew assembleDebug

# Install
adb install -r android/app/build/outputs/apk/debug/app-debug.apk
```

A `Makefile` target wrapping these is a nice-to-have for the POC
(`make poc-b-apk`) but not required if the plan would rather defer
Makefile entries to phase 6.

---

## 5. Run procedure

1. Tune radio to 144.390 MHz, confirm RX traffic via desktop
   graywolf-modem on the laptop (sanity check).
2. Connect tablet to laptop via Wi-Fi adb (`adb connect <ip>:5555`).
3. Plug DAC into the tablet's USB-C port. Confirm Android routed
   audio to the DAC: `adb shell dumpsys audio | grep -i 'usb\|routing'`.
4. `adb install -r app-debug.apk`.
5. Tap the icon on the tablet.
6. Watch the foreground notification appear with mic + connected-
   device types.
7. WebView renders the gain slider and an empty `<pre>`.
8. Within ~30 s, decoded frames begin appearing in the `<pre>`,
   newest on top.
9. Run for 10 minutes. Capture the WebView's frame list (screenshot
   + `adb shell dumpsys` window dump if needed). `adb logcat -s
   GraywolfService:* ModemBridge:* graywolf-pocb:*` running in
   parallel into `scratch/poc-b/run.log`.
10. Trigger the supervisor smoke test: `adb shell ps -A | grep
    graywolf-pocb`, get the PID, `adb shell kill $PID`. Watch
    logcat for the Service's restart sequence. Watch the WebView
    resume decode within 30 s.
11. Trigger the gain-change smoke: drag slider to `-30 dB`,
    decode should stop within ~5 s; back to `-6 dB`, decode
    resumes.
12. Stop the app from the foreground notification's "Stop"
    action (or from the system "Stop" button).

---

## 6. Deliverables

| Artifact | Location |
|---|---|
| Rust cdylib + JNI + audio bridge folded into modem crate | `graywolf-modem/{Cargo.toml,src/lib.rs,src/android/}` |
| `poc-a-android/` removed | (deletion) |
| Kotlin Service + AudioPump + ModemBridge + GoLauncher + WebView | `android/app/...` |
| Go stub binary | `cmd/graywolf-pocb/main.go` |
| Hand-written WebView page | `cmd/graywolf-pocb/pocb_index.html` |
| Run report | `.context/2026-05-XX-android-poc-b-results.md` (filled in after the run, force-added per repo convention) |

The run report captures:

- Toolchain versions (`rustc -V`, `cargo ndk -V`, NDK, JDK, Gradle,
  Go, Android SDK, tablet API level).
- Whether each of the six success criteria was met. If yellow, what
  caveat applies.
- Frame count over the 10-minute window. If significantly off
  POC-A's rate, hypothesis why.
- Logcat excerpts for any unexpected error / warning lines.
- Verdict: green / yellow / red.
- If yellow or red: specific blockers, proposed design-doc revisions
  before phase 2 starts.

---

## 7. Plan structure hint for `/superpowers:writing-plans`

POC-B is bigger than POC-A — three languages, four file trees,
one new build target. The writing-plans output should produce a
plan with roughly the following ordered tasks. The planner is
welcome to reshape; intent is what matters.

1. **Rust fold-in.** Move POC-A's audio + JNI code from
   `poc-a-android/` into `graywolf-modem/src/android/`. Add
   `cdylib` crate-type. Verify desktop graywolf-modem still
   builds and decodes locally. Verify `cargo ndk -t arm64-v8a
   build --lib --release` produces a working
   `libgraywolfmodem.so`. Delete `poc-a-android/`.
2. **Go stub binary.** Write `cmd/graywolf-pocb/main.go`. Verify
   it builds for `GOOS=linux` (use this to prove the modem-UDS
   client logic) and for `GOOS=android GOARCH=arm64`.
3. **Gradle Android project skeleton.** `android/app/` with min
   SDK 28, target SDK 34, manifest declarations matching design
   §10 minus the perms POC-B doesn't need (no
   `BLUETOOTH_CONNECT`, no `ACCESS_FINE_LOCATION` —
   `RECORD_AUDIO` and the foreground-service set are enough).
   `assembleDebug` produces an APK. Empty Service / Activity
   shells.
4. **JNI plumbing.** `ModemBridge.kt` declares external fns
   matching the Rust JNI entry points. `MainActivity` calls
   `loadLibrary`. Verify install + launch + `loadLibrary` works
   (no audio yet).
5. **AudioPump.** Lift POC-A's audio-capture loop into
   `audio/AudioPump.kt`. Wire to the Service lifecycle.
   `RECORD_AUDIO` runtime perm flow. End-to-end sample flow
   from `AudioRecord` → JNI → Rust demod → logcat (frames
   logged but no UI yet).
6. **Go child wiring.** `GoLauncher.kt` execs the Go stub from
   `nativeLibraryDir` with the env-injected paths + bearer
   token. Service waits for the readiness `\n` byte. Modem-UDS
   path between Rust (in-process) and Go (child) confirmed
   working.
7. **WebView + bearer token + frame rendering + status counters.**
   `MainActivity` owns the WebView, gets the token from
   `WebAppInterface` handoff, loads the Go stub's HTTP endpoint,
   renders frames in real time from `last-frames` polling, renders
   `frames_decoded` + `uptime_seconds` from `status` polling at
   the top of the page, gain slider POSTs to `gain`. Counters
   double as the runtime liveness signal during the 10-min
   capture.
8. **Supervisor smoke.** Service detects Go child crash, restarts
   with backoff. Service detects modem JNI panic (catch
   `RuntimeException` from JNI calls), restarts.
9. **Run + report.** 10-minute on-air capture with the manual
   smoke tests for criteria 5 and 6. Fill in
   `.context/2026-05-XX-android-poc-b-results.md`.

Tasks 1, 2, 3 are independent — parallelizable. Tasks 4-8 are
sequential, each unblocks the next. Task 9 depends on all prior.

A few traps the planner should call out explicitly:

- **`extractNativeLibs="true"` + `useLegacyPackaging=true` in the
  manifest** — without these, the Go ELF child won't be extracted
  and the Service can't exec it.
- **Rust cdylib name.** `cargo` produces `libgraywolf_modem.so`
  by default (underscore). `System.loadLibrary("graywolfmodem")`
  expects `libgraywolfmodem.so`. Fix via `[lib] name =
  "graywolfmodem"` in Cargo.toml or rename in the build script.
- **JNI symbol mangling.** The Java/Kotlin package path is
  baked into the JNI symbol name. If `ModemBridge.kt` lives at
  `com.nw5w.graywolf.jni`, the Rust JNI entry points must be
  named `Java_com_nw5w_graywolf_jni_ModemBridge_modemStart`,
  etc. Easy to get wrong; surface in the plan.
- **Bearer token propagation.** Service generates the token
  before exec'ing the Go child (env var) and before injecting
  into the WebView (JS bridge). Both must use the same token.
  Don't generate twice.
- **AudioRecord pause across TX.** N6 says the pump pauses
  during TX so the modem's own audio doesn't echo back. POC-B
  has no TX, so pause logic isn't needed yet — but the
  AudioPump's API should already support stop / start cleanly so
  phase 5 doesn't have to refactor it.

---

## 8. Stop conditions for the executor

Surface to the user, do not press through, if any of:

- **`loadLibrary` fails.** Almost always packaging — but if it's
  truly a cdylib symbol issue, escalate. Do not work around with
  `dlopen` from JNI manually.
- **JNI panic on first sample chunk.** This worked under
  NativeActivity in POC-A. Failure under a Service indicates an
  `ndk_context` initialization order issue. Fix it correctly in
  `JNI_OnLoad`; do not paper over with retries.
- **Go child can't bind a UDS in `${cacheDir}`.** SELinux domain
  restriction. This is the biggest design-invalidating risk in
  POC-B. If real, rescope the design to either move Go in-process
  via `gomobile` (large pivot) or use a TCP loopback for Go↔Rust
  (smaller but still a design change). **Stop and escalate.**
- **WebView can't reach `127.0.0.1:8080`.** Manifest cleartext
  policy or new Android-version localhost rules. Fix in manifest;
  if cleartext localhost is genuinely blocked on the target API
  level, design needs rework around an HTTPS loopback with a
  self-signed cert, or use the `WebViewAssetLoader` proxy.
- **Decode rate < 50% of POC-A's baseline.** Indicates a
  Service/AudioPump regression. Don't iterate on tuning; find the
  cause.

These cases push beyond design assumptions; pressing through
wastes time better spent rethinking the design.

---

## 9. Why this POC and not a multi-week phase

Every item in the success-criteria list above is a single-binary
cross-component integration risk. Hitting them inside a multi-week
phase produces tangled rework — was it the platformsvc proto, the
SPA wiring, the Service supervisor, the JNI bridge, or the Go child
that broke? Hitting them in a one-week scope-locked POC produces
clean lessons that shape the phase plan.

POC-A's success made several design assumptions defensible. POC-B
makes the rest of them defensible. After POC-B is green, the
remaining phases (§11 phases 2-7 of the revised design) are mostly
grunt work — well-scoped, well-isolated, plan-driveable.
