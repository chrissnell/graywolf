package configstore

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateChannelsMode adds the channels.mode column (default 'aprs')
// when missing. Runs post-AutoMigrate so AutoMigrate has already created
// (or verified) the channels table. On fresh installs AutoMigrate adds
// the column from the Go struct and this migration is a no-op; on
// legacy databases where AutoMigrate did not add the column (e.g. if it
// was added to the struct in a later binary than the one that last ran),
// this migration applies it explicitly via ALTER TABLE ADD COLUMN DEFAULT
// which SQLite 3.35+ back-fills onto existing rows.
//
// See docs/superpowers/plans/2026-05-01-ax25-terminal.md §0.2 for why
// 'aprs' is the migrated-row default (preserve current behavior).
func migrateChannelsMode(tx *gorm.DB) error {
	hasCol, err := columnExists(tx, "channels", "mode")
	if err != nil {
		return fmt.Errorf("probe channels.mode: %w", err)
	}
	if hasCol {
		return nil
	}
	stmt := `ALTER TABLE channels ADD COLUMN mode TEXT NOT NULL DEFAULT 'aprs'`
	if err := tx.Exec(stmt).Error; err != nil {
		return fmt.Errorf("add channels.mode: %w", err)
	}
	return nil
}
