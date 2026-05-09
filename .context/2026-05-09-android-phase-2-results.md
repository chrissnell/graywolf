# Android Phase 2 -- run report

## Toolchain versions used
- JDK: openjdk version "17.0.19" 2026-04-21
- NDK: 29.0.14206865
- cargo-ndk: cargo-ndk 4.1.2
- Rust: rustc 1.91.0 (f8297e351 2025-10-28)
- Go: go version go1.26.2 darwin/arm64

## Definition-of-done criteria

| # | Criterion | Status | Notes |
|---|-----------|--------|-------|
| 1 | proto/platform.proto exists, lints clean, generates Go + Kotlin | ✅ | 57d5a77 (Go bindings via `make proto`); Kotlin codegen wired in 05693cf |
| 2 | pkg/platformproto/ committed; make proto regenerates | ✅ | 57d5a77; drift guard locks it in 3439af8 |
| 3 | pkg/platformsvc/ Go API matches spec interface | ✅ | 3e9737c (interface + framing + Hello + req/resp + subscriptions); 19acabf (ConnectWithReconnect); c013c9a (Hello mismatch + server-error paths) |
| 4 | Kotlin PlatformServer skeleton + Hello/GpsFix only | ✅ | 0203fb6 (WireCodec + tests); acae01d (accept loop + dispatcher + Hello + GpsFix) |
| 5 | Round-trip unit tests pass on both sides | ✅ | `GOWORK=off go test ./pkg/platformsvc/... ./pkg/platformproto/...` clean; `./gradlew :app:testDebugUnitTest` clean |
| 6 | Drift guard catches divergence | ✅ | 3439af8 -- `pkg/platformproto/drift_guard_test.go` regenerates and diffs against committed bindings |
| 7 | jniLibs/ gitignored, .so files evicted | ✅ | 344498c -- removed committed .so + retired manual build script; `android/app/src/main/jniLibs/` ignored |
| 8 | Gradle externalNativeBuild produces both .so per ABI | ✅ | 0b3e35d (cargoNdkBuild for Rust modem); e2bbea5 (goCrossCompile for libgraywolf.so); APK contents confirm both `libgraywolf.so` + `libgraywolfmodem.so` per ABI |
| 9 | All three ABIs build clean | ❌ partial -- see Issues #1 | arm64-v8a ✅; x86_64 ✅; **armeabi-v7a dropped** in e6583dc (Go's android/arm requires cgo + NDK C toolchain, and minSdk=28 leaves no realistic armv7 devices). Rust still builds for all 3 ABIs in cargo-ndk; Go matrix + AGP `abiFilters` ship arm64-v8a + x86_64 only. |
| 10 | Toolchain check asserts everything | ✅ | 6b6cef7 (`scripts/check-android-toolchain`); 4834ee9 wires it into `preBuild` so every Gradle build gates on it |
| 11 | GOWORK=off automated | ✅ | e2bbea5 -- `goCrossCompile_*` tasks set `environment("GOWORK", "off")` on the Exec spec; matches `feedback_goembed_goworkoff_in_worktrees` memory |
| 12 | cmd/graywolf-pocb still builds | ✅ | 9e5b88e (pocb wired to dial PlatformServer on boot); cross-compile probe in `scratch/phase-2/android-build-probe.log` confirms `./...` builds clean |
| 13 | POC-D regression clean on T865 | ✅ | CP2102N RTS (Digirig) + CDC-ACM RTS (AIOC) key/unkey clean. CM108 HID writes the bit but does not key the radio -- matches POC-D Stage 2 finding (Digirig CM108 GPIO is not externally wired to the PTT line; chip handles audio only). TX test frame transmitted and decoded by reference station. Log: `scratch/phase-2/pocd-regression.log` (20519 lines, zero graywolf-level ERROR/FATAL/FAIL after filtering MTK kernel audio-stack noise). |
| 14 | Multi-ABI APK + handshake on T865 | ✅ | APK contents at `scratch/phase-2/apk-contents.txt` confirm `lib/arm64-v8a/libgraywolf.so`, `lib/arm64-v8a/libgraywolfmodem.so`, `lib/x86_64/libgraywolf.so`, `lib/x86_64/libgraywolfmodem.so`. Handshake log at `scratch/phase-2/handshake.log` line 5: `platformsvc: connected, server=0.0.1-pocb schema=1`. |

## APK size baseline
- Phase 2 multi-ABI: 35,521,452 bytes (~34M) -- `android/app/build/outputs/apk/debug/app-debug.apk` post-Task-20 build at 11:23 local time on 2026-05-09. (`scratch/phase-2/apk-contents.txt` captured an earlier 27M build at 11:16; current build is 34M after the final wiring commit 9e5b88e.)
- POC-D arm64-only baseline: TBD -- not captured during phase 2. Informs phase 6 split-APK decision; should be captured before phase 6 planning.

## Drift between design doc 4.2 and proto/platform.proto
None at the schema level -- every spec 4.2 message + field was implemented as written.

