# Direct-RX Heatmap Layer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Live Map heatmap layer showing where the station directly received RF packets during the selected interval, with intensity driven by total packet count and digipeated packets attributed to the digipeater's location.

**Architecture:** A new append-only `rx_events` table in the history DB records one row per directly-received (`RX`, non-gated) packet, attributed to the last RF transmitter (origin station for direct packets, last-hop digipeater for digipeated ones). A new `GET /api/heatmap` endpoint aggregates those rows over a time window + bbox into weighted GeoJSON points. A MapLibre `heatmap` layer on the Live Map renders them, with a toggle and opacity slider.

**Tech Stack:** Go (GORM + SQLite via `pkg/historydb`, `net/http` handlers in `pkg/webapi`), Svelte 5 + MapLibre GL v5 frontend (`web/src/lib/map`), `node --test` for pure-JS unit tests.

Design spec: `docs/superpowers/specs/2026-07-02-direct-rx-heatmap-design.md`.

---

## File Structure

**Backend (Go):**
- `pkg/stationcache/heatmap.go` (new) — `HeatPoint` / `HeatmapResult` value types (defined here so both `historydb` and `webapi` can use them without an import cycle; `stationcache` is already imported by both).
- `pkg/historydb/historydb.go` (modify) — `rx_events` schema in `bootstrap`, `recordRxEvent` in the write path, `QueryHeatmap` read method, `rx_events` pruning in `Prune`.
- `pkg/historydb/heatmap_test.go` (new) — ingest-attribution + query + retention tests.
- `pkg/stationcache/persistent.go` (modify) — add `QueryHeatmap` to the `HistoryStore` interface and a delegating `PersistentCache.QueryHeatmap`.
- `pkg/webapi/heatmap.go` (new) — `HeatmapStore` interface, `RegisterHeatmap`, `heatmapHandler` producing GeoJSON.
- `pkg/webapi/heatmap_test.go` (new) — handler tests with a fake store.
- `pkg/app/wiring.go` (modify) — one line registering the heatmap route.

**Frontend (JS/Svelte):**
- `web/src/lib/map/sources/heatmap-source.js` (new) — `heatmapUrl`, `loadHeatmap`, `normalizeFeatureWeights`.
- `web/src/lib/map/sources/heatmap-source.test.js` (new) — pure-function tests.
- `web/src/lib/map/layers/direct-rx-heatmap.js` (new) — `mountHeatmapLayer`.
- `web/src/lib/map/layer-toggles-core.js` (modify) — add `directRxHeatmap` + `directRxHeatmapOpacity` defaults.
- `web/src/lib/map/layer-toggles-core.test.js` (modify) — assert new defaults.
- `web/src/routes/LiveMapV2.svelte` (modify) — import, mount, toggle + opacity UI (card + bottom sheet), fetch scheduling, style-reload refresh.

**Docs:**
- `docs/wiki/` (modify) — Live Map layers page entry.

---

## Task 1: `rx_events` schema + result value types

