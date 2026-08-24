package configstore

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateChannelsEnabled adds the channels.enabled column (default 1)
// when missing and backfills every existing row to enabled=1. Runs
// post-AutoMigrate so AutoMigrate has already created (or verified) the
// channels table. On fresh installs AutoMigrate adds the column from the
// Go struct and no rows exist, so the ALTER is skipped and the UPDATE is
// a no-op. SQLite's ADD COLUMN with a constant DEFAULT 1 already returns
// 1 for pre-existing rows; the explicit UPDATE is belt-and-suspenders so
// every row carries a written value regardless of how the column landed.
//
// The unconditional backfill is safe: this migration runs exactly once
// (user_version gate) and the enabled flag did not exist before it, so
// no row could have been intentionally disabled yet — every pre-existing
// channel must come up enabled. See graywolf#517.
func migrateChannelsEnabled(tx *gorm.DB) error {
	var tableExists int
	if err := tx.Raw(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='channels'",
	).Scan(&tableExists).Error; err != nil {
		return fmt.Errorf("probe channels: %w", err)
	}
	if tableExists == 0 {
		return nil
	}

	hasCol, err := columnExists(tx, "channels", "enabled")
	if err != nil {
		return fmt.Errorf("probe channels.enabled: %w", err)
	}
	if !hasCol {
		if err := tx.Exec(
			"ALTER TABLE channels ADD COLUMN enabled NUMERIC NOT NULL DEFAULT 1",
		).Error; err != nil {
			return fmt.Errorf("add channels.enabled: %w", err)
		}
	}
	if err := tx.Exec("UPDATE channels SET enabled = 1").Error; err != nil {
		return fmt.Errorf("backfill channels.enabled: %w", err)
	}
	return nil
}
