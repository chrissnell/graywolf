# Android Phase 4a Implementation Plan — GPS, Platform Abstraction, UI Gating, App Icon

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire Android GPS through to the Svelte SPA + beacon scheduler + position log; introduce a single-source-of-truth `Platform.kind` enum across SPA/Go/Kotlin; permanently hide desktop-only UI surfaces; ship a graywolf-branded launcher and notification icon.

**Architecture:** Kotlin `GpsAdapter` consumes `LocationManager.GPS_PROVIDER` + `GnssStatus`, builds `GpsFix` / `GnssStatusUpdate` protobufs, and broadcasts them through the existing `PlatformServer` UDS. The Go child's android-tagged reader pumps fixes into the existing `gps.PositionCache` / `gps.SatelliteCache` so beacon scheduling, position log, and `/api/gps/state` work unchanged. Platform-specific UI gating collapses to a single `Platform.kind` constant per language.

**Tech Stack:**
- Kotlin (Android Service + protobuf-javalite 3.25.3 + LocationManager / GnssStatus)
- Go 1.24+ (`pkg/gps`, `pkg/platformsvc`, new `pkg/platform`, `//go:build android`)
- protoc 3 / `protoc-gen-go` for `proto/platform.proto`
- Svelte 5 + svelte-spa-router
- Vitest / node:test for SPA tests, JUnit (Robolectric optional) for Kotlin
- Android Gradle Plugin + protobuf-gradle-plugin 0.9.4

**Spec:** `docs/superpowers/specs/2026-05-10-android-phase-4a-gps-design.md` is authoritative. The plan implements that spec without redesigning anything; if a step seems to disagree with the spec, the spec wins.

**Build hygiene reminders (memory-derived, apply throughout):**
- All Android Go builds use `GOWORK=off` (worktree go.work would silently redirect sources).
- Never amend commits when a hook fails — fix and create a new commit.
- Never push to `main` directly; this plan lives on a feature branch.
- Adaptive icons + manifest changes ship as binary or near-binary asset edits — call out the rendering tool used and verify on device.

---

## Pre-flight

- [ ] **Step 0a: Create a feature branch off `main`**

```bash
git fetch origin
git checkout -b feature/android-phase-4a origin/main
```

- [ ] **Step 0b: Verify proto + Android toolchain are present**

```bash
which protoc protoc-gen-go
ls android/gradlew
```

Expected: `protoc` and `protoc-gen-go` resolve on PATH (used by `make proto`); `android/gradlew` exists.

- [ ] **Step 0c: Verify clean baseline build**

```bash
go test ./...
GOOS=android GOARCH=arm64 CGO_ENABLED=0 GOWORK=off go build ./...
cd web && npm test --silent
```

Expected: all green. If any of these fail on `main` already, stop and surface the breakage to the user before continuing.

---

## Task 1: Extend `proto/platform.proto` with `accuracy_m` and `GnssStatusUpdate`

**Files:**
- Modify: `proto/platform.proto`
- Regenerate: `pkg/platformproto/platform.pb.go`

- [ ] **Step 1: Add `accuracy_m = 10` to `GpsFix`**

Edit `proto/platform.proto`. Inside `message GpsFix`, append a new field after `source = 9`:

```proto
message GpsFix {
  double lat = 1;
  double lon = 2;
  double alt_m = 3;
  double speed_mps = 4;
  double course_deg = 5;
  int64 time_unix_ms = 6;
  double hdop = 7;
  uint32 num_sats = 8;
  GpsSource source = 9;
  // Horizontal accuracy radius in meters (Android Location.getAccuracy()).
  // 0 means "unknown / not provided" — desktop NMEA path leaves this 0.
  // (JSON consumers reading /api/gps/state must treat 0 as "unset", not
  // "perfect"; Android Location.accuracy never returns 0 in practice.)
  // Old clients ignore the field; old servers send 0. Schema version stays 1.
  double accuracy_m = 10;
  // Presence bits. Android Location exposes hasAltitude()/hasSpeed()/
  // hasBearing(); without these companion bools, the receiver cannot
  // distinguish a legitimate zero (stationary station, due-north course,
  // sea-level altitude) from "field absent". Default false on old senders.
  bool has_alt = 11;
  bool has_speed = 12;
  bool has_course = 13;
}
```

- [ ] **Step 2: Add `SatInfo` and `GnssStatusUpdate` messages**

Append at end of `proto/platform.proto` (above the final `}` of the file):

```proto
message SatInfo {
  uint32 svid = 1;          // satellite vehicle id
  string constellation = 2; // "GPS" / "GLONASS" / "BEIDOU" / "GALILEO" / "QZSS" / "SBAS"
  double cn0_dbhz = 3;      // signal strength
  bool used_in_fix = 4;
  double elevation_deg = 5;
  double azimuth_deg = 6;
}

message GnssStatusUpdate {
  uint32 sats_in_view = 1;
  uint32 sats_used = 2;
  repeated SatInfo sats = 3;
}
```

- [ ] **Step 3: Add `gnss_status = 17` to the `PlatformMessage` oneof**

Inside the `PlatformMessage` body oneof (currently ends at `Error error = 16`), append:

```proto
    GnssStatusUpdate gnss_status = 17;  // server → client; pushed alongside GpsFix
```

- [ ] **Step 4: Regenerate Go bindings**

```bash
make proto
```

Expected: `pkg/platformproto/platform.pb.go` is updated; new types `SatInfo`, `GnssStatusUpdate`, `PlatformMessage_GnssStatus` exist.

- [ ] **Step 5: Smoke check the regenerated file**

```bash
grep -c "GnssStatusUpdate\|SatInfo\|PlatformMessage_GnssStatus\|AccuracyM\|HasAlt\|HasSpeed\|HasCourse" pkg/platformproto/platform.pb.go
```

Expected: nonzero count for every term above.

- [ ] **Step 6: Verify both desktop and android Go cross-compile**

```bash
go build ./...
GOOS=android GOARCH=arm64 CGO_ENABLED=0 GOWORK=off go build ./...
```

Expected: both clean.

- [ ] **Step 7: Commit**

```bash
git add proto/platform.proto pkg/platformproto/platform.pb.go
git commit -m "feat(proto): add GpsFix.accuracy_m, presence bits, GnssStatusUpdate

GpsFix gains accuracy_m (field 10) for the Android horizontal-accuracy
radius and three presence bits (has_alt=11, has_speed=12, has_course=13)
so legitimate zero values are distinguishable from absent fields. New
GnssStatusUpdate carries per-satellite C/N0 + used-in-fix state for the
Android GPS page bar display. Schema stays at v1."
git push -u origin feature/android-phase-4a
```

---

## Task 2: Add `pkg/platform` package with build-tagged `Kind` constant

**Files:**
- Create: `pkg/platform/kind_android.go`
- Create: `pkg/platform/kind_default.go`
- Create: `pkg/platform/kind_test.go`

This is a brand-new tiny package; the existing `pkg/app/platform_other.go` / `platform_windows.go` are unrelated (path defaults) and stay untouched.

- [ ] **Step 1: Write the failing test**

Create `pkg/platform/kind_test.go`:

```go
package platform_test

import (
	"testing"

	"github.com/chrissnell/graywolf/pkg/platform"
)

func TestKindNotEmpty(t *testing.T) {
	if platform.Kind == "" {
		t.Fatal("platform.Kind is empty; expected one of: android, desktop")
	}
}

func TestKindKnownValue(t *testing.T) {
	switch platform.Kind {
	case "android", "desktop":
		// ok
	default:
		t.Fatalf("platform.Kind=%q is not one of the known values", platform.Kind)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./pkg/platform/...
```

Expected: FAIL — package or symbol missing.

- [ ] **Step 3: Add `kind_default.go` (desktop / windows / linux / darwin)**

Create `pkg/platform/kind_default.go`:

```go
//go:build !android

// Package platform exposes a single source-of-truth Kind constant for
// the host platform. Used by SPA / Go / Kotlin sites that need to gate
// behavior cross-compile-safely. The companion JS module is
// web/src/lib/platform.js; the Kotlin one is android/.../Platform.kt.
package platform

// Kind is the canonical platform identifier on this build.
// Desktop (Linux/macOS/Windows) and the Android phone build are the
// only two values today; future platforms add more build-tagged files.
const Kind = "desktop"
```

- [ ] **Step 4: Add `kind_android.go`**

Create `pkg/platform/kind_android.go`:

```go
//go:build android

package platform

const Kind = "android"
```

- [ ] **Step 5: Run the test — desktop build**

```bash
go test ./pkg/platform/...
```

Expected: PASS.

- [ ] **Step 6: Cross-compile for Android to verify the build tag**

```bash
GOOS=android GOARCH=arm64 CGO_ENABLED=0 GOWORK=off go build ./pkg/platform/...
```

Expected: clean (no "Kind redeclared" or missing-symbol errors).

- [ ] **Step 7: Commit**

```bash
git add pkg/platform/
git commit -m "feat(platform): introduce pkg/platform.Kind constant

Build-tagged const Kind = \"android\" / \"desktop\" replaces the implicit
\"is this Android?\" predicate scattered across pkg/app. Mirrors the SPA
Platform.kind and the Kotlin Platform.KIND that follow."
git push
```

---

## Task 3: SPA `Platform.kind` enum + dynamic getter

**Files:**
- Modify: `web/src/lib/platform.js`
- Modify: `web/src/lib/platform.test.js`

- [ ] **Step 1: Update the failing test**

Replace `web/src/lib/platform.test.js` with:

```js
import { test, beforeEach, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import { _resetForTests as resetBridge } from './androidBridge.js';
import { Platform, isAndroid, isDesktop } from './platform.js';

beforeEach(() => {
  resetBridge();
  delete globalThis.GraywolfWebInterface;
});
afterEach(() => {
  resetBridge();
  delete globalThis.GraywolfWebInterface;
});

test('Platform.kind === "desktop" when bridge absent', () => {
  assert.equal(Platform.kind, 'desktop');
});

test('Platform.kind === "android" when bridge present', () => {
  globalThis.GraywolfWebInterface = { getBearerToken: () => 'tok' };
  assert.equal(Platform.kind, 'android');
});

test('Platform.kind is read each access (dynamic)', () => {
  assert.equal(Platform.kind, 'desktop');
  globalThis.GraywolfWebInterface = { getBearerToken: () => 'tok' };
  assert.equal(Platform.kind, 'android');
  delete globalThis.GraywolfWebInterface;
  resetBridge();
  assert.equal(Platform.kind, 'desktop');
});

test('legacy isAndroid / isDesktop shims still honor Platform.kind', () => {
  assert.equal(isAndroid(), false);
  assert.equal(isDesktop(), true);
  globalThis.GraywolfWebInterface = { getBearerToken: () => 'tok' };
  assert.equal(isAndroid(), true);
  assert.equal(isDesktop(), false);
});
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd web && node --test src/lib/platform.test.js
```

Expected: FAIL — `Platform` not exported.

- [ ] **Step 3: Replace `web/src/lib/platform.js`**

```js
// Platform detection. Returns 'android' when running inside the
// Android WebView (signalled by the GraywolfWebInterface JS bridge),
// otherwise 'desktop'. Used by Svelte routes/components that gate
// surfaces on host platform.
//
// Read via Platform.kind. The kind getter is dynamic — it consults
// the bridge on every access — so test setups that toggle
// globalThis.GraywolfWebInterface between cases observe the change
// without re-importing.
//
// Companion modules: pkg/platform.Kind (Go) and Platform.KIND (Kotlin).

import { getBearerToken } from './androidBridge.js';

function detectKind() {
  return getBearerToken() !== null ? 'android' : 'desktop';
}

export const Platform = {
  get kind() { return detectKind(); },
};

// Legacy shims kept for one transitional commit (Task 4 sweep removes
// every callsite, then Task 4's final step deletes these exports).
export function isAndroid() { return Platform.kind === 'android'; }
export function isDesktop() { return Platform.kind === 'desktop'; }
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd web && node --test src/lib/platform.test.js
```

Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/platform.js web/src/lib/platform.test.js
git commit -m "feat(web): introduce Platform.kind enum (compat shims kept)

