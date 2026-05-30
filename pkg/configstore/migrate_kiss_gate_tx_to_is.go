package configstore

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateKissGateTxToIs adds the gate_tx_to_is BOOL column to
// kiss_interfaces with default false. Idempotent: the pragma_table_info
// probe short-circuits if the column already exists. On fresh databases
// AutoMigrate already created the column from the updated KissInterface
// struct tags, so this is a no-op there.
func migrateKissGateTxToIs(tx *gorm.DB) error {
	var tableExists int
	if err := tx.Raw(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='kiss_interfaces'",
	).Scan(&tableExists).Error; err != nil {
		return fmt.Errorf("probe kiss_interfaces: %w", err)
	}
	if tableExists == 0 {
		return nil
	}

	var present int
	if err := tx.Raw(
		"SELECT COUNT(*) FROM pragma_table_info('kiss_interfaces') WHERE name='gate_tx_to_is'",
	).Scan(&present).Error; err != nil {
		return fmt.Errorf("probe gate_tx_to_is: %w", err)
	}
	if present > 0 {
		return nil
	}

	if err := tx.Exec(
		"ALTER TABLE kiss_interfaces ADD COLUMN gate_tx_to_is NUMERIC NOT NULL DEFAULT 0",
	).Error; err != nil {
		return fmt.Errorf("add gate_tx_to_is: %w", err)
	}
	return nil
}
