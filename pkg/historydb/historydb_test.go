package historydb

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/stationcache"
)

// TestRainBackfillMigration verifies the issue #126 one-time backfill:
// legacy rows persisted rain in raw APRS101 hundredths; bootstrap must
// convert them to inches exactly once, gated on PRAGMA user_version so
// a reboot never double-divides.
func TestRainBackfillMigration(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// Simulate a pre-fix database: a legacy weather row carrying raw
	// hundredths, with user_version rewound to 0.
	if err := db.db.Exec(`INSERT INTO stations (key, callsign, symbol, last_heard)
		VALUES ('WX1ABC', 'WX1ABC', x'2F5F', CURRENT_TIMESTAMP)`).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}
	if err := db.db.Exec(`INSERT INTO weather (station_key, rain_1h, has_rain_1h, rain_24h, has_rain_24h)
		VALUES ('WX1ABC', 137, 1, 250, 1)`).Error; err != nil {
		t.Fatalf("seed weather: %v", err)
	}
	if err := db.db.Exec(`PRAGMA user_version = 0`).Error; err != nil {
		t.Fatalf("rewind user_version: %v", err)
	}

	readRain := func() (r1, r24 float64) {
		row := db.db.Raw(`SELECT rain_1h, rain_24h FROM weather WHERE station_key = 'WX1ABC'`).Row()
		if err := row.Scan(&r1, &r24); err != nil {
			t.Fatalf("read rain: %v", err)
		}
		return
	}

	// Fail-before is implicit: the seeded values are 137 / 250.
	if err := bootstrap(db.db); err != nil {
		t.Fatalf("bootstrap (migrate): %v", err)
	}
	if r1, r24 := readRain(); r1 != 1.37 || r24 != 2.5 {
		t.Fatalf("after migration: rain_1h=%v rain_24h=%v, want 1.37 / 2.5", r1, r24)
	}

	var uv int
	if err := db.db.Raw("PRAGMA user_version").Row().Scan(&uv); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if uv != 1 {
		t.Fatalf("user_version = %d, want 1", uv)
	}

	// Idempotent: a second bootstrap must not divide again.
	if err := bootstrap(db.db); err != nil {
		t.Fatalf("bootstrap (reboot): %v", err)
	}
	if r1, r24 := readRain(); r1 != 1.37 || r24 != 2.5 {
		t.Fatalf("after reboot: rain_1h=%v rain_24h=%v, want unchanged 1.37 / 2.5", r1, r24)
	}
}

// TestStaticRebeaconDirectNotMasked is the persistence-layer counterpart to
// issue #130: a static station heard directly (hops 0) then via a digipeater
// (hops > 0) must keep the direct reception on its single stored position so
// hydration after a restart still satisfies the Direct RX filter.
func TestStaticRebeaconDirectNotMasked(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	base := time.Now().Add(-time.Minute)
	direct := stationcache.CacheEntry{
		Key: "stn:DIGI1", Callsign: "DIGI1", HasPos: true,
		Lat: 40.0, Lon: -105.0, Symbol: [2]byte{'/', '#'},
		Via: "", Direction: "RX", Hops: 0, Channel: 0, Comment: "direct",
		Timestamp: base,
	}
	digipeated := direct
	digipeated.Via = "WIDE2-1"
	digipeated.Path = []string{"DIGI2*", "WIDE2-1"}
	digipeated.Hops = 2
	digipeated.Comment = "via digi"
	digipeated.Timestamp = base.Add(time.Second)

	if err := db.WriteEntries([]stationcache.CacheEntry{direct}); err != nil {
		t.Fatalf("write direct: %v", err)
	}
	if err := db.WriteEntries([]stationcache.CacheEntry{digipeated}); err != nil {
		t.Fatalf("write digipeated: %v", err)
	}

	stations, err := db.LoadRecent(time.Hour, 200)
	if err != nil {
		t.Fatalf("load recent: %v", err)
	}
	s := stations["stn:DIGI1"]
	if s == nil || len(s.Positions) != 1 {
		t.Fatalf("expected 1 station with 1 position, got %+v", s)
	}
	p := s.Positions[0]
	if p.Direction != "RX" || p.Hops != 0 {
		t.Fatalf("direct reception masked in DB: Direction=%q Hops=%d", p.Direction, p.Hops)
	}
	// Timestamp must still advance to the latest re-beacon.
	if !p.Timestamp.Equal(digipeated.Timestamp) {
		t.Fatalf("timestamp not advanced: got %v want %v", p.Timestamp, digipeated.Timestamp)
	}
}

// TestStaticRebeaconUpgradeAndLatestWins pins the other two transition
// directions of the issue #130 SQL CASE guard: a digipeated fix is upgraded
// to direct when a direct copy arrives, and among non-direct copies the
// latest one still wins. Together with TestStaticRebeaconDirectNotMasked
// this locks the guard against an inverted boolean binding.
func TestStaticRebeaconUpgradeAndLatestWins(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	base := time.Now().Add(-time.Minute)
	mk := func(dir string, hops int, via, comment string, ts time.Time) stationcache.CacheEntry {
		e := stationcache.CacheEntry{
			Key: "stn:DIGI1", Callsign: "DIGI1", HasPos: true,
			Lat: 40.0, Lon: -105.0, Symbol: [2]byte{'/', '#'},
			Via: via, Direction: dir, Hops: hops, Comment: comment, Timestamp: ts,
		}
		if hops > 0 {
			e.Path = []string{"DIGI2*", "WIDE2-1"}
		}
		return e
	}

	load := func() stationcache.Position {
		stations, err := db.LoadRecent(time.Hour, 200)
		if err != nil {
			t.Fatalf("load recent: %v", err)
		}
		s := stations["stn:DIGI1"]
		if s == nil || len(s.Positions) != 1 {
			t.Fatalf("expected 1 station with 1 position, got %+v", s)
		}
		return s.Positions[0]
	}

	// Heard via a digipeater first, then directly: must upgrade to direct.
	if err := db.WriteEntries([]stationcache.CacheEntry{mk("RX", 2, "WIDE2-1", "via digi", base)}); err != nil {
		t.Fatalf("write digipeated: %v", err)
	}
	if err := db.WriteEntries([]stationcache.CacheEntry{mk("RX", 0, "", "direct", base.Add(time.Second))}); err != nil {
		t.Fatalf("write direct: %v", err)
	}
	if p := load(); p.Direction != "RX" || p.Hops != 0 {
		t.Fatalf("direct copy did not upgrade fix: Direction=%q Hops=%d", p.Direction, p.Hops)
	}

	// Among non-direct copies (no direct ever heard), latest still wins.
	db2, err := Open(filepath.Join(t.TempDir(), "h2.db"))
	if err != nil {
		t.Fatalf("open h2: %v", err)
	}
	defer db2.Close()
	if err := db2.WriteEntries([]stationcache.CacheEntry{mk("RX", 2, "WIDE2-1", "first digi", base)}); err != nil {
		t.Fatalf("write first digi: %v", err)
	}
	if err := db2.WriteEntries([]stationcache.CacheEntry{mk("IS", 0, "is", "from aprs-is", base.Add(time.Second))}); err != nil {
		t.Fatalf("write IS: %v", err)
	}
	stations, err := db2.LoadRecent(time.Hour, 200)
	if err != nil {
		t.Fatalf("load recent h2: %v", err)
	}
	p := stations["stn:DIGI1"].Positions[0]
	if p.Direction != "IS" || p.Hops != 0 {
		t.Fatalf("latest non-direct copy did not win: Direction=%q Hops=%d", p.Direction, p.Hops)
	}
}
