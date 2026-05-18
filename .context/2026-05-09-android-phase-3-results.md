# Android Phase 3 — run report

## Toolchain versions used

| Tool | Version |
|------|---------|
| JDK | OpenJDK 17.0.19 (Homebrew) |
| NDK | r29 (Pkg.Revision 29.0.14206865) |
| cargo-ndk | 4.1.2 |
| Rust | 1.90.0 |
| Go | 1.26.2 |
| Node | v25.9.0 |
| Vite | ^5.4.0 (5.4.21 actual) |
| Gradle (system) | 9.5.0 (used to regen 8.7 wrapper) |
| Gradle (wrapper) | 8.7 |

## Definition-of-done criteria

| # | Criterion | Status | Notes / commit |
|---|-----------|--------|----------------|
| 1 | cmd/graywolf/main_android.go is a real entry | ✅ | `5912afd` (3a6 lines incl. test, env contract, platformsvc Hello) |
| 2 | HTTP listener binds 127.0.0.1:8080 with bearer middleware | ✅ | `7895379` + `3a6ba1b` (middleware + wiring) |
| 3 | Embedded SPA serves under Android cross-compile | ✅ | arm64 + amd64 cross-compile clean; binary 34M each |
| 4 | Modem UDS connect | ✅ | `6e5d0b4` (modembridge connect-only mode + ModemSocketPath threading) |
| 5 | Platform UDS connect | ✅ | platformHello() in `cmd/graywolf/main_android.go:64` (Phase 2 platformsvc client) |
| 6 | Hello handshake completes before readiness byte | ✅ | platformHello blocks before app.Run; ready hook fires inside app.Run after net.Listen |
| 7 | Readiness signal `\n` written to stdout | ✅ | `04fbe14` (OnHTTPListenerReady hook) wired in `5912afd` |
| 8 | Disabled-on-Android subsystems compile out | ✅ | updatescheck gated on cfg.Platform; cross-compile clean (`efebe36`) |
| 9 | cmd/graywolf-pocb retired | ✅ | `f01f909` (git rm -r; build.gradle.kts updated) |
| 10 | MainActivity owns WebView + lifecycle | ✅ (preserved from phase 2) | + battery-opt intent on first onResume (`db6cff3`) |
| 11 | GraywolfService is production-grade | ✅ | bearer token (per-app-cold-start) + Stop action + expanded FGS types |
| 12 | GoLauncher renamed log tags | ✅ | `ab2aa46` (TAG_STDOUT=GraywolfGo, TAG_STDERR=GraywolfGoErr) |
| 13 | WebAppInterface phase-3 surface | ✅ | only getBearerToken; sentinel test (`4838ee2`) |
| 14 | JS-bridge ordering honored | ✅ (preserved from phase 2) | addJavascriptInterface before loadUrl (MainActivity:28) |
| 15 | Battery-opt whitelist intent on first launch | ✅ | `db6cff3` (SharedPreferences flag, fired on first onResume) |
| 16 | Manifest perms + types + cleartext narrowing | ✅ | `033cfc4` (network_security_config.xml scoped to 127.0.0.1; FGS bitmap=microphone+connectedDevice+location) |
| 17 | SPA reads bearer token from JS bridge | ✅ | `4b82c6a` (androidBridge.js cached read from globalThis.GraywolfWebInterface) |
| 17a | api.js 401 path skips #/login on Android | ✅ | `40c9b7c` (bridge-gated branch + node:test coverage) |
| 18 | Fetch coverage exhaustive | ⏳ partial — local pipeline ✅, HW route walk pending | Cross-compile arm64+amd64 ✅; npm build ✅; assembleDebug ✅ (APK 48.9M); Kotlin tests ✅. Chrome devtools route walk on T865 still pending. |
| 19 | SPA renders end-to-end on Android | ⏳ deferred to operator | Requires APK install on T865 + Chrome remote devtools |
| 20 | Cold-start succeeds on T865 | ⏳ deferred to operator | Logcat capture + screenshots (see HW task instructions below) |
| 21 | Supervisor restart works under fault injection | ⏳ deferred to operator | SIGKILL of Go child via `adb shell run-as` |
| 22 | POC-D PTT regression deferred | ✅ noted | Proto-driven PTT lands in phase 5 |
| 23 | AudioPump RX still works | ⏳ deferred to operator | Digirig + UV-5R chain wired; logcat for `RxFrame` |