**Files:**
- Create: `pkg/stationcache/heatmap.go`
- Modify: `pkg/historydb/historydb.go` (the `bootstrap` function's `stmts` slice)
- Test: `pkg/historydb/heatmap_test.go`

- [ ] **Step 1: Add the shared result value types**

Create `pkg/stationcache/heatmap.go`:

```go
package stationcache

// HeatPoint is one aggregated heatmap bucket: a coordinate and the total
// number of directly-received packets attributed to it in the query window.
type HeatPoint struct {
	Lat   float64
	Lon   float64
	Count int
}

// HeatmapResult is the aggregate returned by a heatmap query. Points are the
// located buckets, MaxCount is the largest single-bucket count (for client-side
// weight normalization), and Unlocatable counts packets whose attributed
// transmitter had no known position in the window.
type HeatmapResult struct {
	Points      []HeatPoint
	MaxCount    int
	Unlocatable int
}
```

- [ ] **Step 2: Write the failing schema test**

Create `pkg/historydb/heatmap_test.go`:

```go
package historydb

import (
	"testing"
)

func TestBootstrapCreatesRxEvents(t *testing.T) {
	db := openTestDB(t)
	var name string
	err := db.db.Raw(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='rx_events'",
	).Scan(&name).Error
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if name != "rx_events" {
		t.Fatalf("rx_events table not created, got %q", name)
	}
}
```

Add a shared test helper in the same file (used by later tasks too):

```go
func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(t.TempDir() + "/history.db")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./pkg/historydb/ -run TestBootstrapCreatesRxEvents -v`
Expected: FAIL — `rx_events table not created, got ""`.

- [ ] **Step 4: Add the `rx_events` CREATE TABLE to `bootstrap`**

In `pkg/historydb/historydb.go`, inside `bootstrap`'s `stmts := []string{ ... }` slice (the block that currently ends with the `weather` table), append two statements after the `weather` CREATE TABLE:

```go
		`CREATE TABLE IF NOT EXISTS rx_events (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME NOT NULL,
			attr_key  TEXT NOT NULL,
			hops      INTEGER NOT NULL DEFAULT 0,
			lat       REAL NOT NULL DEFAULT 0,
			lon       REAL NOT NULL DEFAULT 0,
			has_pos   INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_rx_events_time ON rx_events(timestamp)`,
```

`CREATE TABLE IF NOT EXISTS` is self-migrating for existing databases (matches how `stations`/`positions`/`weather` are created), so no `ALTER TABLE` block is needed.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./pkg/historydb/ -run TestBootstrapCreatesRxEvents -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/stationcache/heatmap.go pkg/historydb/historydb.go pkg/historydb/heatmap_test.go
git commit -m "Add rx_events table and heatmap result types"
```

---

## Task 2: Record an rx_event for every directly-received packet

**Files:**
- Modify: `pkg/historydb/historydb.go` (the `WriteEntries` transaction loop; add `recordRxEvent`)
- Test: `pkg/historydb/heatmap_test.go`

Attribution rules (from the spec):
- Skip entries that are not `Direction == "RX"`, or that are `Gated` (Internet-to-RF inner packets never count).
- **Digipeated** (`Hops > 0` and a last H-bit digi exists in `Path`): `attr_key = "stn:" + lastDigi`, `has_pos = 0` (the digi's own position is resolved at query time; the packet's lat/lon is the *origin's* and must not be used).
- **Direct** (otherwise): `attr_key = e.Key` (the origin). If the packet carried a position (`e.HasPos`), store `lat/lon` with `has_pos = 1`; else `has_pos = 0` (resolve the origin's position at query time).

- [ ] **Step 1: Write the failing attribution test**

Add to `pkg/historydb/heatmap_test.go`:

```go
import (
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/stationcache"
)

// rxEventRow mirrors the rx_events columns for test assertions.
type rxEventRow struct {
	AttrKey string
	Hops    int
	Lat     float64
	Lon     float64
	HasPos  bool
}

func loadRxEvents(t *testing.T, db *DB) []rxEventRow {
	t.Helper()
	var rows []rxEventRow
	if err := db.db.Raw(
		"SELECT attr_key, hops, lat, lon, has_pos FROM rx_events ORDER BY id",
	).Scan(&rows).Error; err != nil {
		t.Fatalf("load rx_events: %v", err)
	}
	return rows
}

func TestWriteEntriesRecordsRxEvents(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().Truncate(time.Second)

	entries := []stationcache.CacheEntry{
		// Direct RX with position -> event at packet coords, has_pos=1.
		{
			Key: "stn:W1ABC", Callsign: "W1ABC", Direction: "RX",
			Lat: 35.0, Lon: -95.0, HasPos: true,
			Path: []string{}, Hops: 0, Timestamp: now,
		},
		// Digipeated RX -> event at the digi, has_pos=0, packet coords ignored.
		{
			Key: "stn:W2DEF", Callsign: "W2DEF", Direction: "RX",
			Lat: 40.0, Lon: -80.0, HasPos: true,
			Path: []string{"WIDE1-1", "N0DIGI*"}, Hops: 1, Timestamp: now,
		},
		// IS packet -> no event.
		{
			Key: "stn:W3GHI", Callsign: "W3GHI", Direction: "IS",
			Lat: 30.0, Lon: -90.0, HasPos: true, Timestamp: now,
		},
		// Gated RX -> no event.
		{
			Key: "stn:W4JKL", Callsign: "W4JKL", Direction: "RX", Gated: true,
			Lat: 31.0, Lon: -91.0, HasPos: true, Timestamp: now,
		},
	}
	if err := db.WriteEntries(entries); err != nil {
		t.Fatalf("WriteEntries: %v", err)
	}

	got := loadRxEvents(t, db)
	if len(got) != 2 {
		t.Fatalf("expected 2 rx_events, got %d: %+v", len(got), got)
	}
	if got[0] != (rxEventRow{AttrKey: "stn:W1ABC", Hops: 0, Lat: 35.0, Lon: -95.0, HasPos: true}) {
		t.Errorf("direct event wrong: %+v", got[0])
	}
	if got[1] != (rxEventRow{AttrKey: "stn:N0DIGI", Hops: 1, Lat: 0, Lon: 0, HasPos: false}) {
		t.Errorf("digipeated event wrong: %+v", got[1])
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/historydb/ -run TestWriteEntriesRecordsRxEvents -v`
Expected: FAIL — `expected 2 rx_events, got 0`.

- [ ] **Step 3: Add `recordRxEvent` and call it from `WriteEntries`**

In `pkg/historydb/historydb.go`, add this helper (near `insertPositionIfMoved`):

```go
// lastHBitDigi returns the callsign of the last used (H-bit, "*"-suffixed)
// digipeater in path, or "" if there is none.
func lastHBitDigi(path []string) string {
	last := ""
	for _, hop := range path {
		if strings.HasSuffix(hop, "*") {
			last = strings.TrimSuffix(hop, "*")
		}
	}
	return last
}

// recordRxEvent appends one rx_events row for a directly-received packet.
// Entries that did not arrive on RF, or that are Internet-to-RF gated, are
// skipped. Digipeated packets are attributed to the last-hop digipeater's
// station key with has_pos=0 (its position is resolved at query time); direct
// packets are attributed to the origin, storing the packet's own coordinates
// when present.
func recordRxEvent(tx *gorm.DB, e *stationcache.CacheEntry) error {
	if e.Direction != "RX" || e.Gated {
		return nil
	}
	attrKey := e.Key
	lat, lon := 0.0, 0.0
	hasPos := 0
	if digi := lastHBitDigi(e.Path); e.Hops > 0 && digi != "" {
		attrKey = "stn:" + digi
	} else if e.HasPos {
		lat, lon = e.Lat, e.Lon
		hasPos = 1
	}
	return tx.Exec(
		`INSERT INTO rx_events (timestamp, attr_key, hops, lat, lon, has_pos)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		e.Timestamp, attrKey, e.Hops, lat, lon, hasPos,
	).Error
}
```

In `WriteEntries`, inside the `for i := range entries` loop, after the `if e.Weather != nil { ... }` block and before the loop's closing brace, add:

```go
			if err := recordRxEvent(tx, e); err != nil {
				return fmt.Errorf("record rx_event %s: %w", e.Key, err)
			}
```

Confirm `strings` is imported in `historydb.go` (it uses `filepath`/`json` already; add `"strings"` to the import block if absent).

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/historydb/ -run TestWriteEntriesRecordsRxEvents -v`
Expected: PASS.

- [ ] **Step 5: Run the full historydb package to check for regressions**

Run: `go test ./pkg/historydb/ -v`
Expected: PASS (existing tests unaffected).

- [ ] **Step 6: Commit**

```bash
git add pkg/historydb/historydb.go pkg/historydb/heatmap_test.go
git commit -m "Record rx_events for directly-received packets"
```

---

## Task 3: `QueryHeatmap` aggregation on the history DB

**Files:**
- Modify: `pkg/historydb/historydb.go` (add `QueryHeatmap` + a bucket constant)
- Test: `pkg/historydb/heatmap_test.go`

Algorithm:
1. Load `rx_events` with `timestamp >= now-window`.
2. For rows with `has_pos = 0`, resolve the latest position of `attr_key` from `positions`. Rows whose key has no stored position count toward `Unlocatable`.
3. Drop located points outside `bbox`.
4. Bucket by coordinate rounded to `heatBucketDecimals` (4 dp ≈ 11 m — merges a fixed station's re-beacons while preserving mobile corridors) and sum counts.
5. Return points, `MaxCount`, `Unlocatable`.

- [ ] **Step 1: Write the failing query test**

Add to `pkg/historydb/heatmap_test.go`:

```go
func TestQueryHeatmapAggregates(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().Truncate(time.Second)

	entries := []stationcache.CacheEntry{
		// Fixed station heard directly 3x from the same spot -> count 3.
		{Key: "stn:W1ABC", Callsign: "W1ABC", Direction: "RX", Lat: 35.0, Lon: -95.0, HasPos: true, Timestamp: now},
		{Key: "stn:W1ABC", Callsign: "W1ABC", Direction: "RX", Lat: 35.0, Lon: -95.0, HasPos: true, Timestamp: now},
		{Key: "stn:W1ABC", Callsign: "W1ABC", Direction: "RX", Lat: 35.0, Lon: -95.0, HasPos: true, Timestamp: now},
		// A known digipeater beacons its own position (direct), so it is resolvable.
		{Key: "stn:N0DIGI", Callsign: "N0DIGI", Direction: "RX", Lat: 36.0, Lon: -96.0, HasPos: true, Timestamp: now},
		// Digipeated packet -> attributed to N0DIGI's location (has_pos=0, resolved).
		{Key: "stn:W2DEF", Callsign: "W2DEF", Direction: "RX", Lat: 40.0, Lon: -80.0, HasPos: true, Path: []string{"N0DIGI*"}, Hops: 1, Timestamp: now},
		// Digipeated via an UNKNOWN digi -> unlocatable.
		{Key: "stn:W5MNO", Callsign: "W5MNO", Direction: "RX", Lat: 41.0, Lon: -81.0, HasPos: true, Path: []string{"NOPOS*"}, Hops: 1, Timestamp: now},
	}
	if err := db.WriteEntries(entries); err != nil {
		t.Fatalf("WriteEntries: %v", err)
	}

	bbox := stationcache.BBox{SwLat: 30, SwLon: -100, NeLat: 45, NeLon: -70}
	res, err := db.QueryHeatmap(time.Hour, bbox)
	if err != nil {
		t.Fatalf("QueryHeatmap: %v", err)
	}
	if res.Unlocatable != 1 {
		t.Errorf("Unlocatable = %d, want 1", res.Unlocatable)
	}
	// Buckets: W1ABC(35,-95)=3 ; N0DIGI(36,-96)=1 own beacon + 1 via = 2.
	counts := map[string]int{}
	for _, p := range res.Points {
		counts[fmtKey(p.Lat, p.Lon)] = p.Count
	}
	if counts[fmtKey(35.0, -95.0)] != 3 {
		t.Errorf("W1ABC bucket = %d, want 3", counts[fmtKey(35.0, -95.0)])
	}
	if counts[fmtKey(36.0, -96.0)] != 2 {
		t.Errorf("N0DIGI bucket = %d, want 2", counts[fmtKey(36.0, -96.0)])
	}
	if res.MaxCount != 3 {
		t.Errorf("MaxCount = %d, want 3", res.MaxCount)
	}
}

func fmtKey(lat, lon float64) string {
	return heatBucketKey(lat, lon)
}

func TestQueryHeatmapBBoxFilter(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().Truncate(time.Second)
	entries := []stationcache.CacheEntry{
		{Key: "stn:IN", Callsign: "IN", Direction: "RX", Lat: 35.0, Lon: -95.0, HasPos: true, Timestamp: now},
		{Key: "stn:OUT", Callsign: "OUT", Direction: "RX", Lat: 10.0, Lon: 10.0, HasPos: true, Timestamp: now},
	}
	if err := db.WriteEntries(entries); err != nil {
		t.Fatalf("WriteEntries: %v", err)
	}
	res, err := db.QueryHeatmap(time.Hour, stationcache.BBox{SwLat: 30, SwLon: -100, NeLat: 45, NeLon: -70})
	if err != nil {
		t.Fatalf("QueryHeatmap: %v", err)
	}
	if len(res.Points) != 1 || res.Points[0].Count != 1 {
		t.Fatalf("expected 1 in-bbox point, got %+v", res.Points)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/historydb/ -run TestQueryHeatmap -v`
Expected: FAIL — `db.QueryHeatmap undefined` (compile error).

- [ ] **Step 3: Implement `QueryHeatmap` + bucket helpers**

Add to `pkg/historydb/historydb.go`:

```go
// heatBucketDecimals controls coordinate rounding for heatmap aggregation.
// 4 dp (~11 m) merges a fixed station's re-beacons into one bucket while
// keeping mobile corridors at street resolution.
const heatBucketDecimals = 4

func roundHeat(v float64) float64 {
	p := math.Pow(10, heatBucketDecimals)
	return math.Round(v*p) / p
}

func heatBucketKey(lat, lon float64) string {
	return fmt.Sprintf("%.4f,%.4f", roundHeat(lat), roundHeat(lon))
}

// QueryHeatmap aggregates directly-received packets over the given window into
// counted coordinate buckets within bbox. Events whose attributed transmitter
// has no known position are tallied in Unlocatable rather than dropped.
func (d *DB) QueryHeatmap(window time.Duration, bbox stationcache.BBox) (*stationcache.HeatmapResult, error) {
	cutoff := time.Now().Add(-window)

	type eventRow struct {
		AttrKey string
		Lat     float64
		Lon     float64
		HasPos  bool
	}
	var events []eventRow
	if err := d.db.Raw(
		"SELECT attr_key, lat, lon, has_pos FROM rx_events WHERE timestamp >= ?",
		cutoff,
	).Scan(&events).Error; err != nil {
		return nil, fmt.Errorf("load rx_events: %w", err)
	}

	// Resolve latest position for keys that were stored without one.
	needResolve := map[string]struct{}{}
	for _, e := range events {
		if !e.HasPos {
			needResolve[e.AttrKey] = struct{}{}
		}
	}
	resolved := make(map[string]stationcache.LatLon, len(needResolve))
	for key := range needResolve {
		type ll struct {
			Lat float64
			Lon float64
		}
		var got ll
		res := d.db.Raw(
			"SELECT lat, lon FROM positions WHERE station_key = ? ORDER BY timestamp DESC LIMIT 1",
			key,
		).Scan(&got)
		if res.Error == nil && res.RowsAffected > 0 {
			resolved[key] = stationcache.LatLon{Lat: got.Lat, Lon: got.Lon}
		}
	}

	buckets := map[string]*stationcache.HeatPoint{}
	unlocatable := 0
	for _, e := range events {
		lat, lon := e.Lat, e.Lon
		if !e.HasPos {
			ll, ok := resolved[e.AttrKey]
			if !ok {
				unlocatable++
				continue
			}
			lat, lon = ll.Lat, ll.Lon
		}
		if lat < bbox.SwLat || lat > bbox.NeLat || lon < bbox.SwLon || lon > bbox.NeLon {
			continue
		}
		key := heatBucketKey(lat, lon)
		if b, ok := buckets[key]; ok {
			b.Count++
		} else {
			buckets[key] = &stationcache.HeatPoint{Lat: roundHeat(lat), Lon: roundHeat(lon), Count: 1}
		}
	}

	out := &stationcache.HeatmapResult{
		Points:      make([]stationcache.HeatPoint, 0, len(buckets)),
		Unlocatable: unlocatable,
	}
	for _, b := range buckets {
		if b.Count > out.MaxCount {
			out.MaxCount = b.Count
		}
		out.Points = append(out.Points, *b)
	}
	return out, nil
}
```

Confirm `math` is imported in `historydb.go` (it is — `insertPositionIfMoved` uses `math.Abs`).

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./pkg/historydb/ -run TestQueryHeatmap -v`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
git add pkg/historydb/historydb.go pkg/historydb/heatmap_test.go
git commit -m "Add QueryHeatmap aggregation over rx_events"
```

---

## Task 4: Prune old rx_events

**Files:**
- Modify: `pkg/historydb/historydb.go` (the `Prune` method + a retention constant)
- Test: `pkg/historydb/heatmap_test.go`

- [ ] **Step 1: Write the failing retention test**

Add to `pkg/historydb/heatmap_test.go`:

```go
func TestPruneDropsOldRxEvents(t *testing.T) {
	db := openTestDB(t)
	old := time.Now().Add(-rxEventsRetention - time.Hour)
	recent := time.Now().Add(-time.Hour)

	entries := []stationcache.CacheEntry{
		{Key: "stn:OLD", Callsign: "OLD", Direction: "RX", Lat: 35.0, Lon: -95.0, HasPos: true, Timestamp: old},
		{Key: "stn:NEW", Callsign: "NEW", Direction: "RX", Lat: 35.0, Lon: -95.0, HasPos: true, Timestamp: recent},
	}
	if err := db.WriteEntries(entries); err != nil {
		t.Fatalf("WriteEntries: %v", err)
	}
	if err := db.Prune(pruneMaxAgeForTest()); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	got := loadRxEvents(t, db)
	if len(got) != 1 || got[0].AttrKey != "stn:NEW" {
		t.Fatalf("expected only the recent rx_event to survive, got %+v", got)
	}
}

// pruneMaxAgeForTest keeps stations/positions long enough that the test's
// assertion isolates rx_events retention.
func pruneMaxAgeForTest() time.Duration { return 365 * 24 * time.Hour }
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/historydb/ -run TestPruneDropsOldRxEvents -v`
Expected: FAIL — `rxEventsRetention` undefined (compile error).

- [ ] **Step 3: Add the retention constant and extend `Prune`**

In `pkg/historydb/historydb.go`, add near the top-level constants:

```go
// rxEventsRetention bounds how long per-packet heatmap events are kept. The
// Live Map's longest interval is 7 days, so 8 days covers it with margin.
const rxEventsRetention = 8 * 24 * time.Hour
```

In the `Prune` method's transaction, add an rx_events delete before the final `return`:

```go
		if err := tx.Exec(
			"DELETE FROM rx_events WHERE timestamp < ?",
			time.Now().Add(-rxEventsRetention),
		).Error; err != nil {
			return err
		}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./pkg/historydb/ -run TestPruneDropsOldRxEvents -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/historydb/historydb.go pkg/historydb/heatmap_test.go
git commit -m "Prune rx_events past the retention horizon"
```

---

## Task 5: Expose `QueryHeatmap` through the cache interface

**Files:**
- Modify: `pkg/stationcache/persistent.go` (`HistoryStore` interface + `PersistentCache.QueryHeatmap`)

- [ ] **Step 1: Add the method to the `HistoryStore` interface**

In `pkg/stationcache/persistent.go`, find the `HistoryStore` interface (it declares `WriteEntries`, `LoadRecent`, `Prune`, `Close`) and add:

```go
	QueryHeatmap(window time.Duration, bbox BBox) (*HeatmapResult, error)
```

- [ ] **Step 2: Add the delegating method on `PersistentCache`**

Add near `QueryBBox`:

```go
// QueryHeatmap returns aggregated directly-received-packet heat over the
// window within bbox. Returns an empty result when persistence is disabled.
func (p *PersistentCache) QueryHeatmap(window time.Duration, bbox BBox) (*HeatmapResult, error) {
	p.mu.RLock()
	hdb := p.hdb
	p.mu.RUnlock()
	if hdb == nil {
		return &HeatmapResult{}, nil
	}
	return hdb.QueryHeatmap(window, bbox)
}
```

- [ ] **Step 3: Verify the package compiles and `*historydb.DB` still satisfies `HistoryStore`**

Run: `go build ./... && go test ./pkg/stationcache/ ./pkg/historydb/`
Expected: PASS. (If `*DB` no longer satisfies `HistoryStore`, the build fails where `Reconfigure(hdb)` is called — Task 3 already added `QueryHeatmap` to `*DB`, so it should satisfy it.)

- [ ] **Step 4: Commit**

```bash
git add pkg/stationcache/persistent.go
git commit -m "Expose QueryHeatmap through the station cache interface"
```

---

## Task 6: `GET /api/heatmap` endpoint

**Files:**
- Create: `pkg/webapi/heatmap.go`
- Test: `pkg/webapi/heatmap_test.go`
- Modify: `pkg/app/wiring.go` (register the route)

Response is a GeoJSON `FeatureCollection`; each feature has a `count`; top-level `properties` carry `max_count` and `unlocatable`.

- [ ] **Step 1: Write the failing handler test**

Create `pkg/webapi/heatmap_test.go`:

```go
package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/stationcache"
)

type fakeHeatmapStore struct {
	res *stationcache.HeatmapResult
	err error
}

func (f *fakeHeatmapStore) QueryHeatmap(_ time.Duration, _ stationcache.BBox) (*stationcache.HeatmapResult, error) {
	return f.res, f.err
}

func TestHeatmapHandler(t *testing.T) {
	store := &fakeHeatmapStore{res: &stationcache.HeatmapResult{
		Points:      []stationcache.HeatPoint{{Lat: 35.0, Lon: -95.0, Count: 3}},
		MaxCount:    3,
		Unlocatable: 7,
	}}
	mux := http.NewServeMux()
	RegisterHeatmap(nil, mux, store)

	req := httptest.NewRequest(http.MethodGet, "/api/heatmap?bbox=30,-100,40,-90&timerange=3600", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var fc struct {
		Type     string `json:"type"`
		Features []struct {
			Geometry struct {
				Coordinates [2]float64 `json:"coordinates"`
			} `json:"geometry"`
			Properties struct {
				Count int `json:"count"`
			} `json:"properties"`
		} `json:"features"`
		Properties struct {
			MaxCount    int `json:"max_count"`
			Unlocatable int `json:"unlocatable"`
		} `json:"properties"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&fc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if fc.Type != "FeatureCollection" {
		t.Errorf("type = %q", fc.Type)
	}
	if len(fc.Features) != 1 {
		t.Fatalf("features = %d, want 1", len(fc.Features))
	}
	if fc.Features[0].Geometry.Coordinates != [2]float64{-95.0, 35.0} {
		t.Errorf("coords = %v, want [-95,35] (lon,lat)", fc.Features[0].Geometry.Coordinates)
	}
	if fc.Features[0].Properties.Count != 3 {
		t.Errorf("count = %d, want 3", fc.Features[0].Properties.Count)
	}
	if fc.Properties.MaxCount != 3 || fc.Properties.Unlocatable != 7 {
		t.Errorf("max_count=%d unlocatable=%d, want 3/7", fc.Properties.MaxCount, fc.Properties.Unlocatable)
	}
}

func TestHeatmapHandlerBadBBox(t *testing.T) {
	mux := http.NewServeMux()
	RegisterHeatmap(nil, mux, &fakeHeatmapStore{res: &stationcache.HeatmapResult{}})
	req := httptest.NewRequest(http.MethodGet, "/api/heatmap", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/webapi/ -run TestHeatmapHandler -v`
Expected: FAIL — `RegisterHeatmap undefined` (compile error).

- [ ] **Step 3: Implement the handler**

Create `pkg/webapi/heatmap.go`:

```go
package webapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/chrissnell/graywolf/pkg/stationcache"
)

// HeatmapStore is the read side the heatmap handler needs; *stationcache
// .PersistentCache satisfies it.
type HeatmapStore interface {
	QueryHeatmap(window time.Duration, bbox stationcache.BBox) (*stationcache.HeatmapResult, error)
}

// RegisterHeatmap installs GET /api/heatmap. Signature shape (mux second)
// matches the other RegisterXxx helpers in this package.
func RegisterHeatmap(srv *Server, mux *http.ServeMux, store HeatmapStore) {
	_ = srv // kept for consistency with other RegisterXxx
	mux.HandleFunc("GET /api/heatmap", heatmapHandler(store))
}

func heatmapHandler(store HeatmapStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		bbox, err := parseBBox(q.Get("bbox"))
		if err != nil {
			badRequest(w, err.Error())
			return
		}

		timerange := 3600 * time.Second
		if s := q.Get("timerange"); s != "" {
			secs, err := strconv.Atoi(s)
			if err != nil || secs <= 0 {
				badRequest(w, "timerange must be a positive integer (seconds)")
				return
			}
			timerange = time.Duration(secs) * time.Second
		}

		res, err := store.QueryHeatmap(timerange, bbox)
		if err != nil {
			http.Error(w, "heatmap query failed", http.StatusInternalServerError)
			return
		}
		if res == nil {
			res = &stationcache.HeatmapResult{}
		}

		type geometry struct {
			Type        string     `json:"type"`
			Coordinates [2]float64 `json:"coordinates"`
		}
		type feature struct {
			Type       string         `json:"type"`
			Geometry   geometry       `json:"geometry"`
			Properties map[string]int `json:"properties"`
		}
		features := make([]feature, 0, len(res.Points))
		for _, p := range res.Points {
			features = append(features, feature{
				Type:       "Feature",
				Geometry:   geometry{Type: "Point", Coordinates: [2]float64{p.Lon, p.Lat}},
				Properties: map[string]int{"count": p.Count},
			})
		}
		body := map[string]any{
			"type":     "FeatureCollection",
			"features": features,
			"properties": map[string]int{
				"max_count":   res.MaxCount,
				"unlocatable": res.Unlocatable,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}
}
```

Note: `badRequest` is the existing helper used by `parseBBox` callers in `stations.go`; reuse it.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./pkg/webapi/ -run TestHeatmapHandler -v`
Expected: PASS (both tests).

- [ ] **Step 5: Register the route in app wiring**

In `pkg/app/wiring.go`, next to the existing `webapi.RegisterStations(apiSrv, apiMux, a.stationCache)` line, add:

```go
	webapi.RegisterHeatmap(apiSrv, apiMux, a.stationCache)
```

(`a.stationCache` is a `*stationcache.PersistentCache`, which now satisfies `HeatmapStore`.)

- [ ] **Step 6: Build and test the whole backend**

Run: `go build ./... && go test ./pkg/webapi/ ./pkg/app/ ./pkg/historydb/ ./pkg/stationcache/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add pkg/webapi/heatmap.go pkg/webapi/heatmap_test.go pkg/app/wiring.go
git commit -m "Add GET /api/heatmap endpoint"
```

---

## Task 7: Frontend heatmap source (pure helpers)

**Files:**
- Create: `web/src/lib/map/sources/heatmap-source.js`
- Test: `web/src/lib/map/sources/heatmap-source.test.js`

All frontend commands run from `web/`.

- [ ] **Step 1: Write the failing unit tests**

Create `web/src/lib/map/sources/heatmap-source.test.js`:

```javascript
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { heatmapUrl, normalizeFeatureWeights } from './heatmap-source.js';

test('heatmapUrl builds bbox + timerange query', () => {
  const url = heatmapUrl({ swLat: 30, swLon: -100, neLat: 40, neLon: -90 }, 3600);
  assert.equal(url, '/api/heatmap?bbox=30.00000%2C-100.00000%2C40.00000%2C-90.00000&timerange=3600');
});

test('normalizeFeatureWeights adds w = count/maxCount', () => {
  const feats = [
    { type: 'Feature', geometry: { type: 'Point', coordinates: [-95, 35] }, properties: { count: 3 } },
    { type: 'Feature', geometry: { type: 'Point', coordinates: [-96, 36] }, properties: { count: 1 } },
  ];
  const out = normalizeFeatureWeights(feats, 3);
  assert.equal(out[0].properties.w, 1);
  assert.equal(out[1].properties.w, 1 / 3);
  // original count preserved
  assert.equal(out[0].properties.count, 3);
});

test('normalizeFeatureWeights is safe when maxCount is 0', () => {
  const feats = [{ type: 'Feature', geometry: { type: 'Point', coordinates: [0, 0] }, properties: { count: 0 } }];
  const out = normalizeFeatureWeights(feats, 0);
  assert.equal(out[0].properties.w, 0);
});

test('normalizeFeatureWeights tolerates empty input', () => {
  assert.deepEqual(normalizeFeatureWeights([], 0), []);
  assert.deepEqual(normalizeFeatureWeights(undefined, 5), []);
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && node --test src/lib/map/sources/heatmap-source.test.js`
Expected: FAIL — cannot find module `./heatmap-source.js`.

- [ ] **Step 3: Implement the source helpers**

Create `web/src/lib/map/sources/heatmap-source.js`:

```javascript
// Data source for the Live Map direct-RX heatmap layer. Pure URL/weight
// helpers are unit-tested under node --test; loadHeatmap wraps fetch.

// Build the /api/heatmap query for a viewport bbox and interval (seconds).
// bbox matches the server's parseBBox order: sw_lat,sw_lon,ne_lat,ne_lon.
export function heatmapUrl(bbox, timerangeSec) {
  const b = `${bbox.swLat.toFixed(5)},${bbox.swLon.toFixed(5)},${bbox.neLat.toFixed(5)},${bbox.neLon.toFixed(5)}`;
  const params = new URLSearchParams();
  params.set('bbox', b);
  params.set('timerange', String(Math.floor(timerangeSec)));
  return `/api/heatmap?${params.toString()}`;
}

// Return a shallow-copied feature list with a normalized weight property
// `w` = count / maxCount (0..1). The heatmap paint reads `w` so the color
// ramp is independent of absolute packet volume. Safe for empty input and
// maxCount <= 0.
export function normalizeFeatureWeights(features, maxCount) {
  if (!Array.isArray(features)) return [];
  const denom = maxCount > 0 ? maxCount : 0;
  return features.map((f) => ({
    ...f,
    properties: {
      ...f.properties,
      w: denom ? (f.properties?.count ?? 0) / denom : 0,
    },
  }));
}

// Fetch and parse the heatmap for the given viewport/interval. Returns
// { geojson, maxCount, unlocatable }. fetchFn is injectable for tests.
export async function loadHeatmap(bbox, timerangeSec, fetchFn = fetch) {
  const res = await fetchFn(heatmapUrl(bbox, timerangeSec), {
    credentials: 'same-origin',
  });
  if (!res.ok) {
    return { geojson: { type: 'FeatureCollection', features: [] }, maxCount: 0, unlocatable: 0 };
  }
  const body = await res.json();
  return {
    geojson: { type: 'FeatureCollection', features: body.features ?? [] },
    maxCount: body.properties?.max_count ?? 0,
    unlocatable: body.properties?.unlocatable ?? 0,
  };
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd web && node --test src/lib/map/sources/heatmap-source.test.js`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/map/sources/heatmap-source.js web/src/lib/map/sources/heatmap-source.test.js
git commit -m "Add heatmap source helpers"
```

---

## Task 8: Frontend heatmap layer module

**Files:**
- Create: `web/src/lib/map/layers/direct-rx-heatmap.js`

This layer has no pure logic beyond `normalizeFeatureWeights` (already tested); it wraps MapLibre imperative calls, mirroring `radar.js`. No new unit test — it is exercised manually + via the Task 9 wiring. Keep it small and side-effect-only.

- [ ] **Step 1: Implement the layer module**

Create `web/src/lib/map/layers/direct-rx-heatmap.js`:

```javascript
// MapLibre heatmap layer for directly-received RF packets. Follows the
// radar.js pattern: ensure() re-adds the source+layer after a basemap
// setStyle() drops user layers; refresh() feeds new data.
import { normalizeFeatureWeights } from '../sources/heatmap-source.js';

const SRC = 'direct-rx-heatmap-src';
const LYR = 'direct-rx-heatmap';

const EMPTY = { type: 'FeatureCollection', features: [] };

// Insert the heatmap below the first trail/line layer if present so trails
// stay readable; DOM station markers always render above the GL canvas.
function beforeId(map) {
  for (const id of ['trails-line', 'trails-dots', 'trails-glow']) {
    if (map.getLayer(id)) return id;
  }
  return undefined;
}

export function mountHeatmapLayer(map, { visible = false, opacity = 0.8 } = {}) {
  let lastData = EMPTY;
  let lastMax = 0;
  let curOpacity = opacity;

  function ensure() {
    if (!map.getSource(SRC)) {
      map.addSource(SRC, { type: 'geojson', data: lastData });
    }
    if (!map.getLayer(LYR)) {
      map.addLayer(
        {
          id: LYR,
          type: 'heatmap',
          source: SRC,
          paint: {
            'heatmap-weight': ['coalesce', ['get', 'w'], 0],
            'heatmap-intensity': ['interpolate', ['linear'], ['zoom'], 0, 1, 12, 3],
            'heatmap-radius': ['interpolate', ['linear'], ['zoom'], 0, 8, 12, 24],
            'heatmap-opacity': curOpacity,
            'heatmap-color': [
              'interpolate', ['linear'], ['heatmap-density'],
              0, 'rgba(0,0,255,0)',
              0.2, 'rgba(0,128,255,0.6)',
              0.4, 'rgba(0,255,128,0.7)',
              0.6, 'rgba(255,255,0,0.8)',
              0.8, 'rgba(255,128,0,0.9)',
              1, 'rgba(255,0,0,1)',
            ],
          },
        },
        beforeId(map),
      );
      map.setLayoutProperty(LYR, 'visibility', visible ? 'visible' : 'none');
    }
  }

  function refresh(geojson, maxCount) {
    if (geojson) {
      lastData = { type: 'FeatureCollection', features: normalizeFeatureWeights(geojson.features, maxCount) };
      lastMax = maxCount ?? 0;
    }
    ensure();
    const src = map.getSource(SRC);
    if (src) src.setData(lastData);
  }

  function setVisible(v) {
    visible = v;
    ensure();
    map.setLayoutProperty(LYR, 'visibility', v ? 'visible' : 'none');
  }

  function setOpacity(v) {
    curOpacity = v;
    ensure();
    map.setPaintProperty(LYR, 'heatmap-opacity', v);
  }

  function destroy() {
    if (map.getLayer(LYR)) map.removeLayer(LYR);
    if (map.getSource(SRC)) map.removeSource(SRC);
  }

  ensure();
  return { refresh, setVisible, setOpacity, ensure, destroy, get maxCount() { return lastMax; } };
}
```

- [ ] **Step 2: Verify it lints/builds**

Run: `cd web && npm run build`
Expected: build succeeds (module is imported in Task 9; standalone it must at least parse — if the project has a lint step, run `npm run lint`).

- [ ] **Step 3: Commit**

```bash
git add web/src/lib/map/layers/direct-rx-heatmap.js
git commit -m "Add direct-RX heatmap MapLibre layer module"
```

---

## Task 9: Layer toggle defaults

**Files:**
- Modify: `web/src/lib/map/layer-toggles-core.js`
- Test: `web/src/lib/map/layer-toggles-core.test.js`

- [ ] **Step 1: Add the failing default assertions**

In `web/src/lib/map/layer-toggles-core.test.js`, add:

```javascript
test('defaults include directRxHeatmap off with default opacity', () => {
  assert.equal(LAYER_TOGGLES_DEFAULTS.directRxHeatmap, false);
  assert.equal(LAYER_TOGGLES_DEFAULTS.directRxHeatmapOpacity, 0.8);
});

test('parseLayerToggles supplies heatmap defaults for old blobs', () => {
  const raw = JSON.stringify({ stations: true, trails: false });
  const got = parseLayerToggles(raw);
  assert.equal(got.directRxHeatmap, false);
  assert.equal(got.directRxHeatmapOpacity, 0.8);
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && node --test src/lib/map/layer-toggles-core.test.js`
Expected: FAIL — `directRxHeatmap` is `undefined`.

- [ ] **Step 3: Add the defaults**

In `web/src/lib/map/layer-toggles-core.js`, add two keys to `LAYER_TOGGLES_DEFAULTS` (after `rfOnly`):

```javascript
  directRxHeatmap: false,
  directRxHeatmapOpacity: 0.8,
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd web && node --test src/lib/map/layer-toggles-core.test.js`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/map/layer-toggles-core.js web/src/lib/map/layer-toggles-core.test.js
git commit -m "Add directRxHeatmap toggle defaults"
```

---

## Task 10: Wire the heatmap into LiveMapV2

**Files:**
- Modify: `web/src/routes/LiveMapV2.svelte`

This task has no unit test (Svelte component wiring); verify by `npm run build` and a manual smoke check. Follow the existing radar wiring as the sibling pattern.

- [ ] **Step 1: Import the layer + source**

Near the other layer imports (where `mountRadarLayer` is imported, ~lines 21-26), add:

```javascript
import { mountHeatmapLayer } from '../lib/map/layers/direct-rx-heatmap.js';
import { loadHeatmap } from '../lib/map/sources/heatmap-source.js';
```

- [ ] **Step 2: Declare component state**

Near the other layer handles (where `let radarLayer` is declared), add:

```javascript
let heatmapLayer = null;
let heatmapTimer = null;
```

- [ ] **Step 3: Mount the layer in `onMapReady`**

Where `radarLayer = mountRadarLayer(map, {...})` is set (~line 602), add:

```javascript
heatmapLayer = mountHeatmapLayer(map, {
  visible: layerToggles.directRxHeatmap,
  opacity: layerToggles.directRxHeatmapOpacity,
});
if (layerToggles.directRxHeatmap) refreshHeatmap();
```

- [ ] **Step 4: Add the fetch + scheduling helpers**

Add these functions in the component script (near the other refresh helpers):

```javascript
async function refreshHeatmap() {
  if (!heatmapLayer || !map) return;
  const b = map.getBounds();
  const bbox = { swLat: b.getSouth(), swLon: b.getWest(), neLat: b.getNorth(), neLon: b.getEast() };
  try {
    const { geojson, maxCount } = await loadHeatmap(bbox, timerangeSec);
    heatmapLayer.refresh(geojson, maxCount);
  } catch {
    // transient fetch error; the interval retries
  }
}

function startHeatmapPolling() {
  stopHeatmapPolling();
  heatmapTimer = setInterval(refreshHeatmap, 15000);
}

function stopHeatmapPolling() {
  if (heatmapTimer) {
    clearInterval(heatmapTimer);
    heatmapTimer = null;
  }
}
```

15 s cadence: heat drifts slowly, so this is deliberately slower than the 5 s station poll.

- [ ] **Step 5: React to the toggle, opacity, and viewport/interval**

Add these `$effect` blocks alongside the radar effects:

```javascript
$effect(() => {
  const v = layerToggles.directRxHeatmap;
  heatmapLayer?.setVisible(v);
  if (v) {
    refreshHeatmap();
    startHeatmapPolling();
  } else {
    stopHeatmapPolling();
  }
});

$effect(() => {
  heatmapLayer?.setOpacity(layerToggles.directRxHeatmapOpacity);
});

$effect(() => {
  // refetch when the interval changes while the layer is on
  const _t = timerangeSec;
  if (layerToggles.directRxHeatmap) refreshHeatmap();
});
```

The existing `$effect` that persists `layerToggles` to localStorage already covers the new keys (they are part of the same object), so opacity + toggle survive reloads with no extra code.

- [ ] **Step 6: Refetch on pan/zoom, and re-add after style reload**

Where `map.on('moveend', updateBounds)` is registered (~line 731), add a heatmap refetch:

```javascript
map.on('moveend', () => {
  if (layerToggles.directRxHeatmap) refreshHeatmap();
});
```

In the general refresh `$effect` that calls `radarLayer.refresh()` / `frontsLayer.refresh()` after a basemap `setStyle()` (~lines 866-878), add:

```javascript
  if (heatmapLayer) heatmapLayer.refresh();
```

(Calling `refresh()` with no args re-runs `ensure()` and re-applies the last data, restoring the layer after the style swap.)

- [ ] **Step 7: Add the toggle + opacity UI (desktop card)**

In the APRS section of the layer card, next to the existing "Direct RX" (`directRxOnly`) toggle row, add:

```svelte
<label class="toggle-row">
  <input
    type="checkbox"
    checked={layerToggles.directRxHeatmap}
    onchange={(e) => (layerToggles.directRxHeatmap = e.currentTarget.checked)}
  />
  <span>RX Heatmap</span>
</label>
{#if layerToggles.directRxHeatmap}
  <label class="timerange-label" for="heatmap-opacity-range">
    Heatmap opacity: {Math.round(layerToggles.directRxHeatmapOpacity * 100)}%
  </label>
  <input
    id="heatmap-opacity-range"
    type="range"
    min="0.1"
    max="1.0"
    step="0.05"
    class="radar-opacity-range"
    bind:value={layerToggles.directRxHeatmapOpacity}
  />
{/if}
```

- [ ] **Step 8: Mirror the toggle in the mobile bottom sheet**

Find the bottom-sheet duplicate of the APRS toggles (the mobile drawer renders the same toggle rows) and add the same `RX Heatmap` checkbox row there, binding to `layerToggles.directRxHeatmap` identically. (The opacity slider can live only in the desktop card if the bottom sheet does not host the radar opacity slider; match whatever the radar control does for mobile.)

- [ ] **Step 9: Build and smoke-test**

Run: `cd web && npm run build`
Expected: build succeeds.

Manual check (if a dev environment is available): load the Live Map, toggle **RX Heatmap** on, confirm a heat cloud appears over stations heard directly on RF, confirm the opacity slider changes intensity, confirm toggling off removes it, and confirm it survives a page reload (default off unless toggled).

- [ ] **Step 10: Commit**

```bash
git add web/src/routes/LiveMapV2.svelte
git commit -m "Wire direct-RX heatmap layer into the Live Map"
```

---

## Task 11: Run the full test suites + update the wiki

**Files:**
- Modify: `docs/wiki/` (the Live Map layers page)

- [ ] **Step 1: Run the Go suite**

Run: `go build ./... && go test ./pkg/...`
Expected: PASS.

- [ ] **Step 2: Run the JS suite**

Run: `cd web && npm test`
Expected: PASS (includes the new `heatmap-source.test.js` and the extended `layer-toggles-core.test.js`).

- [ ] **Step 3: Update the wiki**

Find the Live Map layers page under `docs/wiki/` (start at `docs/wiki/README.md`). Add an entry for the direct-RX heatmap describing:
- Frontend: `web/src/lib/map/layers/direct-rx-heatmap.js` (MapLibre heatmap layer), `web/src/lib/map/sources/heatmap-source.js` (fetch + weight normalization), toggle key `directRxHeatmap` / `directRxHeatmapOpacity` in `layer-toggles-core.js`.
- Backend: `GET /api/heatmap` in `pkg/webapi/heatmap.go`, the `rx_events` table + `recordRxEvent` + `QueryHeatmap` in `pkg/historydb/historydb.go`, `HeatPoint`/`HeatmapResult` in `pkg/stationcache/heatmap.go`.
- Invariant: an rx_event is written for every `Direction == "RX"`, non-gated cache entry; digipeated packets attribute to the last H-bit digipeater's station key (`has_pos = 0`, resolved at query time); retention is `rxEventsRetention` (8 days).

- [ ] **Step 4: Commit**

```bash
git add docs/wiki/
git commit -m "Document direct-RX heatmap layer in the wiki"
```

---

## Self-Review Notes

**Spec coverage:**
- Heat = directly received, count-weighted → Tasks 2 (record), 3 (aggregate counts). ✓
- Direct → origin position; digipeated → last-hop digi position → Task 2 attribution + Task 3 resolution. ✓
- IS / gated never contribute → Task 2 skip conditions + test. ✓
- `rx_events` table, retention → Tasks 1, 4. ✓
- Unlocatable reported not dropped → Task 3 `Unlocatable`, Task 6 response `unlocatable`. ✓
- `GET /api/heatmap?bbox&timerange`, GeoJSON with `count`/`max_count`/`unlocatable` → Task 6. ✓
- MapLibre heatmap layer, weight `count/max_count`, below markers → Tasks 7, 8. ✓
- Toggle default off + opacity slider, reuse interval dropdown, persisted → Tasks 9, 10. ✓
- Tests: ingest attribution, endpoint, retention, JS cores → Tasks 2, 3, 4, 6, 7, 9. ✓
- Deferred (signal weighting) → explicitly out of scope, not planned. ✓

**Type consistency:** `HeatPoint{Lat,Lon,Count}` / `HeatmapResult{Points,MaxCount,Unlocatable}` defined in Task 1 and used unchanged in Tasks 3, 5, 6. Method `QueryHeatmap(window time.Duration, bbox stationcache.BBox) (*stationcache.HeatmapResult, error)` is identical across Tasks 3 (impl), 5 (interface + delegate), 6 (consumer). Frontend `normalizeFeatureWeights(features, maxCount)` / `heatmapUrl(bbox, timerangeSec)` / `mountHeatmapLayer(map, {visible, opacity})` are defined in Tasks 7-8 and called with matching shapes in Task 10. Toggle keys `directRxHeatmap` / `directRxHeatmapOpacity` consistent across Tasks 9-10.

**Assumptions to verify during execution (not blockers):**
- `badRequest` helper exists in `pkg/webapi` (used by `stations.go`); if it is named differently, match the local convention.
- Trail layer ids in `beforeId()` (`trails-line`/`trails-dots`/`trails-glow`) — confirm against `layers/trails.js`; if they differ, update the list (a wrong id just means no `beforeId`, layer renders at top of GL stack, still under DOM markers).
- The `HistoryStore` interface and `PersistentCache.mu` field names in `persistent.go` — confirm before editing Task 5.
