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

func fmtKey(lat, lon float64) string {
	return heatBucketKey(lat, lon)
}

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