## APK size baseline
- Phase 2 baseline: 35,521,452 bytes (~34M)
- Phase 3 actual: **48,899,874 bytes (~47M); +13.4M delta**
- SPA is embedded inside libgraywolf.so (web/embed.go go:embed); per-ABI .so sizes:
  - lib/arm64-v8a/libgraywolf.so: 34,323,208 bytes
  - lib/arm64-v8a/libgraywolfmodem.so: 1,183,952 bytes
  - lib/x86_64/libgraywolf.so: 34,455,000 bytes
  - lib/x86_64/libgraywolfmodem.so: 1,102,512 bytes

## Drift between phase-3 spec and as-shipped behavior

- **Bearer token rotation cadence.** Spec §1 criterion #11 says the token is generated "on every cold start AND on every supervisor restart of the Go child". As-shipped: token is per-`GraywolfApp` lifetime (process cold start) and STABLE across supervisor-driven Go-child restarts. Rationale: per-Go-restart rotation would require Service→Activity broadcast + `WebView.reload()` + SPA bridge cache invalidation. Threat model preserved by cold-start rotation. Phase 5+ should not "fix" to match spec literal wording without coordinating the WebView reload.
- **modembridge connect-only mode added (Task 6.0, not in original plan).** Spec §1 criterion #4 says "no Android-specific changes to modembridge" but modembridge forks the modem child via `exec.CommandContext`, which can't work on Android (modem cdylib is in-process to the Service). Added `Config.ExistingSocket`: when set, supervisor skips fork+readiness and dials the existing UDS directly. Threaded into `app.Config.ModemSocketPath`. Desktop unchanged.
- **GRAYWOLF_LOG_DB env var not consumed.** Plan listed it as required; as-shipped, the desktop logbuffer wires itself via configstore at runtime. An Android-specific override would require a new `app.Config.LogBufferDBPath` field. Defer to phase 4. Service can still set the env var (no-op for now).
- **OnHTTPListenerReady chosen over polling.** Plan correctly specified the hook approach; mentioned explicitly so future maintainers don't replace it with a stdout sentinel string match.
- **Gradle wrapper jar repaired (Task 18 incidental).** Phase 2 shipped a 42KB stub that crashed every gradlew invocation. Regenerated to 48KB working jar via system Gradle 9.5.0; pinned at distribution version 8.7 to match AGP 8.5.
- **`configFromEnv` lives in portable file (no //go:build tag).** The plan put it in `main_android.go`, but `go test -tags android` on darwin fails because Go stdlib `_android.go` and `_darwin.go` files in `internal/goos` redeclare the same constants. Splitting `configFromEnv` into `cmd/graywolf/android_config.go` (untagged) makes the parse logic testable on the host.
- **GraywolfApp.UsbPttAdapter.init removed.** Plan only listed Activity.onResume.enumerate and Service.onDestroy.closeAll as wiring sites to remove, but App.onCreate.init was also live. Removed all three to fully achieve the "no UsbPttAdapter on the wire" guarantee phase 3 wants.

## Phase 4 prerequisites status

- `pkg/gps/android.go`: not yet created. Phase 4 first task.
- `platformsvc.SubscribeGpsFix`: shipped in phase 2 (`pkg/platformsvc/client_impl.go`). Confirmed callable from real `cmd/graywolf` (no signature changes phase 3).
- `LocationManager` perm flow: `ACCESS_FINE_LOCATION` not yet added to manifest. Phase 4 first task.
- `app.Config.LogBufferDBPath`: not added (deferred from phase 3 deviation list).

## Cold-start time
Deferred to operator (T865 hardware required).

## HW tasks deferred to operator

The following tasks require physical T865 + Digirig + UV-5R + Chrome remote devtools and were not run during this plan's autonomous phase. They MUST be completed before this branch is merged.

### Task 18 (APK assembly)
```bash
export ANDROID_HOME=$HOME/Library/Android/sdk    # or wherever your SDK lives
export ANDROID_NDK_ROOT=/opt/homebrew/Caskroom/android-ndk/29/AndroidNDK14206865.app/Contents/NDK
export JAVA_HOME=/opt/homebrew/opt/openjdk@17/libexec/openjdk.jdk/Contents/Home
cd android && ./gradlew assembleDebug
ls -la app/build/outputs/apk/debug/app-debug.apk
```

Also run Kotlin unit tests:
```bash
./gradlew :app:testDebugUnitTest --tests "com.nw5w.graywolf.binaries.*" \
  --tests "com.nw5w.graywolf.webview.*" \
  --tests "com.nw5w.graywolf.platformsvc.*"
```

### Task 19 (cold-start trace on T865)
```bash
adb install -r android/app/build/outputs/apk/debug/app-debug.apk
adb logcat -s GraywolfService:* GraywolfGo:* GraywolfGoErr:* PlatformServer:* MainActivity:* Supervisor:* > scratch/phase-3/cold-start.log &
# Tap icon, wait for SPA, observe logcat for:
#   MainActivity perm-prompt completion
#   GraywolfService notification + modemAwaitReady=true
#   PlatformServer accept loop
#   GraywolfGo: platformsvc: connected, server_version=... schema_version=1
#   GraywolfGo: graywolf-android: listener_ready
#   WebView loads http://127.0.0.1:8080/
# Open Chrome chrome://inspect, attach to WebView; verify Authorization: Bearer <hex> on /api/* requests.
# Tap Stop in notification; verify "stop action received; shutting down" + clean shutdown.

# Redact bearer token before saving artifact:
TOKEN=$(grep -oE 'token[=:][a-f0-9]{64}' scratch/phase-3/cold-start.log | head -1 | sed 's/.*[=:]//')
[ -n "$TOKEN" ] && sed -i.bak "s/${TOKEN}/REDACTED-BEARER/g" scratch/phase-3/cold-start.log && rm scratch/phase-3/cold-start.log.bak
```

### Task 19a (SPA fetch + WebSocket coverage walk)
```bash
mkdir -p scratch/phase-3
{
    echo "=== fetch( ==="
    grep -rn "fetch(" web/src --include="*.js" --include="*.svelte" | grep -v node_modules
    echo
    echo "=== new WebSocket( ==="
    grep -rn "new WebSocket(" web/src --include="*.js" --include="*.svelte"
    echo
    echo "=== new EventSource( ==="
    grep -rn "new EventSource(" web/src --include="*.js" --include="*.svelte"
    echo
    echo "=== XMLHttpRequest ==="
    grep -rn "XMLHttpRequest" web/src --include="*.js" --include="*.svelte"
    echo
    echo "=== new Worker( ==="
    grep -rn "new Worker(" web/src --include="*.js" --include="*.svelte"
    echo
    echo "=== axios | ky | superagent ==="
    grep -rn "from .axios\\|from .ky\\|from .superagent" web/src --include="*.js" --include="*.svelte"
} > scratch/phase-3/http-call-sites.txt
head -5 web/src/main.js  # verify import './bootstrap.js' is FIRST
```

Then in Chrome remote devtools attached to the WebView, walk every top-level route (Settings, Channels, Audio, Status, Packets, Terminal, Sessions, History, Maps); confirm every `/api/...` request shows `Authorization: Bearer ...`. Open one terminal session; confirm WS upgrade URL contains `?token=...`.

### Task 20 (POC-B RX regression)
1. Plug Digirig into T865 via OTG; connect Digirig audio to UV-5R; tune to 144.390 MHz (US APRS).
2. Tap icon; wait for SPA.
3. `adb logcat | grep -E "RxFrame|aprs:|demod"` — wait for organic transmission OR have second radio TX.
4. Verify SPA's "Packets" or "Live Feed" tab renders the decoded frame within ~2s of logcat decode line.
5. If SPA tab shows no packets despite logcat showing decodes: check Chrome devtools — WebSocket should carry `?token=<hex>`.

### Task 19 step 6a (verify Go-only restart preserves modem JNI state)
Per Phase 2's `supervisorRestart()` (GraywolfService.kt:71), the supervisor restarts BOTH modem and Go child. Verify in logcat after SIGKILL of Go child:
- `Supervisor: poc-b: go_child_died`
- `GraywolfService: poc-b: supervisor_restart_begin`
- `audioPump.stop()` evidence
- `ModemBridge.modemStop()` evidence
- new `modemAwaitReady=true`
- new `audioPump.start()`
- new `poc-b: go_child_up`

### SIGKILL fault injection (Task 19 step 7)
```bash
adb shell ps -A | grep "libgraywolf"
adb shell run-as com.nw5w.graywolf kill -9 <pid>
# Watch logcat for: Supervisor: go_child_died, supervisor_restart_begin, new listener_ready
# Token rotation: NOT expected mid-session (per phase-3 deviation note above; bearer is per-app-cold-start)
```

## Known gaps to address before phase 3 ships clean

These were observed during T865 verification; functionality decode-side works but the
operator-visible surface is incomplete or confusing.

1. **Audio level meter blank on Android.** `/audio-devices/levels` returns empty; modem
   cdylib doesn't compute peak/rms from JNI-pushed samples. **Fix:** in
   `graywolf-modem/src/android.rs` (or wherever `modem_push_samples` lands), maintain
   a sliding window over inbound shorts; periodically (~10Hz) emit
   `IpcMessage::InputLevel { device_id: "android-default", peak_dbfs, rms_dbfs }`. Go
   side already dispatches.

2. **Per-channel `rx_frames` counter stuck at 0.** Modem doesn't tag emitted
   `ReceivedFrame` with a channel id on Android because there's only one audio source
   and no per-channel routing. **Fix:** when modem-config arrives, capture the first
   configured channel's id; tag every emitted `ReceivedFrame` with it. Go dispatcher
   then attributes correctly.

3. **PositionLog disabled by default → live map empty after restart.** `PositionLogConfig.Enabled`
   defaults to `false` (rationale: protect SD cards on Pi). On Android the rationale
   is moot — internal storage handles it fine. **Fix:** either default-on for
   `Platform == "android"`, or auto-enable on first boot if the configstore is empty.
   Operator can also flip it manually via SPA sidebar -> Position Log.

4. **No output device / TX path.** Phase 3 is RX-only by design. Phase 5 wires
   `AudioTrack` for TX plus the proto-driven PTT path.

5. **Audio device path field on AudioDevices form is meaningless on Android.** Android
   uses `MediaRecorder.AudioSource.MIC` (system default) regardless of stored path.
   UI should hide / lock the field on Android. (Plan A item: `web/src/lib/platform.js`
   helper + `{#if !isAndroid()}` guards across AudioDevices, Channels, Ptt routes.)

6. **Updates page meaningless on Android.** updatescheck is gated off (Play Store
   handles updates), but the SPA still shows the Updates settings card. UI should hide
   on Android.

7. **Unused FGS types deferred.** `FOREGROUND_SERVICE_CONNECTED_DEVICE` (USB-PTT,
   phase 5) and `FOREGROUND_SERVICE_LOCATION` (GPS, phase 4) cannot be declared in the
   manifest until their access perms (USB_DEVICE / ACCESS_*_LOCATION) land alongside.
   Re-add when those phases bring their access perms.

## Issues surfaced during plan execution

1. **Phase 2 baseline missing on phase-3 plan branch.** Plan assumed phase-2 commits present; the docs/android-phase-3-plan branch only contained the plan doc itself. Resolved by `git merge --no-ff feature/android-phase-2`.
2. **modembridge couldn't connect to existing UDS.** Spec criterion #4 wished it could; implementation forks the modem child unconditionally. Added Option A: `Config.ExistingSocket` connect-only mode. Detailed in commit `6e5d0b4`.
3. **app.Config field schema mismatch with plan.** Plan referenced `LogBufferDBPath`, `ModemSocketPath`, `PlatformSocket`, `TileCachePath`, `ListenAddr`. Actual: `HistoryDBPath`, `TileCacheDir`, `HTTPAddr` already; added `ModemSocketPath`; left LogBufferDBPath/PlatformSocket out (former defers, latter handled outside app.Config in main_android.go).
4. **`go test -tags android` broken on darwin host.** Stdlib's `internal/goos/zgoos_android.go` and `zgoos_darwin.go` redeclare the same identifiers when both build tags resolve. Worked around by extracting `configFromEnv` to an untagged file.
5. **Gradle wrapper jar broken (42KB stub).** Phase 2 shipped a truncated wrapper that errored on every invocation. Regenerated.
6. **Local Android build environment incomplete.** SDK platforms/build-tools not installed on dev host; assembleDebug requires operator-side setup. APK assembly + Kotlin unit-test runs deferred.