Platform.kind === 'android' | 'desktop' is the new single-source-of-truth
predicate. isAndroid()/isDesktop() stay as compat shims for one commit
while the per-route migration sweep lands."
git push
```

---

## Task 4: Migrate every SPA call site from `isAndroid()` / `isDesktop()` to `Platform.kind`, then delete the shims

**Files (modify; full list — verify with grep before editing):**
- `web/src/components/Sidebar.svelte`
- `web/src/routes/AudioDevices.svelte`
- `web/src/lib/platform.js` (final shim removal)
- `web/src/lib/platform.test.js` (drop the legacy-shim assertion)
- (Tasks 17 and 18 add more callsites — they migrate at write time, not via this sweep)

- [ ] **Step 1: Inventory every callsite**

```bash
cd web && grep -rn "isAndroid\|isDesktop" src/ \
  | grep -v "src/lib/platform.js" \
  | grep -v "src/lib/platform.test.js"
```

Expected after migration: empty output. Use this grep at the end as the gate.

- [ ] **Step 2: Migrate `web/src/components/Sidebar.svelte`**

In `web/src/components/Sidebar.svelte`, replace:

```js
  import { isAndroid } from '../lib/platform.js';
```

with:

```js
  import { Platform } from '../lib/platform.js';
```

And replace:

```js
      items: isAndroid()
        ? allSettingsItems.filter(it => !HIDDEN_ON_ANDROID.has(it.path))
        : allSettingsItems,
```

with:

```js
      items: Platform.kind === 'android'
        ? allSettingsItems.filter(it => !HIDDEN_ON_ANDROID.has(it.path))
        : allSettingsItems,
```

- [ ] **Step 3: Migrate `web/src/routes/AudioDevices.svelte`**

Replace:

```js
  import { isAndroid } from '../lib/platform.js';
```

with:

```js
  import { Platform } from '../lib/platform.js';
```

Then change every `isAndroid()` callsite (currently lines 268, 281, 421) to `Platform.kind === 'android'`. Use `Platform.kind !== 'android'` where the source had `!isAndroid()`.

- [ ] **Step 4: Verify the grep is clean**

```bash
cd web && grep -rn "isAndroid\|isDesktop" src/ \
  | grep -v "src/lib/platform.js" \
  | grep -v "src/lib/platform.test.js"
```

Expected: empty.

- [ ] **Step 5: Delete the compat shims**

In `web/src/lib/platform.js`, remove the two `isAndroid` / `isDesktop` exports, leaving only the `Platform` export.

In `web/src/lib/platform.test.js`, delete the `'legacy isAndroid / isDesktop shims still honor Platform.kind'` test and its imports of `isAndroid, isDesktop`.

- [ ] **Step 6: Run SPA tests**

```bash
cd web && npm test --silent
```

Expected: all tests pass (no `isAndroid`/`isDesktop` references anywhere outside platform.js).

- [ ] **Step 7: Commit**

```bash
git add web/src/components/Sidebar.svelte web/src/routes/AudioDevices.svelte web/src/lib/platform.js web/src/lib/platform.test.js
git commit -m "refactor(web): migrate isAndroid()/isDesktop() callsites to Platform.kind

All call sites now read Platform.kind directly. Compat shims removed
from platform.js. Migration matches the Go (pkg/platform.Kind) and
Kotlin (Platform.KIND) sibling enums."
git push
```

---

## Task 5: Add Kotlin `Platform` constant

**Files:**
- Create: `android/app/src/main/kotlin/com/nw5w/graywolf/Platform.kt`

- [ ] **Step 1: Write the file**

```kotlin
package com.nw5w.graywolf

/**
 * Single source-of-truth platform identifier for Kotlin sites.
 * Always "android" today (this module only ships in the Android APK).
 * Companion of pkg/platform.Kind (Go) and Platform.kind (web SPA).
 *
 * Provides a stable import surface so future iOS-shared Kotlin
 * Multiplatform code only needs to flip this one constant per target.
 */
object Platform {
    const val KIND = "android"
}
```

- [ ] **Step 2: Verify it compiles**

```bash
cd android && ./gradlew :app:compileDebugKotlin
```

Expected: BUILD SUCCESSFUL.

- [ ] **Step 3: Commit**

```bash
git add android/app/src/main/kotlin/com/nw5w/graywolf/Platform.kt
git commit -m "feat(android): introduce Platform.KIND constant

Mirrors pkg/platform.Kind and web/src/lib/platform.js so all three
languages express \"is this Android?\" the same way."
git push
```

---

## Task 6: Extend `pkg/platformsvc` Client with `SubscribeGnssStatus`

**Files:**
- Modify: `pkg/platformsvc/types.go`
- Modify: `pkg/platformsvc/client.go`
- Modify: `pkg/platformsvc/subscriptions.go`
- Modify: `pkg/platformsvc/client_impl.go`

All under `//go:build android`.

- [ ] **Step 1: Add type alias to `pkg/platformsvc/types.go`**

Append after the existing `AudioRouteChanged = pb.AudioRouteChanged` line:

```go
type GnssStatusUpdate = pb.GnssStatusUpdate
type SatInfo = pb.SatInfo
```

- [ ] **Step 2: Extend the Client interface in `pkg/platformsvc/client.go`**

After the existing `SubscribeGpsFix` method, add:

```go
	SubscribeGnssStatus(ctx context.Context, ch chan<- *GnssStatusUpdate) error
```

- [ ] **Step 3: Add the new subs slot in `pkg/platformsvc/client_impl.go`**

Inside the `clientImpl` struct, next to `gpsFixSubs`, add:

```go
	gnssStatusSubs []chan<- *GnssStatusUpdate
```

- [ ] **Step 4: Implement the Subscribe method in `pkg/platformsvc/subscriptions.go`**

Append:

```go
func (c *clientImpl) SubscribeGnssStatus(_ context.Context, ch chan<- *GnssStatusUpdate) error {
	if c.closed.Load() {
		return ErrClosed
	}
	c.subsMu.Lock()
	c.gnssStatusSubs = append(c.gnssStatusSubs, ch)
	c.subsMu.Unlock()
	return nil
}
```

- [ ] **Step 5: Extend `dispatch` in `pkg/platformsvc/client_impl.go`**

Inside the existing `dispatch(msg)` switch, after the `*pb.PlatformMessage_AudioRouteChanged` arm, add:

```go
	case *pb.PlatformMessage_GnssStatus:
		c.subsMu.Lock()
		subs := append([]chan<- *GnssStatusUpdate{}, c.gnssStatusSubs...)
		c.subsMu.Unlock()
		for _, s := range subs {
			select {
			case s <- b.GnssStatus:
			default:
			}
		}
```

- [ ] **Step 6: Cross-compile for Android**

```bash
GOOS=android GOARCH=arm64 CGO_ENABLED=0 GOWORK=off go build ./pkg/platformsvc/...
```

Expected: clean.

- [ ] **Step 7: Commit (test added in Task 7)**

```bash
git add pkg/platformsvc/types.go pkg/platformsvc/client.go pkg/platformsvc/client_impl.go pkg/platformsvc/subscriptions.go
git commit -m "feat(platformsvc): add SubscribeGnssStatus to Client interface

Mirrors SubscribeGpsFix but routes the per-sat GnssStatusUpdate proto
to subscribed channels. Enables the Go side of phase 4a's Android GPS
page (per-sat C/N0 bars + used/in-view counts)."
git push
```

---

## Task 7: Test for `SubscribeGnssStatus` dispatch

**Files:**
- Modify: `pkg/platformsvc/client_test.go`

- [ ] **Step 1: Read the existing test file to find the helper that injects a fake conn**

```bash
grep -n "injectConn\|TestSubscribeGpsFix\|net.Pipe" pkg/platformsvc/client_test.go
```

Expected: locate the existing GpsFix subscription test as a template.

- [ ] **Step 2: Write the failing test**

Append to `pkg/platformsvc/client_test.go`:

```go
func TestSubscribeGnssStatusDelivers(t *testing.T) {
	cli := &clientImpl{closeCh: make(chan struct{})}
	server, client := net.Pipe()
	cli.injectConn(client)
	t.Cleanup(func() { _ = cli.Close(); _ = server.Close() })

	ch := make(chan *GnssStatusUpdate, 4)
	if err := cli.SubscribeGnssStatus(context.Background(), ch); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Server-side push of a GnssStatusUpdate frame.
	push := &pb.PlatformMessage{Body: &pb.PlatformMessage_GnssStatus{
		GnssStatus: &pb.GnssStatusUpdate{
			SatsInView: 11,
			SatsUsed:   8,
			Sats: []*pb.SatInfo{
				{Svid: 5, Constellation: "GPS", Cn0Dbhz: 41.5, UsedInFix: true},
			},
		},
	}}
	if err := writeFrame(server, push); err != nil {
		t.Fatalf("server write: %v", err)
	}

	select {
	case got := <-ch:
		if got.GetSatsInView() != 11 || got.GetSatsUsed() != 8 || len(got.GetSats()) != 1 {
			t.Fatalf("unexpected payload: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for GnssStatusUpdate")
	}
}

func TestGnssStatusDoesNotCrossTalkToGpsFixSub(t *testing.T) {
	cli := &clientImpl{closeCh: make(chan struct{})}
	server, client := net.Pipe()
	cli.injectConn(client)
	t.Cleanup(func() { _ = cli.Close(); _ = server.Close() })

	gpsCh := make(chan *GpsFix, 4)
	gnssCh := make(chan *GnssStatusUpdate, 4)
	_ = cli.SubscribeGpsFix(context.Background(), gpsCh)
	_ = cli.SubscribeGnssStatus(context.Background(), gnssCh)

	push := &pb.PlatformMessage{Body: &pb.PlatformMessage_GnssStatus{
		GnssStatus: &pb.GnssStatusUpdate{SatsInView: 1},
	}}
	_ = writeFrame(server, push)

	select {
	case <-gnssCh:
		// good
	case <-time.After(time.Second):
		t.Fatal("expected GnssStatus on its channel")
	}

	select {
	case got := <-gpsCh:
		t.Fatalf("GpsFix subscriber leaked a GnssStatus event: %+v", got)
	case <-time.After(200 * time.Millisecond):
		// good
	}
}
```

