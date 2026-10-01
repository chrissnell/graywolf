package configstore

import (
	"path/filepath"
	"testing"
)

// TestMigrateBeaconQsyToneFreq_AddsColumn exercises migrateBeaconQsyToneFreq
// directly on a database that pre-dates the tone_freq column: drops the
// column AutoMigrate already added, invokes the migration body, and
// asserts the column comes back. There is no backfill to assert (unlike
// migrateChannelsEnabled) — tone_freq is a brand-new concept with no
// legacy data to derive. Going through the body directly is the only way
// to reach the ADD COLUMN branch — a re-Open would let AutoMigrate add
// the column from the Go struct first.
func TestMigrateBeaconQsyToneFreq_AddsColumn(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "beacon_qsy_tone_freq.db")
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	if err := store.DB().Exec(`ALTER TABLE beacons DROP COLUMN tone_freq`).Error; err != nil {
		t.Fatalf("drop tone_freq column: %v", err)
	}

	hasCol, err := columnExists(store.DB(), "beacons", "tone_freq")
	if err != nil {
		t.Fatalf("probe pre-migration: %v", err)
	}
	if hasCol {
		t.Fatalf("pre-migration: tone_freq column unexpectedly present")
	}

	if err := migrateBeaconQsyToneFreq(store.DB()); err != nil {
		t.Fatalf("migrateBeaconQsyToneFreq: %v", err)
	}

	hasCol, err = columnExists(store.DB(), "beacons", "tone_freq")
	if err != nil {
		t.Fatalf("probe post-migration: %v", err)
	}
	if !hasCol {
		t.Fatalf("post-migration: tone_freq column still missing")
	}

	// Idempotence: a second invocation must be a no-op.
	if err := migrateBeaconQsyToneFreq(store.DB()); err != nil {
		t.Fatalf("second invocation: %v", err)
	}
}
