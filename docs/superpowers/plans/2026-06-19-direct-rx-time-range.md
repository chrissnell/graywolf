# Direct RX Time-Range Filter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Live Map "Direct RX" filter show a station only if it was heard *directly* (RF, zero digi hops) *within* the selected time range, instead of any station that has *ever* been heard directly (graywolf issue #349).

**Architecture:** Today the Direct RX filter is pure classification — `isDirectRx()` returns true if any stored position is `direction==='RX', hops===0`, and never consults *when* that direct hearing happened. The static-rebeacon merge (issue #130 fix) deliberately keeps the most-direct reception sticky on the collapsed fix but advances its timestamp to the latest beacon, so the time of the last *direct* hearing is currently discarded. We add a `LastDirectHeard` timestamp to each station, set it only on direct receptions (never advanced by digipeated/gated copies), surface it in the stations API DTO, and make the frontend predicate require `LastDirectHeard` to fall inside the active time-range window. This fixes #349 without regressing #130 — direct *classification* stays sticky for display; only the *filter* ages it out of the window.

**Tech Stack:** Go (`pkg/stationcache`, `pkg/webapi`), Svelte 5 frontend (`web/src`), Go `testing`, frontend `node --test`.

---

### File Structure

- `pkg/stationcache/store.go` — add `LastDirectHeard time.Time` field to `Station`.
- `pkg/stationcache/memcache.go` — set `LastDirectHeard` in `updateMetadata` when a packet is direct RF; never on non-direct.
- `pkg/stationcache/memcache_test.go` — tests for the new tracking behavior.
- `pkg/webapi/stations.go` — add `last_direct_heard` to `StationDTO` and map it in `stationToDTO`.
- `pkg/webapi/stations_test.go` (or existing DTO test file) — assert the new field is serialized.
- `web/src/lib/map/direct-rx-core.js` — new pure helper `directHeardWithin(station, cutoffMs)` (follows the established `*-core.js` testable-logic convention).
- `web/src/lib/map/direct-rx-core.test.js` — `node --test` coverage for the helper.
- `web/src/routes/LiveMapV2.svelte` — rewrite `isDirectRx()` to delegate to the helper using the active time-range cutoff; the existing shared-predicate `$effect` (line ~673) and the `rfStationCount` derivation (line ~744) pick this up automatically.

**Out of scope / documented follow-up:** persistence of `LastDirectHeard` across restart in `pkg/historydb`. Without it the field degrades gracefully — after a restart a station only reappears under Direct RX once it is heard directly again, and the filter is otherwise correct. See the "Optional follow-up" note at the end before deciding to ship without it.

---

### Task 1: Track last-direct-heard time in the station cache

**Files:**
- Modify: `pkg/stationcache/store.go` (Station struct, after `LastHeard` at line ~55)
- Modify: `pkg/stationcache/memcache.go` (`updateMetadata`, lines ~246-269)
- Test: `pkg/stationcache/memcache_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `pkg/stationcache/memcache_test.go`:

```go
// TestMemCache_LastDirectHeardSetOnDirect verifies a direct RF reception
// (RX, hops 0) records LastDirectHeard, and a digipeated-only station never
// does (issue #349 — the Direct RX filter keys on this timestamp).
func TestMemCache_LastDirectHeardSetOnDirect(t *testing.T) {
	c := newTestCache(t)

	c.Update([]CacheEntry{stationEntry("stn:DIRECT", "DIRECT", 40.0, -105.0)}) // RX, hops 0
	c.Update([]CacheEntry{digiEntry("stn:DIGIONLY", "DIGIONLY", 41.0, -105.0, 2)}) // RX, hops 2

	results := c.QueryBBox(BBox{SwLat: 39, SwLon: -106, NeLat: 42, NeLon: -104}, 1*time.Hour)
	byKey := map[string]Station{}
	for _, s := range results {
		byKey[s.Key] = s
	}
	if byKey["stn:DIRECT"].LastDirectHeard.IsZero() {
		t.Fatal("direct reception did not set LastDirectHeard")
	}
	if !byKey["stn:DIGIONLY"].LastDirectHeard.IsZero() {
		t.Fatal("digipeated-only station must not set LastDirectHeard")
	}
}

// TestMemCache_LastDirectHeardNotAdvancedByDigi verifies a later digipeated
// copy of a station heard directly earlier does NOT advance LastDirectHeard —
// the direct hearing must age out of the Direct RX window on its own schedule
// (issue #349), even though issue #130 keeps the fix classified as direct.
func TestMemCache_LastDirectHeardNotAdvancedByDigi(t *testing.T) {
	c := newTestCache(t)

	direct := stationEntry("stn:MOBILE", "MOBILE", 40.0, -105.0)
	direct.Timestamp = time.Now().Add(-30 * time.Minute)
	c.Update([]CacheEntry{direct})

	digi := digiEntry("stn:MOBILE", "MOBILE", 40.0, -105.0, 2)
	digi.Timestamp = time.Now()
	c.Update([]CacheEntry{digi})

	results := c.QueryBBox(BBox{SwLat: 39, SwLon: -106, NeLat: 41, NeLon: -104}, 1*time.Hour)
	if len(results) != 1 {
		t.Fatalf("expected 1 station, got %d", len(results))
	}
	if !results[0].LastDirectHeard.Equal(direct.Timestamp) {
		t.Fatalf("LastDirectHeard advanced by digipeated copy: got %v want %v",
			results[0].LastDirectHeard, direct.Timestamp)
	}
	// #130 still holds: the displayed fix is still classified direct.
	p := results[0].Positions[0]
	if !isDirectRF(p.Direction, p.Hops) {
		t.Fatalf("issue #130 regressed: fix no longer direct (Direction=%q Hops=%d)", p.Direction, p.Hops)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/stationcache/ -run TestMemCache_LastDirectHeard -v`
Expected: FAIL — `results[0].LastDirectHeard undefined (type Station has no field LastDirectHeard)`.

- [ ] **Step 3: Add the field to Station**

In `pkg/stationcache/store.go`, add the field immediately after `LastHeard time.Time` (line ~55):

```go
	LastHeard time.Time
	// LastDirectHeard is the timestamp of the most recent reception heard
	// directly on RF (RX, zero digi hops). Set only by direct receptions and
	// never advanced by digipeated/gated/IS copies, so the Live Map "Direct
	// RX" filter can age a station out of the selected time window even when
	// the fix stays classified as direct for display (issues #130 + #349).
	// Zero value means the station has never been heard directly.
	LastDirectHeard time.Time
```

- [ ] **Step 4: Record it on direct receptions**

In `pkg/stationcache/memcache.go`, in `updateMetadata`, add after `s.LastHeard = now` (line ~265):

```go
	s.LastHeard = now
	if isDirectRF(e.Direction, e.Hops) {
		s.LastDirectHeard = e.Timestamp
	}
```

`isDirectRF(direction string, hops int) bool` already exists at line ~275. Using `e.Timestamp` (the packet time, same value stored on each `Position`) keeps it consistent with the per-position timestamps the trail filter already uses.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./pkg/stationcache/ -run TestMemCache_LastDirectHeard -v`
Expected: PASS (both tests).

- [ ] **Step 6: Run the full package to confirm #130 tests still pass**

Run: `go test ./pkg/stationcache/ -v`
Expected: PASS — including `TestMemCache_DirectRFNotMaskedByDigipeat` and `TestMemCache_DigipeatedThenDirect`.

- [ ] **Step 7: Commit**

```bash
git add pkg/stationcache/store.go pkg/stationcache/memcache.go pkg/stationcache/memcache_test.go
git commit -m "stationcache: track LastDirectHeard for Direct RX time-range filter"
```

---

### Task 2: Surface last_direct_heard in the stations API DTO

**Files:**
- Modify: `pkg/webapi/stations.go` (`StationDTO` at line ~22, `stationToDTO` at line ~302)
- Test: `pkg/webapi/stations_test.go`

- [ ] **Step 1: Write the failing test**

Add to `pkg/webapi/stations_test.go` (create the file with `package webapi` and the imports below if it does not exist):

```go
package webapi

import (
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/stationcache"
)

func TestStationToDTO_LastDirectHeard(t *testing.T) {
	direct := time.Now().Add(-10 * time.Minute)
	s := stationcache.Station{
		Callsign:        "W1ABC",
		LastHeard:       time.Now(),
		LastDirectHeard: direct,
		Positions: []stationcache.Position{
			{Lat: 40, Lon: -105, Direction: "RX", Timestamp: time.Now()},
		},
	}
	dto := stationToDTO(s, false, false, nil, time.Now().Add(-time.Hour))
	if !dto.LastDirectHeard.Equal(direct) {
		t.Fatalf("LastDirectHeard not mapped: got %v want %v", dto.LastDirectHeard, direct)
	}
}
```

Confirm the module path first with `head -1 go.mod`; adjust the `stationcache` import path in the snippet if it differs from `github.com/chrissnell/graywolf`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/webapi/ -run TestStationToDTO_LastDirectHeard -v`
Expected: FAIL — `dto.LastDirectHeard undefined (type StationDTO has no field LastDirectHeard)`.

- [ ] **Step 3: Add the field to StationDTO**

In `pkg/webapi/stations.go`, add to `StationDTO` immediately after the `LastHeard` field (line ~36):

```go
	// LastHeard is the UTC RFC3339 timestamp of the most recent packet from this station.
	LastHeard time.Time `json:"last_heard"`
	// LastDirectHeard is the UTC RFC3339 timestamp of the most recent reception heard
	// directly on RF (RX, zero digi hops). Zero value (serialized as the JSON
	// zero time) means the station has never been heard directly. The Live Map
	// "Direct RX" filter requires this to fall within the selected time range.
	LastDirectHeard time.Time `json:"last_direct_heard"`
```

- [ ] **Step 4: Map it in stationToDTO**

In `pkg/webapi/stations.go`, in the `StationDTO{...}` literal inside `stationToDTO` (line ~303), add after `LastHeard: s.LastHeard,`:

```go
		LastHeard:       s.LastHeard,
		LastDirectHeard: s.LastDirectHeard,
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./pkg/webapi/ -run TestStationToDTO_LastDirectHeard -v`
Expected: PASS.

- [ ] **Step 6: Regenerate API types if the repo tracks them**

The frontend has `web/src/lib/api/generated/api.d.ts`. If the repo generates it from the Go DTOs, regenerate so `last_direct_heard` appears:

Run: `grep -rn "generate" Makefile | grep -i "api\|swagger\|openapi"`
If a generate target exists (e.g. `make generate-api`), run it; otherwise note in the commit that the generated types are hand-maintained and skip. Do not hand-edit generated files unless that is the established pattern.

- [ ] **Step 7: Commit**

```bash
git add pkg/webapi/stations.go pkg/webapi/stations_test.go
git commit -m "webapi: expose last_direct_heard on station DTO"
```

---

### Task 3: Make the frontend Direct RX filter time-range aware

**Files:**
- Create: `web/src/lib/map/direct-rx-core.js`
- Test: `web/src/lib/map/direct-rx-core.test.js`
- Modify: `web/src/routes/LiveMapV2.svelte` (`isDirectRx`, lines ~190-200)

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/map/direct-rx-core.test.js`:

```js
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { directHeardWithin } from './direct-rx-core.js';

test('station heard directly within the window qualifies', () => {
  const nowMs = 1_000_000;
  const station = { last_direct_heard: new Date(nowMs - 10_000).toISOString() };
  assert.equal(directHeardWithin(station, nowMs - 60_000), true);
});

test('station last heard directly before the cutoff is excluded', () => {
  const nowMs = 1_000_000;
  const station = { last_direct_heard: new Date(nowMs - 120_000).toISOString() };
  assert.equal(directHeardWithin(station, nowMs - 60_000), false);
});

test('station never heard directly is excluded', () => {
  // Zero-time / missing last_direct_heard means never heard directly.
  assert.equal(directHeardWithin({ last_direct_heard: '0001-01-01T00:00:00Z' }, 0), false);
  assert.equal(directHeardWithin({}, 0), false);
  assert.equal(directHeardWithin(null, 0), false);
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && node --test src/lib/map/direct-rx-core.test.js`
Expected: FAIL — cannot find module `./direct-rx-core.js`.

- [ ] **Step 3: Write the helper**

Create `web/src/lib/map/direct-rx-core.js`:

```js
// Pure predicate behind the Live Map "Direct RX" filter. A station qualifies
// only if it was heard directly on RF (RX, zero digi hops) at or after the
// given cutoff (milliseconds since epoch). This ages a direct hearing out of
// the selected time window even though the server keeps the fix classified as
// direct for display (graywolf issues #130 + #349).
//
// last_direct_heard is the server-supplied timestamp of the most recent direct
// reception. A zero/absent value means the station has never been heard
// directly. The Go zero time serializes as "0001-01-01T00:00:00Z", which
// parses to a negative epoch and therefore never passes a real cutoff.
export function directHeardWithin(station, cutoffMs) {
  const ts = station?.last_direct_heard;
  if (!ts) return false;
  const t = Date.parse(ts);
  if (Number.isNaN(t)) return false;
  return t >= cutoffMs;
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd web && node --test src/lib/map/direct-rx-core.test.js`
Expected: PASS (3 tests).

- [ ] **Step 5: Wire the helper into LiveMapV2.svelte**

In `web/src/routes/LiveMapV2.svelte`, add the import alongside the other `map/` imports (near line 40, where `clock-offset.svelte.js` is imported):

```js
  import { directHeardWithin } from '../lib/map/direct-rx-core.js';
```

Replace the existing `isDirectRx` function (lines ~190-200) with:

```js
  // Direct RX predicate: a station qualifies only if it was heard directly on
  // RF (RX, zero digi hops) WITHIN the active time range. The server tracks the
  // last direct-hearing time in last_direct_heard and never advances it on a
  // later digipeated copy, so a station heard directly earlier but only via a
  // digipeater recently drops out of this filter once the direct hearing falls
  // outside the window (issue #349). Uses serverNow() so the cutoff matches the
  // host clock that stamped the timestamps.
  function isDirectRx(station) {
    const cutoffMs = clockOffset.serverNow() - dataStore.timerangeMs;
    return directHeardWithin(station, cutoffMs);
  }
```

`clockOffset` is already imported (line ~40) and `dataStore.timerangeMs` already exists. The shared-predicate `$effect` (line ~673) and the `rfStationCount` derivation (line ~744) call `isDirectRx` and need no further change.

- [ ] **Step 6: Build the frontend to confirm it compiles**

Run: `cd web && npm run build`
Expected: build succeeds with no errors referencing `isDirectRx`, `directHeardWithin`, or unused imports.

- [ ] **Step 7: Run the frontend test suite**

Run: `cd web && npm test`
Expected: PASS, including the new `direct-rx-core.test.js`.

- [ ] **Step 8: Commit**

```bash
git add web/src/lib/map/direct-rx-core.js web/src/lib/map/direct-rx-core.test.js web/src/routes/LiveMapV2.svelte
git commit -m "livemap: scope Direct RX filter to the selected time range (#349)"
```

---

### Task 4: Manual end-to-end verification

**Files:** none (verification only).

- [ ] **Step 1: Run the backend tests once more across touched packages**

Run: `go test ./pkg/stationcache/ ./pkg/webapi/`
Expected: PASS.

- [ ] **Step 2: Reproduce the #349 scenario manually**

With a running instance (or a seeded dev instance — see `pkg/demoseed`), confirm:
1. A station heard directly, then later only via a digipeater, with a short time range (e.g. 15 min) selected and Direct RX enabled, **no longer** appears once the direct hearing is older than the window.
2. The same station still appears when the time range is widened to include the direct hearing.
3. A station heard directly within the window still appears (no false negatives).
4. The Direct RX count in the status bar (`rfStationCount`) matches the markers shown.

- [ ] **Step 3: Update the wiki if needed**

Per `graywolf/CLAUDE.md`, if any wiki page describes the Direct RX filter or the station cache reception-merge invariants, update it to mention `LastDirectHeard` and the time-range semantics in this same change. Check `docs/wiki/` for a Live Map or stationcache page first.

---

### Optional follow-up: persist LastDirectHeard across restart

Not required for a correct first ship, but decide explicitly before merging. `pkg/historydb` persists station state and `pkg/stationcache/persistent.go` hydrates it on startup. `LastDirectHeard` is currently in-memory only, so after a process restart a station is excluded from Direct RX until it is heard directly again — the filter stays *correct* (no false positives), it just forgets older-but-in-window direct hearings across a restart.

To persist it: add a `last_direct_heard` column via a historydb migration, include it in `WriteEntries`/`LoadRecent`, and map it in the `CacheEntry` <-> `Station` hydrate path. This is a small, self-contained change but involves a schema migration, so it is kept separate. If shipping without it, note the restart behavior in the PR description.

---

## Self-Review Notes

- **Spec coverage:** #349 requirement (Direct RX must respect the time range) is covered by Tasks 1-3; #130 non-regression is asserted in Task 1 Step 1 and Step 6.
- **Type consistency:** Go field `LastDirectHeard` (Station + StationDTO), JSON key `last_direct_heard`, JS helper `directHeardWithin(station, cutoffMs)` — names used identically across all tasks.
- **No placeholders:** every code step shows complete code; the only conditional steps (Task 2 Step 6 API regen, Task 4 Step 3 wiki) are explicit checks with a defined default action.
