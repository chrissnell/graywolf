package historydb

import (
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/stationcache"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(t.TempDir() + "/history.db")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

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

func TestRecordRxEventPersists(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().Truncate(time.Second)

	if err := db.RecordRxEvent(stationcache.RxEvent{
		Timestamp: now, AttrKey: "stn:W1ABC", Hops: 0, Lat: 35, Lon: -95, HasPos: true,
	}); err != nil {
		t.Fatalf("RecordRxEvent direct: %v", err)
	}
	if err := db.RecordRxEvent(stationcache.RxEvent{
		Timestamp: now, AttrKey: "stn:N0DIGI", Hops: 1, HasPos: false,
	}); err != nil {
		t.Fatalf("RecordRxEvent digi: %v", err)
	}

	got := loadRxEvents(t, db)
	if len(got) != 2 {
		t.Fatalf("expected 2 rx_events, got %d: %+v", len(got), got)
	}
	if got[0] != (rxEventRow{AttrKey: "stn:W1ABC", Hops: 0, Lat: 35, Lon: -95, HasPos: true}) {
		t.Errorf("direct event wrong: %+v", got[0])
	}
	if got[1] != (rxEventRow{AttrKey: "stn:N0DIGI", Hops: 1, Lat: 0, Lon: 0, HasPos: false}) {
		t.Errorf("digi event wrong: %+v", got[1])
	}
}

// TestWriteEntriesDoesNotRecordRxEvents locks in the fix for the heatmap
// double-count: counting must live at the RF-ingest edge, never in the shared
// cache write path (which also runs for the iGate RF->IS re-gate and the
// startup roster reload). If someone re-couples recording to WriteEntries this
// fails.
func TestWriteEntriesDoesNotRecordRxEvents(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().Truncate(time.Second)
	if err := db.WriteEntries([]stationcache.CacheEntry{
		{Key: "stn:W1ABC", Callsign: "W1ABC", Direction: "RX", Lat: 35, Lon: -95, HasPos: true, Timestamp: now},
		{Key: "stn:W2DEF", Callsign: "W2DEF", Direction: "RX", Lat: 40, Lon: -80, HasPos: true, Path: []string{"N0DIGI*"}, Hops: 1, Timestamp: now},
	}); err != nil {
		t.Fatalf("WriteEntries: %v", err)
	}
	if got := loadRxEvents(t, db); len(got) != 0 {
		t.Fatalf("WriteEntries must not write rx_events, got %d: %+v", len(got), got)
	}
}

func fmtKey(lat, lon float64) string {
	return heatBucketKey(lat, lon)
}

func TestQueryHeatmapAggregates(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().Truncate(time.Second)

	// N0DIGI must have a stored position so a digipeated event attributed to it
	// resolves. WriteEntries is the position-persist path.
	if err := db.WriteEntries([]stationcache.CacheEntry{
		{Key: "stn:N0DIGI", Callsign: "N0DIGI", Direction: "RX", Lat: 36, Lon: -96, HasPos: true, Timestamp: now},
	}); err != nil {
		t.Fatalf("WriteEntries: %v", err)
	}

	events := []stationcache.RxEvent{
		// Fixed station heard directly 3x from the same spot -> count 3.
		{Timestamp: now, AttrKey: "stn:W1ABC", Hops: 0, Lat: 35, Lon: -95, HasPos: true},
		{Timestamp: now, AttrKey: "stn:W1ABC", Hops: 0, Lat: 35, Lon: -95, HasPos: true},
		{Timestamp: now, AttrKey: "stn:W1ABC", Hops: 0, Lat: 35, Lon: -95, HasPos: true},
		// N0DIGI's own direct beacon (resolvable position).
		{Timestamp: now, AttrKey: "stn:N0DIGI", Hops: 0, Lat: 36, Lon: -96, HasPos: true},
		// Digipeated via N0DIGI -> resolved to N0DIGI's location.
		{Timestamp: now, AttrKey: "stn:N0DIGI", Hops: 1, HasPos: false},
		// Digipeated via an unknown digi -> unlocatable.
		{Timestamp: now, AttrKey: "stn:NOPOS", Hops: 1, HasPos: false},
	}
	for _, ev := range events {
		if err := db.RecordRxEvent(ev); err != nil {
			t.Fatalf("RecordRxEvent: %v", err)
		}
	}

	bbox := stationcache.BBox{SwLat: 30, SwLon: -100, NeLat: 45, NeLon: -70}
	res, err := db.QueryHeatmap(time.Hour, bbox)
	if err != nil {
		t.Fatalf("QueryHeatmap: %v", err)
	}
	if res.Unlocatable != 1 {
		t.Errorf("Unlocatable = %d, want 1", res.Unlocatable)
	}
	counts := map[string]int{}
	for _, p := range res.Points {
		counts[fmtKey(p.Lat, p.Lon)] = p.Count
	}
	if counts[fmtKey(35, -95)] != 3 {
		t.Errorf("W1ABC bucket = %d, want 3", counts[fmtKey(35, -95)])
	}
	if counts[fmtKey(36, -96)] != 2 {
		t.Errorf("N0DIGI bucket = %d, want 2 (own beacon + digipeated)", counts[fmtKey(36, -96)])
	}
	if res.MaxCount != 3 {
		t.Errorf("MaxCount = %d, want 3", res.MaxCount)
	}
}

func TestQueryHeatmapBBoxFilter(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().Truncate(time.Second)
	for _, ev := range []stationcache.RxEvent{
		{Timestamp: now, AttrKey: "stn:IN", Lat: 35, Lon: -95, HasPos: true},
		{Timestamp: now, AttrKey: "stn:OUT", Lat: 10, Lon: 10, HasPos: true},
	} {
		if err := db.RecordRxEvent(ev); err != nil {
			t.Fatalf("RecordRxEvent: %v", err)
		}
	}
	res, err := db.QueryHeatmap(time.Hour, stationcache.BBox{SwLat: 30, SwLon: -100, NeLat: 45, NeLon: -70})
	if err != nil {
		t.Fatalf("QueryHeatmap: %v", err)
	}
	if len(res.Points) != 1 || res.Points[0].Count != 1 {
		t.Fatalf("expected 1 in-bbox point, got %+v", res.Points)
	}
}

func TestPruneDropsOldRxEvents(t *testing.T) {
	db := openTestDB(t)
	old := time.Now().Add(-rxEventsRetention - time.Hour)
	recent := time.Now().Add(-time.Hour)

	for _, ev := range []stationcache.RxEvent{
		{Timestamp: old, AttrKey: "stn:OLD", Lat: 35, Lon: -95, HasPos: true},
		{Timestamp: recent, AttrKey: "stn:NEW", Lat: 35, Lon: -95, HasPos: true},
	} {
		if err := db.RecordRxEvent(ev); err != nil {
			t.Fatalf("RecordRxEvent: %v", err)
		}
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
