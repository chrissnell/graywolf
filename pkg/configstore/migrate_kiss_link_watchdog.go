package configstore

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateKissLinkWatchdog adds the link_watchdog (TEXT, default 'auto')
// and idle_reconnect_sec (INTEGER, default 0) columns to kiss_interfaces.
// Idempotent: each column is probed first. On fresh databases AutoMigrate
// already created both from the KissInterface struct tags, so this is a
// no-op there.
func migrateKissLinkWatchdog(tx *gorm.DB) error {
	var tableExists int
	if err := tx.Raw(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='kiss_interfaces'",
	).Scan(&tableExists).Error; err != nil {
		return fmt.Errorf("probe kiss_interfaces: %w", err)
	}
	if tableExists == 0 {
		return nil
	}

	cols := []struct{ name, ddl string }{
		{"link_watchdog", "ALTER TABLE kiss_interfaces ADD COLUMN link_watchdog TEXT NOT NULL DEFAULT 'auto'"},
		{"idle_reconnect_sec", "ALTER TABLE kiss_interfaces ADD COLUMN idle_reconnect_sec INTEGER NOT NULL DEFAULT 0"},
	}
	for _, c := range cols {
		var present int
		if err := tx.Raw(
			"SELECT COUNT(*) FROM pragma_table_info('kiss_interfaces') WHERE name=?", c.name,
		).Scan(&present).Error; err != nil {
			return fmt.Errorf("probe %s: %w", c.name, err)
		}
		if present > 0 {
			continue
		}
		if err := tx.Exec(c.ddl).Error; err != nil {
			return fmt.Errorf("add %s: %w", c.name, err)
		}
	}
	return nil
}
