package configstore

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateIGateIsTxVia replaces the legacy i_gate_configs.max_msg_hops
// column (an inert WIDE-hop count that never reached the transmit path,
// issue #489) with the new is_tx_via literal via-path string. AutoMigrate
// has already added is_tx_via (empty default) from the struct tag, so
// every existing row reads empty — direct, no path — which exactly
// preserves the pre-fix behavior (IS->RF frames were always sent direct
// regardless of max_msg_hops). No value backfill is therefore needed; we
// only drop the dead column. Idempotent: a no-op once max_msg_hops is
// gone. Mirrors migrateBeaconSendPath (migration 25).
func migrateIGateIsTxVia(tx *gorm.DB) error {
	has, err := columnExists(tx, "i_gate_configs", "max_msg_hops")
	if err != nil {
		return fmt.Errorf("probe i_gate_configs.max_msg_hops: %w", err)
	}
	if !has {
		return nil
	}
	if err := tx.Exec(`ALTER TABLE i_gate_configs DROP COLUMN max_msg_hops`).Error; err != nil {
		return fmt.Errorf("drop max_msg_hops: %w", err)
	}
	return nil
}
