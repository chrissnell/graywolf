package configstore

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateAX25TerminalTables seeds the AX25TerminalConfig singleton row
// (id=1) on the first run after the table exists. AutoMigrate creates
// the four ax25-terminal tables (terminal-config, profiles, transcript
// sessions, transcript entries) directly from the Go structs in models.go;
// this migration only handles the singleton seed so the REST GET handler
// always finds a row to render.
//
// Idempotent: probes for the existing singleton before inserting.
//
// See docs/superpowers/plans/2026-05-01-ax25-terminal.md §3c.1.
func migrateAX25TerminalTables(tx *gorm.DB) error {
	var tableExists int
	if err := tx.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='ax25_terminal_configs'").Scan(&tableExists).Error; err != nil {
		return fmt.Errorf("probe ax25_terminal_configs: %w", err)
	}
	if tableExists == 0 {
		// Table not yet created (this would only happen if migration
		// ordering somehow skipped AutoMigrate). Nothing to seed.
		return nil
	}
	var rowCount int64
	if err := tx.Table("ax25_terminal_configs").Where("id = 1").Count(&rowCount).Error; err != nil {
		return fmt.Errorf("count terminal-config singleton: %w", err)
	}
	if rowCount > 0 {
		return nil
	}
	row := AX25TerminalConfig{
		ID:             1,
		ScrollbackRows: 1000,
		CursorBlink:    false,
		DefaultModulo:  8,
		DefaultPaclen:  256,
		MacrosJSON:     "[]",
		RawTailFilter:  "",
	}
	if err := tx.Create(&row).Error; err != nil {
		return fmt.Errorf("seed terminal-config singleton: %w", err)
	}
	return nil
}
