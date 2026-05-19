package configstore

import (
	"fmt"

	"gorm.io/gorm"
)

// migratePttAndroidMethodField moves the Android PTT transport int out of
// the overloaded gpio_pin field into the dedicated ptt_method column.
// Idempotent: the `ptt_method = 0` guard means an already-migrated row
// (ptt_method is always 1..4 afterward) is never re-touched, so a rerun
// cannot re-coerce a valid value. Malformed legacy rows (gpio_pin not in
// 1..4) coerce to 1 (PTT_METHOD_CP2102N_RTS, the historical UI fallback).
// One-way only; safe because graywolf is a single-user station.
func migratePttAndroidMethodField(tx *gorm.DB) error {
	var tableExists int
	if err := tx.Raw(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='ptt_configs'",
	).Scan(&tableExists).Error; err != nil {
		return fmt.Errorf("probe ptt_configs: %w", err)
	}
	if tableExists == 0 {
		return nil
	}
	return tx.Exec(
		`UPDATE ptt_configs ` +
			`SET ptt_method = CASE WHEN gpio_pin BETWEEN 1 AND 4 THEN gpio_pin ELSE 1 END, ` +
			`    gpio_pin = 0 ` +
			`WHERE method = 'android' AND ptt_method = 0`,
	).Error
}