Behavioral deviations forced by reality (not schema):
- `MessageHandler` dispatcher silently DROPS unhandled message types instead of returning `Error{NOT_IMPLEMENTED}`. Plan 1.4 said respond NOT_IMPLEMENTED, but the plan's own `unimplementedMessageReturnsNotImplemented` test (referenced in Task 11) reads only ONE response after BatteryState+Hello -- which only passes if the server does NOT reply to BatteryState. `NotImplementedHandler` exists in `MessageHandler.kt` as dead code; phase 3+ can wire request-style messages (`UsbDeviceListRequest`, `UsbSelectRequest`, `PttKeyRequest`, `PttUnkeyRequest`, `AudioDeviceListRequest`) to NOT_IMPLEMENTED responses if needed.
- `LocalServerSocket(String name)` binds in the Linux abstract namespace, not the filesystem. Plan 1.3 implied filesystem. Resolved by passing `@<path>` to the Go child via `GRAYWOLF_PLATFORM_SOCKET` env so Go's `net.Dial("unix", ...)` resolves the same abstract socket. No plan-level schema change; runtime convention only.
- The protobuf-gradle-plugin's `proto.include("platform.proto")` filter is silently ignored on AGP-bridged source sets in Kotlin DSL; switched to `exclude("graywolf.proto")` (commit a7dc467), which works.

## Phase 3 prerequisites
- `cmd/graywolf/main_android.go` stub: **present** -- 1c262e8 (one-line `func main() {}` gated `//go:build android`; phase 3 replaces with real Android entry).
- `pkg/gps/android.go` stub: **N/A** -- `pkg/gps/serial.go` and the rest of `pkg/gps` are already cross-portable; cross-compile probe came back clean.
- `pkg/pttdevice/android.go` stub: **N/A** -- `pkg/pttdevice/cm108_linux.go` already has `//go:build linux` and paired sibling files (`cm108_modem.go !linux`, `sysfs_other.go`, `gpio_lines_linux.go` / `gpio_lines_other.go`, `enumerate_nonwindows.go`) cover the non-linux case.
- Cross-compile of `./...` for android target: **pass** -- `GOOS=android GOARCH=arm64 CGO_ENABLED=0 GOWORK=off go build ./...` exits 0; `go list ./...` under GOOS=android returns 59 packages clean. Probe log: `scratch/phase-2/android-build-probe.log`.

## Issues surfaced

1. **armv7 dropped (plan 1.9 deviation).** Go's `android/arm` target requires cgo + NDK C toolchain -- internal linker doesn't support 32-bit Android. minSdk=28 leaves zero realistic armv7 devices (last common armv7 tablets cap at Android 8). Resolution: Rust still builds for all 3 ABIs in Cargo (cargo-ndk handles it), but Go matrix + AGP `abiFilters` ship arm64-v8a + x86_64 only. Final APK has 2 ABIs. Commit e6583dc.

2. **x86_64 Go also needs cgo.** Plan assumed `CGO_ENABLED=0` worked for all ABIs. Discovered during Task 16: only `android/arm64` supports Go's internal linker; `android/amd64` requires cgo + NDK clang too. Resolution: per-ABI `GoAbi` config in `goCrossCompile_*` Exec tasks; arm64 stays `CGO_ENABLED=0`, x86_64 sets `CGO_ENABLED=1` + `CC=$ANDROID_NDK_ROOT/toolchains/llvm/prebuilt/<host>/bin/x86_64-linux-android28-clang`. Phase 2 spec did not anticipate this. Commit e2bbea5.

3. **AGP 8.5 disables BuildConfig generation by default.** Plan Task 12 used `BuildConfig.VERSION_NAME` but it didn't compile on AGP 8.5 without `buildFeatures.buildConfig = true`. Resolution: added the toggle in `android/app/build.gradle.kts`. Single-line fix bundled into Task 12 commit (0058259).

4. **`LocalServerSocket(String)` binds abstract, not filesystem.** Plan 1.3 implied filesystem. Discovered during Task 20 device handshake (Go child got `connect: no such file or directory`). Resolution: GraywolfService passes `@<cacheDir>/platform.sock` as `GRAYWOLF_PLATFORM_SOCKET` to the Go child; both sides agree on the abstract namespace. Commits 0058259 (server bringup) + 9e5b88e (Go child dial).

5. **JDK NIO UDS APIs absent from android.jar API 28 stubs.** Plan Task 11 used `java.net.UnixDomainSocketAddress` + `ServerSocketChannel.open(StandardProtocolFamily.UNIX)` directly; both compile against android.jar API 28 (the APIs are missing from the stubs) but exist at runtime under JDK 17. Resolution: `startForTest()` accesses both via reflection. Production `start()` uses `LocalServerSocket` (API 1) and never touches the JDK NIO path. Commit acae01d.

6. **`unimplementedMessageReturnsNotImplemented` test contradicts plan 1.4.** Plan 1.4 said respond `Error{NOT_IMPLEMENTED}` for non-Hello/non-GpsFix variants; the plan's own test reads only ONE reply after BatteryState+Hello -> server must NOT reply to BatteryState. Resolution: dispatcher silently drops + logs unhandled types. `NotImplementedHandler` retained in `MessageHandler.kt` as dead code; phase 3+ can revisit (e.g., differentiate request-style vs notification-style by oneof variant naming). Commit acae01d.

7. **Plan's `proto { include() }` filter ignored by AGP source set.** Plan Task 9 used `proto.include("platform.proto")` to keep `graywolf.proto` out of Android codegen. AGP-bridged source set silently ignores `include()`; `Graywolf.java` was generated unwanted. Resolution: switched to `exclude("graywolf.proto")` (Task 9 follow-up commit a7dc467).

8. **Plan Task 4 stub deviation.** Plan declared `Client.ConnectWithReconnect` in the interface (Task 3) but Task 4 `client_impl.go` did NOT implement it; the impl came in Task 6. To make Task 4 compile, a stub `ConnectWithReconnect` returning `errors.New("not implemented yet")` was added on `*clientImpl`; Task 6 (commit 19acabf) replaced it with the real reconnect loop with exponential backoff.
