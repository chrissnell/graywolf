package historydb

import (
	"path/filepath"
	"testing"
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