(If `pb`, `net`, `time`, `context` aren't already imported in this file, add them.)

- [ ] **Step 3: Run the test**

```bash
GOOS=android GOARCH=arm64 CGO_ENABLED=0 GOWORK=off go test ./pkg/platformsvc/...
```

Expected: PASS for both new tests (and every existing test).

(Note: the `//go:build android` constraint on this file forces `GOOS=android` for `go test`. If the tests are intended to run on the host, drop the build constraint or move the test file. Confirm by inspecting the existing `client_test.go` build tag and replicating it.)

- [ ] **Step 4: Commit**

```bash
git add pkg/platformsvc/client_test.go
git commit -m "test(platformsvc): cover GnssStatus dispatch + non-crosstalk

Asserts SubscribeGnssStatus delivers the proto and that a concurrent
GpsFix subscriber doesn't see GnssStatus events (and vice versa)."
git push
```

---

## Task 8: Kotlin `PlatformServer` broadcast path — `activeOutputs`, per-stream synchronization, `broadcastGpsFix`, `broadcastGnssStatus`

**Files:**
- Modify: `android/app/src/main/kotlin/com/nw5w/graywolf/platformsvc/PlatformServer.kt`

This task adds the server→client push direction and synchronizes every `WireCodec.writeFrame` call so concurrent broadcast and response writes can't interleave on the same OutputStream.

- [ ] **Step 1: Add `activeOutputs` + lock + broadcast helpers**

In `PlatformServer.kt`, inside the class body and next to the existing `gpsFixSubs` field, add:

```kotlin
    @Volatile private var activeOutputs: List<OutputStream> = emptyList()
    private val outputsLock = Object()

    /**
     * Push a server-produced PlatformMessage to every connected client.
     * Synchronizes per-stream: serveClient's response writes also wrap
     * WireCodec.writeFrame in synchronized(out) so a concurrent
     * broadcast can't interleave bytes with a response frame.
     */
    private fun broadcast(msg: com.nw5w.graywolf.platformproto.PlatformMessage) {
        val outs = activeOutputs  // snapshot — CoW List, safe to iterate
        for (os in outs) {
            try {
                synchronized(os) { WireCodec.writeFrame(os, msg) }
            } catch (_: IOException) {
                // serveClient will remove the dead stream on its next read failure.
            }
        }
    }

    fun broadcastGpsFix(fix: com.nw5w.graywolf.platformproto.GpsFix) =
        broadcast(com.nw5w.graywolf.platformproto.PlatformMessage.newBuilder().setGpsFix(fix).build())

    fun broadcastGnssStatus(status: com.nw5w.graywolf.platformproto.GnssStatusUpdate) =
        broadcast(com.nw5w.graywolf.platformproto.PlatformMessage.newBuilder().setGnssStatus(status).build())
```

- [ ] **Step 2: Track `activeOutputs` in `serveClient`**

Replace the existing `private fun serveClient(stream: ClientStream)` body with:

```kotlin
    private fun serveClient(stream: ClientStream) {
        val out = stream.outputStream()
        val input = stream.inputStream()
        var registered = false
        try {
            while (running) {
                val req = WireCodec.readFrame(input)
                val handler: MessageHandler = when (req.bodyCase) {
                    PlatformMessage.BodyCase.HELLO ->
                        HelloHandler(serverVersion, schemaVersion)
                    PlatformMessage.BodyCase.GPS_FIX ->
                        GpsFixHandler(onFix = { fix -> gpsFixSubs.forEach { it(fix) } })
                    else -> {
                        Log.d(TAG, "dropping unhandled notification ${req.bodyCase.name}")
                        continue
                    }
                }
                val resp = handler.handle(req)
                if (resp != null) {
                    synchronized(out) { WireCodec.writeFrame(out, resp) }
                    // Register into activeOutputs only after a successful Hello
                    // round-trip — otherwise a broadcast that fires during the
                    // pre-Hello window would write into an unframed stream the
                    // client isn't yet ready to read, and the first fix could
                    // be silently lost. Hello is the only handshake message,
                    // so register exactly once on its success.
                    if (!registered &&
                        req.bodyCase == PlatformMessage.BodyCase.HELLO &&
                        resp.bodyCase != PlatformMessage.BodyCase.ERROR) {
                        synchronized(outputsLock) { activeOutputs = activeOutputs + out }
                        registered = true
                    }
                    if (req.bodyCase == PlatformMessage.BodyCase.HELLO &&
                        resp.bodyCase == PlatformMessage.BodyCase.ERROR) {
                        return
                    }
                }
            }
        } catch (e: IOException) {
            Log.i(TAG, "client disconnected: $e")
        } finally {
            if (registered) {
                synchronized(outputsLock) { activeOutputs = activeOutputs - out }
            }
            try { stream.close() } catch (_: IOException) {}
        }
    }
```

The deltas vs the existing implementation:
1. The existing `WireCodec.writeFrame(out, resp)` is wrapped in `synchronized(out) { ... }`.
2. After a successful Hello round-trip (and only then), register the stream into `activeOutputs` under `outputsLock`. This avoids a first-fix race: if the GpsAdapter broadcasts before the client has finished its Hello handshake, the broadcast would write framed bytes the client isn't ready to deframe.
3. In the `finally` block, deregister the stream — but only if `registered` is true (Hello-failed clients never registered, so don't try to remove a stream that was never added).

- [ ] **Step 3: Verify the file compiles**

```bash
cd android && ./gradlew :app:compileDebugKotlin
```

Expected: BUILD SUCCESSFUL.

- [ ] **Step 4: Commit (tests in Task 12)**

```bash
git add android/app/src/main/kotlin/com/nw5w/graywolf/platformsvc/PlatformServer.kt
git commit -m "feat(android-platformsvc): add server-to-client broadcast path

Adds activeOutputs (registered in serveClient AFTER a successful Hello
round-trip, under outputsLock), broadcast(msg), broadcastGpsFix(fix),
broadcastGnssStatus(status). Registering after Hello avoids a first-fix
race where a broadcast fired before the client had completed handshake
would land on an unframed pre-handshake stream. Every WireCodec.writeFrame
on a connected client stream is wrapped in synchronized(out) so response
writes and broadcast writes cannot interleave bytes on the wire."
git push
```

---

## Task 9: Kotlin `GpsAdapter`

**Files:**
- Create: `android/app/src/main/kotlin/com/nw5w/graywolf/gps/GpsAdapter.kt`

- [ ] **Step 1: Write the source file**

```kotlin
package com.nw5w.graywolf.gps

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.location.GnssStatus
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import android.util.Log
import androidx.core.content.ContextCompat
import com.nw5w.graywolf.platformproto.GnssStatusUpdate
import com.nw5w.graywolf.platformproto.GpsFix
import com.nw5w.graywolf.platformproto.GpsSource
import com.nw5w.graywolf.platformproto.SatInfo
import com.nw5w.graywolf.platformsvc.PlatformServer

/**
 * GPS producer: subscribes to the system LocationManager, translates
 * each Location into a GpsFix proto and each GnssStatus callback into
 * a GnssStatusUpdate proto, and pushes them through PlatformServer's
 * server-to-client broadcast.
 *
 * Lifecycle: start() in GraywolfService.onCreate after PlatformServer.start;
 * stop() in onDestroy before PlatformServer.stop. start() is a silent
 * no-op if ACCESS_FINE_LOCATION is not granted — the user must re-grant
 * via system settings, then re-launch the app.
 */
class GpsAdapter(
    private val ctx: Context,
    private val server: PlatformServer,
) {
    private val locationManager =
        ctx.getSystemService(Context.LOCATION_SERVICE) as LocationManager

    @Volatile private var lastSatCount: Int = 0
    @Volatile private var started: Boolean = false

    private val locationListener = LocationListener { loc -> onLocation(loc) }

    private val gnssStatusCallback = object : GnssStatus.Callback() {
        override fun onSatelliteStatusChanged(status: GnssStatus) {
            var used = 0
            val builder = GnssStatusUpdate.newBuilder()
            for (i in 0 until status.satelliteCount) {
                val isUsed = status.usedInFix(i)
                if (isUsed) used++
                builder.addSats(SatInfo.newBuilder()
                    .setSvid(status.getSvid(i))
                    .setConstellation(constellationName(status.getConstellationType(i)))
                    .setCn0Dbhz(status.getCn0DbHz(i).toDouble())
                    .setUsedInFix(isUsed)
                    .setElevationDeg(status.getElevationDegrees(i).toDouble())
                    .setAzimuthDeg(status.getAzimuthDegrees(i).toDouble())
                    .build())
            }
            lastSatCount = used
            server.broadcastGnssStatus(builder
                .setSatsInView(status.satelliteCount)
                .setSatsUsed(used)
                .build())
        }
    }

    fun start() {
        if (started) return
        if (ContextCompat.checkSelfPermission(ctx, Manifest.permission.ACCESS_FINE_LOCATION)
            != PackageManager.PERMISSION_GRANTED) {
            Log.i(TAG, "GpsAdapter.start skipped — ACCESS_FINE_LOCATION not granted")
            return
        }
        try {
            locationManager.requestLocationUpdates(
                LocationManager.GPS_PROVIDER,
                10_000L, 0f, locationListener
            )
            locationManager.registerGnssStatusCallback(gnssStatusCallback, /* handler = */ null)
            started = true
            Log.i(TAG, "GpsAdapter started: GPS_PROVIDER 10s/0m + GNSS status callback")
        } catch (se: SecurityException) {
            Log.w(TAG, "GpsAdapter start hit SecurityException: $se")
        }
    }

    fun stop() {
        if (!started) return
        started = false
        try { locationManager.removeUpdates(locationListener) } catch (_: Throwable) {}
        try { locationManager.unregisterGnssStatusCallback(gnssStatusCallback) } catch (_: Throwable) {}
    }

    /** Visible for testing. */
    internal fun toGpsFix(loc: Location, satCount: Int): GpsFix {
        return GpsFix.newBuilder()
            .setLat(loc.latitude)
            .setLon(loc.longitude)
            .setAltM(if (loc.hasAltitude()) loc.altitude else 0.0)
            .setHasAlt(loc.hasAltitude())
            .setSpeedMps(if (loc.hasSpeed()) loc.speed.toDouble() else 0.0)
            .setHasSpeed(loc.hasSpeed())
            .setCourseDeg(if (loc.hasBearing()) loc.bearing.toDouble() else 0.0)
            .setHasCourse(loc.hasBearing())
            .setTimeUnixMs(loc.time)
            .setHdop(0.0) // Android doesn't expose HDOP — see proto comment on accuracy_m.
            .setNumSats(satCount.coerceAtLeast(0))
            .setSource(GpsSource.GPS_SOURCE_ANDROID_GPS)
            .setAccuracyM(if (loc.hasAccuracy()) loc.accuracy.toDouble() else 0.0)
            .build()
    }

    private fun onLocation(loc: Location) {
        server.broadcastGpsFix(toGpsFix(loc, lastSatCount))
    }

    private fun constellationName(type: Int): String = when (type) {
        GnssStatus.CONSTELLATION_GPS -> "GPS"
        GnssStatus.CONSTELLATION_GLONASS -> "GLONASS"
        GnssStatus.CONSTELLATION_BEIDOU -> "BEIDOU"
        GnssStatus.CONSTELLATION_GALILEO -> "GALILEO"
        GnssStatus.CONSTELLATION_QZSS -> "QZSS"
        GnssStatus.CONSTELLATION_SBAS -> "SBAS"
        else -> "UNKNOWN"
    }

    companion object { private const val TAG = "GpsAdapter" }
}
```

- [ ] **Step 2: Verify it compiles**

```bash
cd android && ./gradlew :app:compileDebugKotlin
```

Expected: BUILD SUCCESSFUL.

- [ ] **Step 3: Commit (tests in Task 10)**

```bash
git add android/app/src/main/kotlin/com/nw5w/graywolf/gps/GpsAdapter.kt
git commit -m "feat(android): GpsAdapter — LocationManager → GpsFix producer

Subscribes to GPS_PROVIDER (10s / 0m cadence) and GnssStatus.Callback,
maps each Location into a GpsFix proto, each GnssStatus snapshot into
a GnssStatusUpdate proto, broadcasts both via PlatformServer. Permission
gate is silent: missing ACCESS_FINE_LOCATION → log + early return."
git push
```

---

## Task 10: GpsAdapter unit test

**Files:**
- Create: `android/app/src/test/kotlin/com/nw5w/graywolf/gps/GpsAdapterTest.kt`

(Test placement matches the existing platformsvc tests — confirm with `find android/app/src/test -name "*Test.kt"` before creating.)

- [ ] **Step 1: Write the failing test**

```kotlin
package com.nw5w.graywolf.gps

import android.location.Location
import com.nw5w.graywolf.platformproto.GpsSource
import org.junit.Test
import org.mockito.Mockito.mock
import org.mockito.Mockito.`when`
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class GpsAdapterTest {
    @Test fun toGpsFix_populatesAllFields() {
        val loc = mock(Location::class.java)
        `when`(loc.latitude).thenReturn(39.7392)
        `when`(loc.longitude).thenReturn(-104.9903)
        `when`(loc.hasAltitude()).thenReturn(true)
        `when`(loc.altitude).thenReturn(1609.0)
        `when`(loc.hasSpeed()).thenReturn(true)
        `when`(loc.speed).thenReturn(0.5f)
        `when`(loc.hasBearing()).thenReturn(true)
        `when`(loc.bearing).thenReturn(142.0f)
        `when`(loc.time).thenReturn(1_700_000_000_000L)
        `when`(loc.hasAccuracy()).thenReturn(true)
        `when`(loc.accuracy).thenReturn(4.8f)

        val adapter = GpsAdapter(mock(android.content.Context::class.java),
                                 mock(com.nw5w.graywolf.platformsvc.PlatformServer::class.java))
        val fix = adapter.toGpsFix(loc, 8)

        assertEquals(39.7392, fix.lat, 1e-9)
        assertEquals(-104.9903, fix.lon, 1e-9)
        assertEquals(1609.0, fix.altM, 1e-9)
        assertEquals(0.5, fix.speedMps, 1e-9)
        assertEquals(142.0, fix.courseDeg, 1e-9)
        assertEquals(1_700_000_000_000L, fix.timeUnixMs)
        assertEquals(4.8, fix.accuracyM, 1e-6)
        assertEquals(8, fix.numSats.toInt())
        assertEquals(GpsSource.GPS_SOURCE_ANDROID_GPS, fix.source)
        assertTrue(fix.hdop == 0.0) // sentinel — see spec §3.3
        assertTrue(fix.hasAlt)
        assertTrue(fix.hasSpeed)
        assertTrue(fix.hasCourse)
    }

    @Test fun toGpsFix_defaultsWhenLocationFieldsAbsent() {
        val loc = mock(Location::class.java)
        `when`(loc.latitude).thenReturn(0.0)
        `when`(loc.longitude).thenReturn(0.0)
        `when`(loc.hasAltitude()).thenReturn(false)
        `when`(loc.hasSpeed()).thenReturn(false)
        `when`(loc.hasBearing()).thenReturn(false)
        `when`(loc.hasAccuracy()).thenReturn(false)
        `when`(loc.time).thenReturn(0L)

        val adapter = GpsAdapter(mock(android.content.Context::class.java),
                                 mock(com.nw5w.graywolf.platformsvc.PlatformServer::class.java))
        val fix = adapter.toGpsFix(loc, 0)

        assertEquals(0.0, fix.altM, 1e-9)
        assertEquals(0.0, fix.speedMps, 1e-9)
        assertEquals(0.0, fix.courseDeg, 1e-9)
        assertEquals(0.0, fix.accuracyM, 1e-9)
        assertEquals(0, fix.numSats.toInt())
        assertEquals(false, fix.hasAlt)
        assertEquals(false, fix.hasSpeed)
        assertEquals(false, fix.hasCourse)
    }
}
```

(If the project's existing test deps don't already include Mockito, copy the relevant `testImplementation` lines from `android/app/build.gradle.kts` from another test-dependent task; if absent, add `testImplementation("org.mockito:mockito-core:5.11.0")` and re-run gradle sync.)

- [ ] **Step 2: Run the test**

```bash
cd android && ./gradlew :app:testDebugUnitTest --tests "*GpsAdapterTest*"
```

Expected: 2 tests PASSED.

- [ ] **Step 3: Commit**

```bash
git add android/app/src/test/kotlin/com/nw5w/graywolf/gps/GpsAdapterTest.kt
# If you had to add Mockito to build.gradle.kts:
git add android/app/build.gradle.kts
git commit -m "test(android): GpsAdapter — Location → GpsFix mapping

Covers the populated-and-default branches of toGpsFix and asserts the
HDOP sentinel + GPS_SOURCE_ANDROID_GPS source on every fix."
git push
```

---

## Task 11: PlatformServer broadcast test

**Files:**
- Modify: existing PlatformServer test file (find with `find android/app/src/test -name "PlatformServer*Test.kt"`); if none, create `android/app/src/test/kotlin/com/nw5w/graywolf/platformsvc/PlatformServerBroadcastTest.kt`.

- [ ] **Step 1: Locate the existing test or create new**

```bash
find android/app/src/test -name "PlatformServer*Test*"
```

- [ ] **Step 2: Write the failing test**

If creating a new file, content:

```kotlin
package com.nw5w.graywolf.platformsvc

import com.nw5w.graywolf.platformproto.GpsFix
import com.nw5w.graywolf.platformproto.GpsSource
import com.nw5w.graywolf.platformproto.PlatformMessage
import org.junit.After
import org.junit.Before
import org.junit.Test
import java.io.File
import java.net.UnixDomainSocketAddress
import java.nio.channels.SocketChannel
import java.nio.channels.Channels
import kotlin.test.assertEquals

class PlatformServerBroadcastTest {
    private lateinit var server: PlatformServer
    private lateinit var sockPath: String

    @Before fun setup() {
        sockPath = File.createTempFile("gw-test-", ".sock").also { it.delete() }.absolutePath
        server = PlatformServer(socketPath = sockPath, serverVersion = "test", schemaVersion = 1)
        server.startForTest()
    }

    @After fun teardown() { server.stop() }

    @Test fun broadcastGpsFix_reachesConnectedClient() {
        val ch = SocketChannel.open(UnixDomainSocketAddress.of(sockPath))
        val out = Channels.newOutputStream(ch)
        val input = Channels.newInputStream(ch)

        // Send Hello so serveClient registers the stream into activeOutputs
        // before the broadcast fires.
        WireCodec.writeFrame(out,
            PlatformMessage.newBuilder().setHello(
                com.nw5w.graywolf.platformproto.Hello.newBuilder()
                    .setSchemaVersion(1).setClientVersion("t").build()
            ).build())
        // drain the Hello response
        WireCodec.readFrame(input)

        val fix = GpsFix.newBuilder()
            .setLat(1.0).setLon(2.0).setSource(GpsSource.GPS_SOURCE_ANDROID_GPS).build()
        server.broadcastGpsFix(fix)

        val msg = WireCodec.readFrame(input)
        assertEquals(PlatformMessage.BodyCase.GPS_FIX, msg.bodyCase)
        assertEquals(1.0, msg.gpsFix.lat, 1e-9)
        assertEquals(2.0, msg.gpsFix.lon, 1e-9)

        ch.close()
    }
}
```

- [ ] **Step 3: Run the test**

```bash
cd android && ./gradlew :app:testDebugUnitTest --tests "*PlatformServerBroadcast*"
```

Expected: PASSED.

- [ ] **Step 4: Commit**

```bash
git add android/app/src/test/kotlin/com/nw5w/graywolf/platformsvc/PlatformServerBroadcastTest.kt
git commit -m "test(android-platformsvc): cover broadcastGpsFix → connected client

End-to-end test through the test-mode UDS: Hello round-trip primes
activeOutputs, then a server-side broadcastGpsFix arrives on the client
stream as a GPS_FIX-bodied PlatformMessage frame."
git push
```

---

## Task 12: Go `pkg/gps/android.go` — RunAndroid + RunAndroidGnss + tests

The existing `pkg/gps` exposes `RunSerial(ctx, cfg, cache, logger) error` and `RunGPSD(...)`. The android reader keeps that shape so it slots into `gpsManager` cleanly.

**Files:**
- Create: `pkg/gps/android.go`
- Create: `pkg/gps/android_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/gps/android_test.go`:

```go
//go:build android

package gps

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/platformproto"
)

// fakeGpsClient implements the subset of platformsvc.Client that
// RunAndroid + RunAndroidGnss depend on. close() must be called from
// t.Cleanup to drain the relay goroutines and prevent leaks under -race.
type fakeGpsClient struct {
	gpsCh  chan *pb.GpsFix
	gnssCh chan *pb.GnssStatusUpdate
}

func (f *fakeGpsClient) SubscribeGpsFix(_ context.Context, ch chan<- *pb.GpsFix) error {
	go func() {
		for fix := range f.gpsCh {
			ch <- fix
		}
	}()
	return nil
}
func (f *fakeGpsClient) SubscribeGnssStatus(_ context.Context, ch chan<- *pb.GnssStatusUpdate) error {
	go func() {
		for st := range f.gnssCh {
			ch <- st
		}
	}()
	return nil
}
func (f *fakeGpsClient) close() {
	if f.gpsCh != nil {
		close(f.gpsCh)
	}
	if f.gnssCh != nil {
		close(f.gnssCh)
	}
}

func TestRunAndroidWritesFixToCache(t *testing.T) {
	cli := &fakeGpsClient{gpsCh: make(chan *pb.GpsFix, 1)}
	t.Cleanup(cli.close)
	cache := NewMemCache()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := make(chan error, 1)
	go func() { errCh <- RunAndroid(ctx, cli, cache, logger) }()

	cli.gpsCh <- &pb.GpsFix{
		Lat:        39.7392,
		Lon:        -104.9903,
		AltM:       1609.0,
		HasAlt:     true,
		SpeedMps:   0.5,
		HasSpeed:   true,
		CourseDeg:  142.0,
		HasCourse:  true,
		TimeUnixMs: 1_700_000_000_000,
		AccuracyM:  4.8,
		Source:     pb.GpsSource_GPS_SOURCE_ANDROID_GPS,
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fix, ok := cache.Get()
		if ok && fix.Latitude == 39.7392 {
			if fix.Longitude != -104.9903 {
				t.Fatalf("unexpected Longitude: %v", fix.Longitude)
			}
			if !fix.HasAlt || fix.Altitude != 1609.0 {
				t.Fatalf("Altitude not propagated: %+v", fix)
			}
			// 0.5 m/s == 0.972... knots
			if fix.Speed < 0.96 || fix.Speed > 0.98 {
				t.Fatalf("Speed (knots) out of range: %v", fix.Speed)
			}
			if fix.Heading != 142.0 || !fix.HasCourse {
				t.Fatalf("Heading not propagated: %+v", fix)
			}
			if fix.Timestamp.UnixMilli() != 1_700_000_000_000 {
				t.Fatalf("Timestamp wrong: %v", fix.Timestamp)
			}
			cancel()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for Fix to land in cache")
}

func TestRunAndroidGnssWritesSatelliteView(t *testing.T) {
	cli := &fakeGpsClient{gnssCh: make(chan *pb.GnssStatusUpdate, 1)}
	t.Cleanup(cli.close)
	satCache := NewMemCache() // MemCache implements SatelliteCache too
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := make(chan error, 1)
	go func() { errCh <- RunAndroidGnss(ctx, cli, satCache, logger) }()

	cli.gnssCh <- &pb.GnssStatusUpdate{
		SatsInView: 11,
		SatsUsed:   8,
		Sats: []*pb.SatInfo{
			{Svid: 5, Constellation: "GPS", Cn0Dbhz: 41.5, UsedInFix: true, ElevationDeg: 32, AzimuthDeg: 145},
			{Svid: 12, Constellation: "GLONASS", Cn0Dbhz: 38.2, UsedInFix: false, ElevationDeg: 11, AzimuthDeg: 220},
		},
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		view, ok := satCache.GetSatellites()
		if ok && len(view.Satellites) == 2 {
			if view.Satellites[0].PRN != 5 || view.Satellites[0].SNR != 41 {
				t.Fatalf("first sat wrong: %+v", view.Satellites[0])
			}
			cancel()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for SatelliteView")
}
```

(`MemCache.NewMemCache()` is the existing constructor in `pkg/gps/cache.go`. The test casts `MemCache` to `SatelliteCache` per the `pkg/gps` interfaces — confirm `MemCache.UpdateSatellites` exists; if not, the SatelliteCache cache lives elsewhere and the test target needs adjusting.)

- [ ] **Step 2: Run the test to verify it fails**

```bash
GOOS=android GOARCH=arm64 CGO_ENABLED=0 GOWORK=off go test ./pkg/gps/...
```

Expected: FAIL — `RunAndroid` and `RunAndroidGnss` undefined.

- [ ] **Step 3: Write the implementation**

Create `pkg/gps/android.go`:

```go
//go:build android

package gps

import (
	"context"
	"log/slog"
	"time"

	"github.com/chrissnell/graywolf/pkg/platformsvc"
)

// mpsToKnots converts metres per second (Android Location.speed unit)
// to knots (gps.Fix canonical unit, matches APRS / NMEA RMC).
const mpsToKnots = 1.94384449

// AndroidPlatformGpsClient is the subset of platformsvc.Client that the
// android GPS reader needs. Declared as a small interface so unit tests
// can inject a fake without dragging in the whole client surface.
type AndroidPlatformGpsClient interface {
	SubscribeGpsFix(ctx context.Context, ch chan<- *platformsvc.GpsFix) error
	SubscribeGnssStatus(ctx context.Context, ch chan<- *platformsvc.GnssStatusUpdate) error
}

// RunAndroid subscribes to GpsFix events from the platformsvc client
// and pushes each into the provided PositionCache. Returns when ctx is
// cancelled. It does not return on a single subscriber-channel hiccup;
// the platformsvc client manages reconnects upstream.
func RunAndroid(ctx context.Context, cli AndroidPlatformGpsClient, cache PositionCache, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	ch := make(chan *platformsvc.GpsFix, 8)
	if err := cli.SubscribeGpsFix(ctx, ch); err != nil {
		return err
	}
	logger.Info("gps android reader started")
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case pbfix := <-ch:
			if pbfix == nil {
				continue
			}
			cache.Update(toFix(pbfix))
		}
	}
}

// RunAndroidGnss subscribes to per-sat GnssStatusUpdate events and
// pushes each into the SatelliteCache. Pairs with RunAndroid.
func RunAndroidGnss(ctx context.Context, cli AndroidPlatformGpsClient, satCache SatelliteCache, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	ch := make(chan *platformsvc.GnssStatusUpdate, 4)
	if err := cli.SubscribeGnssStatus(ctx, ch); err != nil {
		return err
	}
	logger.Info("gps android gnss-status reader started")
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case st := <-ch:
			if st == nil {
				continue
			}
			satCache.UpdateSatellites(toSatelliteView(st))
		}
	}
}

func toFix(pb *platformsvc.GpsFix) Fix {
	// Read the proto presence bits directly. Earlier drafts derived
	// HasAlt from "AltM != 0" but legitimate zero values exist
	// (sea-level altitude, due-north course, stationary speed) and
	// derived presence silently dropped them; the proto carries
	// has_alt / has_speed / has_course explicitly.
	return Fix{
		Latitude:  pb.GetLat(),
		Longitude: pb.GetLon(),
		Altitude:  pb.GetAltM(),
		HasAlt:    pb.GetHasAlt(),
		// Android delivers speed in m/s; gps.Fix carries knots.
		Speed:     pb.GetSpeedMps() * mpsToKnots,
		Heading:   pb.GetCourseDeg(),
		HasCourse: pb.GetHasCourse(),
		Timestamp: time.UnixMilli(pb.GetTimeUnixMs()).UTC(),
		FixMode:   3, // Android only delivers Locations after a real fix.
	}
}

func toSatelliteView(st *platformsvc.GnssStatusUpdate) SatelliteView {
	out := SatelliteView{
		UpdatedAt:  time.Now().UTC(),
		Satellites: make([]SatelliteInfo, 0, len(st.GetSats())),
	}
	for _, s := range st.GetSats() {
		// Cn0Dbhz is float64 (e.g. 41.5 dB-Hz) but SatelliteInfo.SNR is
		// int — the truncated dB-Hz is plenty of resolution for the bar
		// display. If a future polar plot needs half-dB precision,
		// widen SatelliteInfo.SNR to float32 in a follow-up.
		out.Satellites = append(out.Satellites, SatelliteInfo{
			PRN:       int(s.GetSvid()),
			Elevation: int(s.GetElevationDeg()),
			Azimuth:   int(s.GetAzimuthDeg()),
			SNR:       int(s.GetCn0Dbhz()),
		})
	}
	return out
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
GOOS=android GOARCH=arm64 CGO_ENABLED=0 GOWORK=off go test ./pkg/gps/...
```

Expected: PASS for both new tests.

(If `MemCache` doesn't implement `SatelliteCache`, locate the satellite cache type via `grep -rn "UpdateSatellites" pkg/` and update the test to use that constructor instead. Don't introduce a new cache type just to make the test work.)

- [ ] **Step 5: Commit**

```bash
git add pkg/gps/android.go pkg/gps/android_test.go
git commit -m "feat(gps): RunAndroid + RunAndroidGnss readers (build-tagged)

Android-only reader pumps GpsFix events from the platformsvc client into
the existing gps.PositionCache; the gnss companion writes per-sat
detail into the SatelliteCache. Same shape as RunSerial / RunGPSD so
it slots into the existing gpsManager dispatch."
git push
```

---

## Task 13: Wire the Android reader into `pkg/app` boot — `gpsManager` android branch

The existing `gpsManager.start()` switches on `gpsCfg.SourceType` from configstore (`serial` | `gpsd`). On Android we don't want operators to "configure GPS" — the reader auto-starts when the platformsvc client is connected. Strategy: build-tagged hook so desktop builds compile cleanly and Android builds dispatch to the new reader.

**Files:**
- Modify: `pkg/app/gpsmanager.go`
- Create: `pkg/app/gpsmanager_android.go`
- Create: `pkg/app/gpsmanager_default.go`
- Create: `pkg/app/platformsvc_android.go` (Android-tagged `App.platformClient` field + alias)
- Create: `pkg/app/platformsvc_default.go` (no-op desktop sibling)
- Modify: `pkg/app/app.go` only for the desktop-safe `gpsCache` / `satelliteCache` wiring; the `platformClient` field lives in the build-tagged file so desktop builds carry no `interface{}` foot-gun.

- [ ] **Step 1: Inspect the current `gpsmanager.go` switch**

```bash
sed -n '85,115p' pkg/app/gpsmanager.go
```

Confirm the switch on `gpsCfg.SourceType` shape — Task 13 keeps it intact.

- [ ] **Step 2: Add the build-tagged hook (default no-op)**

Create `pkg/app/gpsmanager_default.go`:

```go
//go:build !android

package app

import (
	"context"
	"log/slog"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/gps"
)

// platformGpsRunner returns a non-nil run func when the host platform
// owns a built-in GPS source (Android). Desktop builds always return
// nil so the gpsManager falls through to its configstore-driven switch.
func platformGpsRunner(_ *App, _ *configstore.GPSConfig, _ *slog.Logger, _ func(string)) (gpsRunFunc, string) {
	return nil, ""
}

// platformGnssRunner mirrors platformGpsRunner for the per-sat companion.
func platformGnssRunner(_ *App, _ *slog.Logger) func(ctx context.Context) error {
	return nil
}

// platformGpsAlwaysOn reports whether the host platform supplies an
// always-on GPS source that should run regardless of configstore-side
// "Enabled" toggle. Android: true; desktop: false (operator opts in).
const platformGpsAlwaysOn = false

var _ = gps.RunSerial // keep imports honest
```

- [ ] **Step 3: Add the Android hook**

Create `pkg/app/gpsmanager_android.go`:

```go
//go:build android

package app

import (
	"context"
	"log/slog"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/gps"
)

func platformGpsRunner(a *App, _ *configstore.GPSConfig, logger *slog.Logger, _ func(string)) (gpsRunFunc, string) {
	cli := a.platformClient
	if cli == nil {
		logger.Warn("gps: platformsvc client not available; android reader disabled")
		return nil, ""
	}
	return func(ctx context.Context) error {
		return gps.RunAndroid(ctx, cli, a.gpsCache, logger)
	}, "gps android reader"
}

func platformGnssRunner(a *App, logger *slog.Logger) func(ctx context.Context) error {
	cli := a.platformClient
	if cli == nil {
		return nil
	}
	return func(ctx context.Context) error {
		return gps.RunAndroidGnss(ctx, cli, a.satelliteCache, logger)
	}
}

const platformGpsAlwaysOn = true
```

- [ ] **Step 4: Patch `gpsmanager.go` to consult the platform hook first**

In `pkg/app/gpsmanager.go`, replace the current `start` body's early-return:

```go
	if err != nil || gpsCfg == nil || !gpsCfg.Enabled {
		m.logger.Info("gps reader disabled")
		return
	}
```

with:

```go
	if !platformGpsAlwaysOn {
		if err != nil || gpsCfg == nil || !gpsCfg.Enabled {
			m.logger.Info("gps reader disabled")
			return
		}
	}
```

Then immediately after the existing `var run gpsRunFunc; var name string` declarations and before the `switch gpsCfg.SourceType` block, prepend:

```go
	if pf, pname := platformGpsRunner(m.app, gpsCfg, m.logger, onParseError); pf != nil {
		run = pf
		name = pname
	} else if platformGpsAlwaysOn {
		// Always-on platform (Android) but the runner couldn't be built —
		// e.g. platformsvc client is nil because GRAYWOLF_PLATFORM_SOCKET
		// was unset or Hello failed. Falling through to the configstore
		// switch would try to open /dev/ttyUSB0 (or whatever the operator
		// last configured on a desktop) which is wrong on a phone. Log
		// loudly and bail.
		m.logger.Warn("gps: always-on platform reader unavailable; not falling back to serial/gpsd")
		return
	} else {
		switch gpsCfg.SourceType {
		// ... existing cases unchanged ...
		}
	}
```

(Wrap the existing switch in the `else` clause so desktop behavior is unchanged.)

This needs `m.app *App` access; if `gpsManager` doesn't hold a back-reference today, add one. Update `newGPSManager` signature:

```go
func newGPSManager(app *App, store *configstore.Store, cache *gps.MemCache, logger *slog.Logger, m *metrics.Metrics) *gpsManager {
	return &gpsManager{app: app, store: store, cache: cache, logger: logger, m: m}
}
```

And add the `app *App` field to the struct. Update the single existing call site (search with `grep -n newGPSManager pkg/app/`) to pass `a`.

- [ ] **Step 5: Add `App.satelliteCache` / `App.gpsCache`; build-tag `App.platformClient`**

In `pkg/app/app.go`, add the desktop-safe fields to the `App` struct (somewhere near the existing `gpsMgr *gpsManager` field):

```go
	satelliteCache gps.SatelliteCache
	gpsCache       *gps.MemCache
```

The `platformClient` field is declared in a build-tagged sibling file so it carries the real `platformsvc.Client` interface on Android and is *absent* on desktop — typing it as `interface{}` on desktop would silently accept any future buggy assignment, so the cleanest cross-compile shape is a build-tagged extension struct.

`pkg/app/platformsvc_android.go`:

```go
//go:build android

package app

import "github.com/chrissnell/graywolf/pkg/platformsvc"

// On Android, App carries a live platformsvc client used by the GPS
// reader (phase 4a), the PTT relay (phase 4b), and other Service-side
// notifications. The field lives in this build-tagged file so desktop
// builds carry no platformsvc dependency at all.
func (a *App) PlatformClient() platformsvc.Client { return a.platformClient }

// platformClient is added to App via this build-tagged struct embed
// (declared below) so the field exists only on Android.
type appAndroidExt struct {
	platformClient platformsvc.Client
}
```

`pkg/app/platformsvc_default.go`:

```go
//go:build !android

package app

// Desktop App carries no platformsvc client. This empty struct keeps
// the embed point in App's definition build-clean across both targets.
type appAndroidExt struct{}
```

Embed `appAndroidExt` in `App` (in `app.go`):

```go
type App struct {
	// ... existing fields ...
	appAndroidExt // platformClient lives here on Android, empty on desktop
}
```

Then `gpsmanager_android.go` reads `a.platformClient` (resolves through the embed) and desktop builds never see the field name at all.

- [ ] **Step 6: Spawn `platformGnssRunner` alongside the GPS reader**

The GPS reader writes lat/lon into `gpsCache`; the per-sat C/N0 stream lives in `satelliteCache` and powers the bar display in Task 18 plus the `gnss_status` block in `/api/gps/state`. After `gpsManager.start()` launches its primary `run` goroutine, add a second guarded launch immediately after:

```go
	if gnssRun := platformGnssRunner(m.app, m.logger); gnssRun != nil {
		go func() {
			if err := gnssRun(m.runCtx); err != nil && !errors.Is(err, context.Canceled) {
				m.logger.Warn("gps gnss reader exited", "err", err)
			}
		}()
	}
```

(If `gpsManager` doesn't already store a `runCtx` and the existing GPS goroutine reads its context from a different name, mirror that — don't introduce a new lifetime surface.)

- [ ] **Step 7: Verify `/api/gps/state` serializer emits `gnss_status`**

```bash
grep -rn "gpsState\|GPSState\|/api/gps/state\|gnss_status\|SatelliteView" pkg/server/ pkg/app/ pkg/gps/ 2>/dev/null
```

If the existing handler reads from `gps.PositionCache` only, extend it to also pull `SatelliteCache.GetSatellites()` and emit a `gnss_status` JSON block matching the proto's field names (snake_case via the existing JSON tags). If the handler already aggregates both, no work to do — note that and move on.

The Android GPS page (Task 18) reads `s.gnss_status` directly, so this serialization is on the critical path; do not skip.

- [ ] **Step 8: Compile both targets**

```bash
go build ./...
GOOS=android GOARCH=arm64 CGO_ENABLED=0 GOWORK=off go build ./...
```

Expected: both clean.

- [ ] **Step 9: Run desktop tests to confirm no regression**

```bash
go test ./pkg/app/...
```

Expected: existing gpsmanager tests still pass.

- [ ] **Step 10: Commit**

```bash
git add pkg/app/gpsmanager.go pkg/app/gpsmanager_android.go pkg/app/gpsmanager_default.go pkg/app/platformsvc_android.go pkg/app/platformsvc_default.go pkg/app/app.go
# also include the /api/gps/state handler if step 7 required edits
git commit -m "feat(app): wire android GPS + GNSS readers into gpsManager via build-tagged hook

When platformGpsRunner returns a non-nil run func (Android), the
manager dispatches to it regardless of configstore-side Enabled and
also spawns platformGnssRunner so per-sat C/N0 reaches the satellite
cache. When the platform is always-on but the runner is unavailable
(e.g. platformsvc client nil), gpsManager logs and returns rather than
falling back to serial/gpsd. Desktop builds keep the existing
configstore-driven serial/gpsd switch behavior."
git push
```

---

## Task 14: Real `cmd/graywolf/main_android.go` entrypoint — connect platformsvc, run app

The current android entrypoint is a no-op stub. Replace it with one that boots the Go app, connects the platformsvc client to the env-supplied UDS, and starts the GPS + GNSS readers as named components.

**Files:**
- Modify: `cmd/graywolf/main_android.go`

- [ ] **Step 1: Inspect the existing main.go for the desktop boot shape**

```bash
sed -n '1,100p' cmd/graywolf/main.go
```

Use this as the reference for log setup, ParseFlags, ctx, signals.

- [ ] **Step 2: Rewrite `cmd/graywolf/main_android.go`**

```go
//go:build android

// Android entrypoint. Boots the same pkg/app.App graph as desktop, but
// also connects the platformsvc UDS client (talking to the Kotlin
// GraywolfService) and starts the android GPS + GNSS readers.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/chrissnell/graywolf/pkg/app"
	"github.com/chrissnell/graywolf/pkg/platformsvc"
)

var (
	Version   = "dev"
	GitCommit = "unknown"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := app.ParseFlags(os.Args[1:])
	if err != nil {
		logger.Error("ParseFlags failed", "err", err)
		os.Exit(2)
	}
	cfg.Version = Version
	cfg.GitCommit = GitCommit

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	a := app.New(cfg, logger)

	// Resolve the platformsvc UDS path from the env the Kotlin side
	// exports (GraywolfService.bootGoChild). The "@" prefix is the
	// abstract-namespace marker that Linux net.Dialer accepts.
	socketPath := os.Getenv("GRAYWOLF_PLATFORM_SOCKET")
	if socketPath == "" {
		logger.Warn("GRAYWOLF_PLATFORM_SOCKET unset; android GPS readers disabled")
	} else {
		cli := platformsvc.NewClient(socketPath)
		if err := cli.ConnectWithReconnect(ctx); err != nil {
			logger.Error("platformsvc dial failed", "err", err)
		} else {
			if _, err := cli.Hello(ctx, platformsvc.SchemaVersion); err != nil {
				logger.Error("platformsvc Hello failed", "err", err)
			}
			a.SetPlatformClient(cli)
		}
	}

	if err := a.Run(ctx); err != nil {
		logger.Error("graywolf exited with error", "err", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 3: Add `App.SetPlatformClient` (build-tagged accessor)**

In `pkg/app/app.go` or a new `pkg/app/platformsvc_setter_android.go`:

```go
//go:build android

package app

// SetPlatformClient injects the platformsvc client. Must be called
// before Run; gpsManager.start() reads it through platformGpsRunner.
func (a *App) SetPlatformClient(c platformsvcClient) { a.platformClient = c }
```

- [ ] **Step 4: Cross-compile**

```bash
GOOS=android GOARCH=arm64 CGO_ENABLED=0 GOWORK=off go build ./...
```

Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add cmd/graywolf/main_android.go pkg/app/platformsvc_setter_android.go
git commit -m "feat(android-cmd): real main_android.go that connects platformsvc

Boots the same app.New(...).Run(ctx) graph as desktop and wires the
platformsvc client (UDS path from \$GRAYWOLF_PLATFORM_SOCKET) into the
App before Run. The android GPS reader picks up the client through
gpsmanager_android.go's platformGpsRunner."
git push
```

---

## Task 15: Wire `GpsAdapter` into `GraywolfService` lifecycle

**Files:**
- Modify: `android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt`

- [ ] **Step 1: Add the field + import**

In `GraywolfService.kt`:

```kotlin
import com.nw5w.graywolf.gps.GpsAdapter
```

Add the field next to the others:

```kotlin
    private var gpsAdapter: GpsAdapter? = null
```

- [ ] **Step 2: Start the adapter after PlatformServer.start in `onCreate`**

Locate the existing block:

```kotlin
        platformServer = PlatformServer(
            socketPath = platformSocketPath(),
            serverVersion = BuildConfig.VERSION_NAME,
            schemaVersion = 1,
        ).also { it.start() }
```

Append immediately after:

```kotlin
        gpsAdapter = GpsAdapter(this, platformServer!!).also { it.start() }
```

- [ ] **Step 3: Stop the adapter first in `onDestroy`**

Edit the existing `onDestroy` so the order is:

```kotlin
    override fun onDestroy() {
        supervisor.stop()
        gainPoller?.interrupt()
        gainPoller = null
        goListenerReady = false
        gpsAdapter?.stop()
        gpsAdapter = null
        goLauncher?.stop()
        audioPump.stop()
        platformServer?.stop()
        ModemBridge.modemStop()
        UsbPttAdapter.closeAll()
        super.onDestroy()
    }
```

(Adapter stops before the server it talks to.)

- [ ] **Step 4: Compile**

```bash
cd android && ./gradlew :app:compileDebugKotlin
```

Expected: BUILD SUCCESSFUL.

- [ ] **Step 5: Commit**

```bash
git add android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt
git commit -m "feat(android-service): start/stop GpsAdapter alongside PlatformServer

GpsAdapter starts after PlatformServer.start (it broadcasts through
the server) and stops before PlatformServer.stop. Permission gate is
inside GpsAdapter.start, so this hook is unconditional."
git push
```

---

## Task 16: AndroidManifest + MainActivity permissions

**Files:**
- Modify: `android/app/src/main/AndroidManifest.xml`
- Modify: `android/app/src/main/kotlin/com/nw5w/graywolf/MainActivity.kt`

- [ ] **Step 1: Add the permission and FGS type to the manifest**

In `AndroidManifest.xml`:

After `<uses-permission android:name="android.permission.RECORD_AUDIO"/>`, add:

```xml
    <uses-permission android:name="android.permission.ACCESS_FINE_LOCATION"/>
    <uses-permission android:name="android.permission.FOREGROUND_SERVICE_LOCATION"/>
```

And change the existing service declaration's `foregroundServiceType` from:

```xml
            android:foregroundServiceType="microphone"/>
```

to:

```xml
            android:foregroundServiceType="microphone|location"/>
```

- [ ] **Step 2: Update the foreground-service start call in `GraywolfService.kt` to declare both types**

In `onCreate`, find:

```kotlin
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(
                NOTIF_ID, notif,
                ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE
            )
```

Replace the `FOREGROUND_SERVICE_TYPE_MICROPHONE` flag with the bitwise-OR of both:

```kotlin
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(
                NOTIF_ID, notif,
                ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE or
                    ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION
            )
```

- [ ] **Step 3: Extend `ensurePerms` in `MainActivity.kt`**

Change the body to:

```kotlin
    private fun ensurePerms() {
        val needed = mutableListOf<String>()
        if (checkSelfPermission(Manifest.permission.RECORD_AUDIO) != PackageManager.PERMISSION_GRANTED) {
            needed += Manifest.permission.RECORD_AUDIO
        }
        if (checkSelfPermission(Manifest.permission.ACCESS_FINE_LOCATION) != PackageManager.PERMISSION_GRANTED) {
            needed += Manifest.permission.ACCESS_FINE_LOCATION
        }
        if (Build.VERSION.SDK_INT >= 33 &&
            checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            needed += Manifest.permission.POST_NOTIFICATIONS
        }
        if (needed.isNotEmpty()) {
            requestPermissions(needed.toTypedArray(), REQ_PERMS)
        } else {
            startEverything()
        }
    }
```

- [ ] **Step 4: Compile + run lint**

```bash
cd android && ./gradlew :app:compileDebugKotlin :app:lintDebug
```

Expected: BUILD SUCCESSFUL. Lint may emit informational warnings about `ACCESS_FINE_LOCATION`; surface and continue.

- [ ] **Step 5: Commit**

```bash
git add android/app/src/main/AndroidManifest.xml android/app/src/main/kotlin/com/nw5w/graywolf/MainActivity.kt android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt
git commit -m "feat(android): request ACCESS_FINE_LOCATION + FGS type=location

Manifest declares ACCESS_FINE_LOCATION and the matching
FOREGROUND_SERVICE_LOCATION; GraywolfService declares the location FGS
type alongside microphone; MainActivity.ensurePerms requests fine
location grouped with the existing audio/notification prompt."
git push
```

---

## Task 17: SPA UI gating sweep — Login, AGW, AudioDevices `device_path`, Sidebar

**Files:**
- Modify: `web/src/App.svelte`
- Modify: `web/src/components/Sidebar.svelte`
- Modify: `web/src/routes/AudioDevices.svelte`

Spec §5: permanent hides only.

(The spec's "LAN listen address/port" item is currently configured server-side only — `grep -rn "listen_address\|listenAddr\|listen_port" web/src/` returns no matches against `main` at the time this plan was written, so there is nothing to hide on the SPA side. If the plan-writer's grep above turns up a match introduced after this plan was authored, add a step that wraps the matching FormField in `{#if Platform.kind !== 'android'}` and update the file list below.)

- [ ] **Step 1: Hide AGW + Login from the sidebar on Android**

In `web/src/components/Sidebar.svelte`, change:

```js
  const HIDDEN_ON_ANDROID = new Set(['/ptt', '/simulation']);
```

to:

```js
  const HIDDEN_ON_ANDROID = new Set(['/ptt', '/simulation', '/agw', '/login']);
```

(Login isn't in the settings group today, but the constant doubles as a future safety net.)

- [ ] **Step 2: Skip Login mount on Android in `web/src/App.svelte`**

Add to the imports:

```js
  import { Platform } from './lib/platform.js';
  import { replace } from 'svelte-spa-router';
```

Replace the current `let isLoginPage = $derived(currentPath === '/login');` with:

```js
  let isLoginPage = $derived(currentPath === '/login' && Platform.kind !== 'android');
```

And add a redirect effect immediately after the existing currentPath subscribe:

```js
  $effect(() => {
    if (Platform.kind === 'android' && currentPath === '/login') {
      // replace() uses history.replaceState — no new history entry, so
      // pressing back doesn't loop the user through /login again. Direct
      // `window.location.hash = '#/'` would push, causing a visible
      // navigation ping-pong on Android's back button.
      replace('/');
    }
  });
```

- [ ] **Step 3: Strip the AGW route from `App.svelte`'s route map on Android**

Replace the current `routes` object construction with a build-once-conditional shape:

```js
  const baseRoutes = {
    '/login': Login,
    '/': Dashboard,
    '/map': LiveMapV2,
    '/messages': Messages,
    '/messages/*': Messages,
    '/terminal': Terminal,
    '/terminal/transcripts': TerminalTranscripts,
    '/actions': Actions,
    '/channels': Channels,
    '/audio-devices': AudioDevices,
    '/ptt': Ptt,
    '/kiss': Kiss,
    '/agw': Agw,
    '/igate': Igate,
    '/digipeater': Digipeater,
    '/beacons': Beacons,
    '/callsign': Callsign,
    '/gps': Gps,
    '/simulation': Simulation,
    '/position-log': PositionLog,
    '/logs': Logs,
    '/preferences': Preferences,
    '/preferences/maps': MapsSettings,
    '/about': About,
  };
  const routes = (() => {
    if (Platform.kind !== 'android') return baseRoutes;
    const r = { ...baseRoutes };
    delete r['/agw'];
    delete r['/login'];
    return r;
  })();
```

- [ ] **Step 4: Lock `device_path` on Android in `AudioDevices.svelte`**

Find the current Device Path FormField (around line 475):

```svelte
  <FormField label="Device Path" error={errors.device_path} id="ad-path">
    <Input id="ad-path" bind:value={form.device_path} placeholder="hw:0,0" />
  </FormField>
```

Replace with:

```svelte
  <FormField label="Device Path" error={errors.device_path} id="ad-path">
    {#if Platform.kind === 'android'}
      <Input id="ad-path" value="android-default" readonly />
    {:else}
      <Input id="ad-path" bind:value={form.device_path} placeholder="hw:0,0" />
    {/if}
  </FormField>
```

And in the `emptyForm()` function, change the default `device_path` so that on Android the form pre-fills properly:

```js
  function emptyForm() {
    return {
      name: '',
      device_path: Platform.kind === 'android' ? 'android-default' : '',
      sample_rate: '48000',
      source_type: 'soundcard',
      direction: 'input',
    };
  }
```

(Add `import { Platform } from '../lib/platform.js';` if not already present from Task 4.)

- [ ] **Step 5: Confirm `device_path` validator accepts the sentinel**

The current `validate()` in `AudioDevices.svelte` requires only `form.device_path.trim()` non-empty (`if (!form.device_path.trim()) e.device_path = 'Required'`). The literal `"android-default"` passes that check, so no validator special-case is needed. If a future tightening adds a `hw:N,N` regex check, it must allowlist the `android-default` sentinel — leave a one-line comment near the validator referencing this constraint.

- [ ] **Step 6: Run SPA tests**

```bash
cd web && npm test --silent
```

Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add web/src/App.svelte web/src/components/Sidebar.svelte web/src/routes/AudioDevices.svelte
git commit -m "feat(web): permanent-hide UI sweep for Android

Sidebar now hides AGW + Login alongside PTT/Simulation. App.svelte
strips the /agw and /login route entries on Android and uses
svelte-spa-router replace('/') for the /login redirect so the back
button can't loop. AudioDevices.svelte locks device_path to
'android-default' (the existing non-empty validator accepts it)."
git push
```

---

## Task 18: GPS page Android redesign — mission-control layout

**Files:**
- Modify: `web/src/routes/Gps.svelte` (split top-level so Android variant is a separate component)
- Create: `web/src/routes/GpsAndroid.svelte`
- Tests: smoke-test in `web/src/routes/GpsAndroid.test.js` (uses `svelte/server` SSR; no DOM dep)

The simplest split: keep `Gps.svelte` as the desktop layout untouched; in its `<script>`, branch on `Platform.kind` and render `<GpsAndroid />` instead.

- [ ] **Step 0: Confirm map-snippet scope with the user**

Spec §6.1 lists the static map view as part of phase 4a but the implementer (you) needs to know whether the user wants this phase shipped *with* the map snippet wired into `GpsAndroid.svelte`, or whether the bars-and-stats layout below ships first and the map lands as a follow-up. Stop and ask before writing any code in this task. Acceptable answers:

- **All-in:** add a `<MapTile />` (or the existing maplibre wrapper) snippet at the top of `GpsAndroid.svelte` rendering the current fix as a single marker, no zoom/pan controls. Add the work as additional steps.
- **Bars-first:** ship the layout below as-is and capture a follow-up issue for the map snippet.

Do not silently pick one; this is a load-bearing scope question.

- [ ] **Step 1: Identify the existing `gps_state` store path**

```bash
grep -rn "gps_state\|gpsState\|GpsState\|/api/gps" web/src/lib/ web/src/routes/Gps.svelte | head -20
```

Note the store/import path used; the new component will read from it directly.

- [ ] **Step 2: Create `GpsAndroid.svelte`**

```svelte
<script>
  import { onMount, onDestroy } from 'svelte';
  import { api } from '../lib/api.js';
  import PageHeader from '../components/PageHeader.svelte';

  // Mission-control layout for the Android GPS page.
  // Reads from the same /api/gps/state endpoint the desktop page uses;
  // poll every second so the operator gets near-live values without
  // adding a new SSE/WebSocket channel for this page alone. Polling is
  // paused when the page is hidden (screen off / app backgrounded) so
  // the device doesn't burn radio + CPU on values nobody is reading.
  let fix = $state(null);
  let gnss = $state(null);
  let lastUpdated = $state(null);
  let timer = null;

  async function poll() {
    try {
      const s = await api.get('/gps/state');
      fix = s.fix ?? null;
      gnss = s.gnss_status ?? null;
      lastUpdated = Date.now();
    } catch (_) { /* ignore — keep last values */ }
  }

  function startPolling() {
    if (timer) return;
    poll();
    timer = setInterval(poll, 1000);
  }
  function stopPolling() {
    if (timer) { clearInterval(timer); timer = null; }
  }
  function onVisibility() {
    if (document.visibilityState === 'hidden') stopPolling();
    else startPolling();
  }

  onMount(() => {
    startPolling();
    document.addEventListener('visibilitychange', onVisibility);
  });
  onDestroy(() => {
    stopPolling();
    document.removeEventListener('visibilitychange', onVisibility);
  });

  // Status pill derivation: lat/lon present and fix age < 5s → LOCKED;
  // recent fix object but no lat → SEARCHING; no fix object → NO FIX.
  const FIX_FRESH_MS = 5000;
  let status = $derived.by(() => {
    if (!fix) return { label: 'NO FIX', tone: 'bad' };
    const age = lastUpdated ? Date.now() - lastUpdated : Infinity;
    if (fix.lat != null && fix.lon != null && age < FIX_FRESH_MS) {
      return { label: 'FIX LOCKED', tone: 'good' };
    }
    return { label: 'SEARCHING', tone: 'warn' };
  });

  function fmtAccuracy(m) {
    if (m == null || m === 0) return '—';
    return `${m.toFixed(1)} m`;
  }
  function fmtAgeSec(ms) {
    if (ms == null) return '—';
    return `${Math.max(0, Math.floor((Date.now() - ms) / 1000))}s ago`;
  }
  function satLabel(s) {
    const cn0 = s.cn0_dbhz?.toFixed(1) ?? '—';
    return `SVID ${s.svid} (${s.constellation}), C/N0 ${cn0} dBHz, used in fix`;
  }
</script>

<PageHeader title="GPS" subtitle="Status, satellites, position" />

<!-- role=status makes the pill a polite live region: AT users hear
     "FIX LOCKED" when the state transitions, without having to navigate
     to it. Tone is purely decorative; the label carries the meaning. -->
<section class="status-row" data-tone={status.tone} role="status" aria-live="polite">
  <div class="status-pill">{status.label}</div>
  <div class="status-meta">
    accuracy {fmtAccuracy(fix?.accuracy_m)} · updated {fmtAgeSec(lastUpdated)}
  </div>
</section>

<section class="latlon" aria-label="Position">
  <div><label>LATITUDE</label><span>{fix?.lat?.toFixed(5) ?? '—'}</span></div>
  <div><label>LONGITUDE</label><span>{fix?.lon?.toFixed(5) ?? '—'}</span></div>
</section>

<section class="trio" aria-label="Movement">
  <div><label>ALT</label><span>{fix?.alt_m != null ? `${fix.alt_m.toFixed(0)} m` : '—'}</span></div>
  <div><label>SPEED</label><span>{fix?.speed_mps != null ? `${fix.speed_mps.toFixed(1)} m/s` : '—'}</span></div>
  <div><label>COURSE</label><span>{fix?.course_deg != null ? `${fix.course_deg.toFixed(0)}°` : '—'}</span></div>
</section>

<section class="sats" aria-labelledby="sats-heading">
  <h2 id="sats-heading">
    SATS USED IN FIX ({gnss?.sats_used ?? 0} / {gnss?.sats_in_view ?? 0} in view)
  </h2>
  <div class="bars" role="group" aria-label="Per-satellite C/N0">
    {#each (gnss?.sats ?? []).filter(s => s.used_in_fix) as s}
      <div
        class="bar"
        role="img"
        tabindex="0"
        aria-label={satLabel(s)}
        title={satLabel(s)}
        style:height={`${Math.min(100, (s.cn0_dbhz ?? 0) * 2)}%`}
      ></div>
    {/each}
  </div>
</section>

<style>
  .status-row { display: flex; align-items: center; gap: 1rem; margin-bottom: 1rem; }
  .status-pill { padding: 0.25rem 0.75rem; border-radius: 999px; font-weight: 600; color: white; }
  .status-row[data-tone="good"] .status-pill { background: var(--color-success); }
  .status-row[data-tone="warn"] .status-pill { background: var(--color-warning); }
  .status-row[data-tone="bad"]  .status-pill { background: var(--color-danger); }
  .latlon { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; margin-bottom: 1rem; }
  .latlon label, .trio label { display: block; font-size: 0.75rem; opacity: 0.6; text-transform: uppercase; letter-spacing: 0.05em; }
  .latlon span, .trio span { font-size: 1.5rem; font-family: var(--font-mono); }
  .trio { display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 1rem; margin-bottom: 1rem; }
  .sats h2 { font-size: 0.75rem; opacity: 0.6; text-transform: uppercase; letter-spacing: 0.05em; font-weight: 600; margin: 0 0 0.5rem; }
  .sats .bars {
    display: flex;
    align-items: flex-end;
    gap: 4px;
    height: 80px;
    padding: 8px 0;
    overflow-x: auto;        /* horizontally scroll past the viewport when sat count is high */
    scroll-snap-type: x proximity;
  }
  .sats .bar {
    width: 12px;
    min-height: 4px;
    background: var(--color-primary);
    border-radius: 2px 2px 0 0;
    flex-shrink: 0;
    scroll-snap-align: start;
    cursor: help;
    outline-offset: 2px;     /* don't clip the focus ring against the bar */
  }
  .sats .bar:focus { outline: 2px solid var(--color-primary); }
  /* Narrow viewports: thinner bars so a typical 10-sat fix doesn't scroll. */
  @media (max-width: 480px) {
    .sats .bar { width: 8px; }
    .sats .bars { gap: 3px; }
  }
  /* Landscape on a phone — promote position to one row. */
  @media (orientation: landscape) and (max-height: 480px) {
    .latlon { grid-template-columns: 1fr 1fr 1fr 1fr; }
    .latlon { /* placeholder so future fields slot in cleanly */ }
  }
</style>
```

- [ ] **Step 3: Branch in `Gps.svelte`**

In `web/src/routes/Gps.svelte` add at the top of the script:

```js
  import { Platform } from '../lib/platform.js';
  import GpsAndroid from './GpsAndroid.svelte';
```

And at the very top of the markup (before the existing `<script>`-driven layout):

```svelte
{#if Platform.kind === 'android'}
  <GpsAndroid />
{:else}
  <!-- existing desktop layout — no other changes -->
{/if}
```

Wrap the entire current desktop page contents in the `{:else}` branch.

- [ ] **Step 4: Smoke test via `svelte/server` SSR — no DOM harness needed**

The project's `npm test` script runs `node --test 'src/**/*.test.js'` against pure JS — there is no jsdom or vitest environment. Rather than bolt one on for a single component test, render `GpsAndroid` to an HTML string with `svelte/server`'s `render()` and assert against the output. This proves the component compiles cleanly and produces the expected layout skeleton (the value an "import-only" sentinel cannot give).

Create `web/src/routes/GpsAndroid.test.js`:

```js
import { test, beforeEach, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import { render } from 'svelte/server';
import { _resetForTests as resetBridge } from '../lib/androidBridge.js';
import GpsAndroid from './GpsAndroid.svelte';

beforeEach(() => {
  resetBridge();
  globalThis.GraywolfWebInterface = { getBearerToken: () => 'tok' };
});
afterEach(() => {
  resetBridge();
  delete globalThis.GraywolfWebInterface;
});

test('GpsAndroid SSR renders the layout skeleton when /api/gps/state is empty', () => {
  const { body } = render(GpsAndroid);
  assert.match(body, /LATITUDE/);
  assert.match(body, /LONGITUDE/);
  assert.match(body, /SATS USED IN FIX/);
  // The "no fix" branch should produce the bad-tone status pill label.
  assert.match(body, /NO FIX/);
});
```

(If `node --test` complains about the `.svelte` import not resolving, the project's existing test path already runs `.svelte` imports through the bundler/loader; match that. If it does not, add a `--loader` flag to the npm test script that delegates `.svelte` to the existing build pipeline. Do *not* add a runtime DOM harness — `svelte/server` is the lighter touch.)

- [ ] **Step 5: Run SPA tests**

```bash
cd web && npm test --silent
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add web/src/routes/GpsAndroid.svelte web/src/routes/Gps.svelte web/src/routes/GpsAndroid.test.js
git commit -m "feat(web-gps): mission-control layout for Android

GpsAndroid.svelte renders status pill (role=status live region, design
tokens for tone), lat/lon block, alt/speed/course trio, and used-in-fix
C/N0 bars from /api/gps/state poll. Polling pauses on
visibilitychange=hidden so the page doesn't burn battery while the
screen is off. Bars overflow-x scroll on narrow viewports and expose
per-sat detail via aria-label and focusable tabindex. Desktop Gps.svelte
is untouched and renders the existing serial/gpsd layout when
Platform.kind !== 'android'. Map-snippet decision (all-in vs follow-up)
captured in Task 18 Step 0 — see commit body for which path was taken."
git push
```

---

## Task 19: App icon — adaptive launcher + notification monochrome silhouette

**Files:**
- Modify: `android/app/src/main/res/values/colors.xml` (background flips dark → white)
- Modify: `android/app/src/main/res/drawable/ic_launcher_foreground.xml` (replace placeholder vector with the wolf logo trace)
- Create: `android/app/src/main/res/drawable/ic_notification.xml` (white silhouette, simplified)
- Modify: `android/app/src/main/AndroidManifest.xml` (drop `roundIcon` reference if any; adaptive xml handles round masks)
- Modify: `android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt` (use `ic_notification` as small-icon)
- Source asset: `web/public/favicon.svg` (already in repo — wolf logo)

(Project `minSdk = 28` and the FGS-type-location requirement bumps that floor regardless. The adaptive icon XML in `mipmap-anydpi-v26/` covers every device the app actually runs on, so legacy density `mipmap-{mdpi,hdpi,xhdpi,xxhdpi,xxxhdpi}/ic_launcher.png` PNGs are deliberately *not* generated — they only matter on API ≤ 25, which the app doesn't support. Removing the legacy raster path keeps the icon source-of-truth as the single SVG → vector pipeline, eliminates the rasterization-aliasing concern at mdpi, and shrinks APK weight slightly.)

- [ ] **Step 1: Generate the foreground vector**

The adaptive icon foreground sits in a 108×108 dp canvas with a 72×72 dp safe zone (centered). The simplest deterministic path: open Android Studio → "New > Vector Asset", source = `web/public/favicon.svg`, size = 108×108 dp, name = `ic_launcher_foreground`. The tool produces an Android-compatible `<vector>` XML with the path data inlined. Replace the existing placeholder content of `android/app/src/main/res/drawable/ic_launcher_foreground.xml` with the generated XML.

If Android Studio is not available, use ImageMagick / Inkscape to render `favicon.svg` to a 108×108 px PNG, then run `vd-tool -c -in foreground.png -out drawable/`. Either path is acceptable; the deliverable is `ic_launcher_foreground.xml` with the wolf silhouette filled in `#0A0A0A` (dark, against the new white background).

- [ ] **Step 2: Flip background to white**

Edit `android/app/src/main/res/values/colors.xml`:

```xml
<?xml version="1.0" encoding="utf-8"?>
<resources>
    <color name="ic_launcher_background">#FFFFFFFF</color>
</resources>
```

(was `#0A0A0A`).

- [ ] **Step 3: Verify `mipmap-anydpi-v26/ic_launcher.xml` references both resources**

The current contents already are correct:

```xml
<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">
    <background android:drawable="@color/ic_launcher_background"/>
    <foreground android:drawable="@drawable/ic_launcher_foreground"/>
</adaptive-icon>
```

If `ic_launcher_round.xml` exists in the same directory and references different resources, replace its contents with the same two lines so the round mask uses the same adaptive layers.

- [ ] **Step 4: Remove any leftover legacy mipmap PNGs**

If a previous icon iteration left `mipmap-{mdpi,hdpi,xhdpi,xxhdpi,xxxhdpi}/ic_launcher.png` (or round variants) in the tree, delete them:

```bash
find android/app/src/main/res -path '*/mipmap-mdpi/ic_launcher*.png' \
  -o -path '*/mipmap-hdpi/ic_launcher*.png' \
  -o -path '*/mipmap-xhdpi/ic_launcher*.png' \
  -o -path '*/mipmap-xxhdpi/ic_launcher*.png' \
  -o -path '*/mipmap-xxxhdpi/ic_launcher*.png' \
  | xargs -r rm -v
```

Then drop any `<application android:roundIcon="@mipmap/ic_launcher_round"/>` reference in `AndroidManifest.xml` if it points at a now-absent legacy PNG. The adaptive `mipmap-anydpi-v26/ic_launcher.xml` plus the foreground vector cover every supported device.

- [ ] **Step 5: Notification small-icon vector — required simplified silhouette**

Status-bar icons render at 24×24 dp on Android. A 108 dp adaptive-launcher path data, scaled into a 24 dp viewport, renders as an unreadable blob — and this icon is shown every minute the foreground-service notification is up, so it is the most-seen icon in the entire app. The simplified silhouette is therefore *required*, not a nice-to-have; do not ship the launcher-path-data fallback.

Generate properly via Android Studio: **New > Image Asset > Notification Icons**, source `web/public/favicon.svg`, "Trim" enabled. The wizard threshold-renders the SVG into a clean monochrome 24 dp vector path. Save the result as `android/app/src/main/res/drawable/ic_notification.xml` — a single path, `android:fillColor="#FFFFFFFF"` (the OS recolors monochrome status-bar icons by system theme).

If Android Studio is unavailable, an equivalent CLI path: render `favicon.svg` to a 24×24 px PNG with `rsvg-convert`, threshold at 50 % luminance with ImageMagick (`convert in.png -threshold 50% out.png`), then trace to vector with `vd-tool -c -in out.png -out drawable/ic_notification.xml`. Visually inspect the result against an actual status-bar render before committing — verify the wolf silhouette is recognizable at 24 dp.

- [ ] **Step 6: Use it as the foreground-service notification small-icon**

In `GraywolfService.kt`, find:

```kotlin
            .setSmallIcon(android.R.drawable.ic_media_play)
```

Replace with:

```kotlin
            .setSmallIcon(R.drawable.ic_notification)
```

- [ ] **Step 7: Build the APK and visually inspect**

```bash
cd android && ./gradlew clean :app:assembleDebug
```

Expected: BUILD SUCCESSFUL. Pull the APK off `android/app/build/outputs/apk/debug/` and unzip to confirm `res/mipmap-*` contain the new PNGs and `res/drawable/ic_launcher_foreground.xml` is the wolf-trace, not the placeholder chevron.

- [ ] **Step 8: Commit**

```bash
git add android/app/src/main/res/
git add android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt
git commit -m "feat(android): graywolf-branded launcher and notification icons

Adaptive icon: white background, dark wolf foreground vector. Legacy
density mipmap PNGs are intentionally absent — minSdk=28 plus the
FGS-type-location floor mean the adaptive XML in mipmap-anydpi-v26
covers every supported device. Status-bar small-icon switches from
android.R.drawable.ic_media_play to a properly threshold'd 24 dp
monochrome silhouette in drawable/ic_notification.xml."
git push
```

---

## Task 20: Live-device validation on the T865

The build succeeds and unit tests pass — but Android-side behavior (`LocationManager`, FGS-type-location enforcement, icon rendering, permission flow) only really proves out on a real device. Run this checklist on a T865 with the operator nearby for a clear sky during the GPS portion.

- [ ] **Step 1: Install the debug APK**

```bash
cd android && ./gradlew :app:installDebug
adb shell am force-stop com.nw5w.graywolf || true
adb shell am start -n com.nw5w.graywolf/.MainActivity
```

- [ ] **Step 2: Verify the launcher icon shows the new wolf logo**

On the home screen / app drawer: confirm the icon is dark wolf on white, not the AGP placeholder. Take a screenshot for the PR.

- [ ] **Step 3: Verify the runtime permission prompt includes location**

First launch on a fresh install should prompt for **Microphone**, **Location (precise)**, **Notifications** in a single grouped dialog. Grant all three.

- [ ] **Step 4: Verify the foreground-service notification uses `ic_notification`**

Pull down the notification shade — the graywolf entry's small icon should be the white silhouette, not the system play-arrow.

- [ ] **Step 5: Confirm GpsAdapter starts within ~1s of Service onCreate**

```bash
adb logcat -s GpsAdapter:* GraywolfService:* PlatformServer:*
```

Expected within 1s of launch:

```
I GpsAdapter: GpsAdapter started: GPS_PROVIDER 10s/0m + GNSS status callback
I PlatformServer: PlatformServer bound at /data/.../platform.sock
```

- [ ] **Step 6: Walk outside; confirm the first fix arrives within 30s**

Filter for `GpsAdapter` and the Go-side `gps android reader started`:

```bash
adb logcat | grep -E "GpsAdapter|gps android"
```

Expected: a `GpsFix` is broadcast and the Go side logs `gps android reader started` then begins updating its cache. Within ~30s of cold start, the SPA dashboard's position widget should populate.

- [ ] **Step 7: Verify the Android GPS page renders the mission-control layout**

Navigate to `#/gps`. Expected layout: status pill (FIX LOCKED in green), lat/lon, alt/speed/course trio, used-in-fix C/N0 bars. Confirm sat counts match what the OS GPS-status app reports.

- [ ] **Step 8: Verify UI gating sweep**

- Sidebar: no AGW entry, no Login entry, PTT and Simulation hidden as before.
- AudioDevices add-form: device path shows "android-default" and is read-only.
- Navigating to `#/login` directly should redirect to `#/`.
- Settings: no LAN listen address/port field visible.

- [ ] **Step 9: Force-stop + relaunch — verify warm-start fix re-acquisition**

```bash
adb shell am force-stop com.nw5w.graywolf
adb shell am start -n com.nw5w.graywolf/.MainActivity
```

Expected: fix re-acquires within ~5s (warm-start almanac). Beacon scheduler logs `pkg/beacon: scheduled` lines once a fix is in cache.

- [ ] **Step 10: Revoke FINE_LOCATION via system settings**

Settings → Apps → graywolf → Permissions → Location → Don't allow. Then `adb shell am force-stop com.nw5w.graywolf && adb shell am start -n com.nw5w.graywolf/.MainActivity`.

Expected log:

```
I GpsAdapter: GpsAdapter.start skipped — ACCESS_FINE_LOCATION not granted
```

No crash, service stays up, GPS page shows "NO FIX" status.

- [ ] **Step 11: Final sweep — go-build / cross-build / gradle**

```bash
go test -race ./...
GOOS=android GOARCH=arm64 CGO_ENABLED=0 GOWORK=off go build ./...
cd web && npm test --silent && npm run build
cd ../android && ./gradlew clean :app:testDebugUnitTest :app:assembleDebug
```

Expected: every command exits 0.

- [ ] **Step 12: Open the PR**

```bash
gh pr create --title "android: phase 4a (GPS, platform abstraction, UI gating, app icon)" --body "$(cat <<'EOF'
## Summary
- Wires `LocationManager.GPS_PROVIDER` + `GnssStatus` through Kotlin `GpsAdapter` → `PlatformServer` → Go android reader → existing position cache + beacon scheduler.
- Replaces `isAndroid()` / `isDesktop()` predicates with a single `Platform.kind` enum across SPA / Go / Kotlin.
- Permanent UI gating sweep: hides Login, AGW, server LAN-listen config, locks AudioDevices `device_path`.
- Replaces the AGP-default launcher with a graywolf-branded adaptive icon and adds a monochrome notification small-icon.

## Test plan
- [x] `go test -race ./...` (desktop)
- [x] `GOOS=android GOARCH=arm64 CGO_ENABLED=0 GOWORK=off go build ./...`
- [x] `cd web && npm test && npm run build`
- [x] `cd android && ./gradlew :app:testDebugUnitTest :app:assembleDebug`
- [x] T865 device walk: cold start → fix in <30s; warm restart → fix in <5s; perm-revoke → silent skip; UI sweep walkthrough; icon visual confirm.
EOF
)"
```

---

## Self-review checklist (run before handoff)

- [ ] **Spec coverage:** every numbered §2 in-scope item maps to a task above (GPS in → 1/9/12/13/14/15, platform abstraction → 2/3/4/5, UI gating sweep → 17, GPS page redesign → 18, app icon → 19). The proto changes (§10), permission/manifest changes (§8), and PlatformServer broadcast (§3.4) all have dedicated tasks (1, 16, 8).
- [ ] **No placeholders:** every code step has the actual code; the icon task identifies the exact tools (`rsvg-convert` / Android Studio Asset Studio) for the binary asset rendering.
- [ ] **Type consistency:** `pkg/gps.Fix` field names match the actual struct (`Latitude` / `Longitude` / `Altitude` / `Speed` (knots) / `Heading` / `HasAlt` / `HasCourse` / `Timestamp` (time.Time)) — Task 12 converts `speed_mps → Speed * mpsToKnots` and `time_unix_ms → time.UnixMilli().UTC()`. The proto-side names (`AltM`, `SpeedMps`, `CourseDeg`, `TimeUnixMs`, `AccuracyM`) are the regenerated Go bindings from Task 1.
- [ ] **Cross-compile awareness:** every step that touches `pkg/platformsvc`, `pkg/gps/android.go`, `pkg/app/gpsmanager_android.go`, or `cmd/graywolf/main_android.go` runs the `GOOS=android … GOWORK=off go build ./...` check, per the worktree go.work hazard memory.
- [ ] **Live-device gate:** Task 20 is non-skippable; nothing about LocationManager FGS-type-location, runtime permission grouping, or adaptive-icon rendering can be proven from CI.
