package configstore

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateBeaconQsyToneFreq adds the beacons.tone_freq column (no default,
// matching the sibling freq/tone/freq_offset columns it travels with).
// AutoMigrate already adds it from the Go struct tag on a fresh install;
// this migration is the documented, version-tracked record of the
// addition and a belt-and-suspenders ALTER for any DB opened with an
// older AutoMigrate pass. No backfill: tone_freq is a brand-new concept
// with nothing to derive from existing columns (unlike migration 23's
// compress->position_format conversion). See the QSY feature plan.
func migrateBeaconQsyToneFreq(tx *gorm.DB) error {
	hasCol, err := columnExists(tx, "beacons", "tone_freq")
	if err != nil {
		return fmt.Errorf("probe beacons.tone_freq: %w", err)
	}
	if hasCol {
		return nil
	}
	if err := tx.Exec("ALTER TABLE beacons ADD COLUMN tone_freq TEXT").Error; err != nil {
		return fmt.Errorf("add beacons.tone_freq: %w", err)
	}
	return nil
}
