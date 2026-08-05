package configstore

import (
	"context"
	"testing"
)

func TestMigrateStationConfigSSID_BackfillsEmbeddedSSID(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Insert a raw row with an embedded SSID to exercise the backfill path.
	if err := s.db.WithContext(ctx).Exec(
		"INSERT OR REPLACE INTO station_configs (id, callsign, ssid) VALUES (1, 'KE7XYZ-9', 0)",
	).Error; err != nil {
		t.Fatalf("seed raw row: %v", err)
	}

	if err := migrateStationConfigSSID(s.db.WithContext(ctx)); err != nil {
		t.Fatalf("migrateStationConfigSSID: %v", err)
	}

	got, err := s.GetStationConfig(ctx)
	if err != nil {
		t.Fatalf("GetStationConfig: %v", err)
	}
	if got.Callsign != "KE7XYZ" {
		t.Errorf("Callsign = %q, want KE7XYZ", got.Callsign)
	}
	if got.SSID != 9 {
		t.Errorf("SSID = %d, want 9", got.SSID)
	}
}

func TestMigrateStationConfigSSID_Idempotent(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Running the migration twice on a store that already has the column
	// must not return an error.
	if err := migrateStationConfigSSID(s.db.WithContext(ctx)); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := migrateStationConfigSSID(s.db.WithContext(ctx)); err != nil {
		t.Fatalf("second run (idempotency): %v", err)
	}
}
