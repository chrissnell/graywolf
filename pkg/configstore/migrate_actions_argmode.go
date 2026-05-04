package configstore

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateActionsArgMode adds the arg_mode column to the actions table.
// Default 'kv' preserves existing-row behavior. The column is NOT NULL
// so application code can always read it without a sentinel check.
//
// Idempotent: if the column already exists (e.g. AutoMigrate added it
// from the model in a later-binary first run, or the test harness
// rolled user_version back to re-fire migrations), the ALTER is
// skipped. Rerun safety matches the channel_mode migration pattern.
func migrateActionsArgMode(tx *gorm.DB) error {
	hasCol, err := columnExists(tx, "actions", "arg_mode")
	if err != nil {
		return fmt.Errorf("probe actions.arg_mode: %w", err)
	}
	if hasCol {
		return nil
	}
	if err := tx.Exec(`ALTER TABLE actions ADD COLUMN arg_mode TEXT NOT NULL DEFAULT 'kv'`).Error; err != nil {
		return fmt.Errorf("add actions.arg_mode: %w", err)
	}
	return nil
}
