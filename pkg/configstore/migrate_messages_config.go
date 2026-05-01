package configstore

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateMessagesConfigCopyFromIgate creates messages_configs (id=1)
// and seeds TxChannel from i_gate_configs.tx_channel when the column
// is still present. Runs post-AutoMigrate so the messages_configs
// table exists. Skips the copy when a non-empty messages_configs
// already exists.
//
// LOAD-BEARING: this migration depends on i_gate_configs.tx_channel
// surviving AutoMigrate. The IGateConfig model retains TxChannel for
// IS->RF gating, so the column must not be removed without first
// deleting this migration. See the matching comment on the
// IGateConfig.TxChannel struct field.
//
// See docs/superpowers/plans/2026-05-01-ax25-terminal.md §0.8.
func migrateMessagesConfigCopyFromIgate(tx *gorm.DB) error {
	var count int64
	if err := tx.Table("messages_configs").Count(&count).Error; err != nil {
		return fmt.Errorf("count messages_configs: %w", err)
	}
	if count > 0 {
		return nil
	}
	hasCol, err := columnExists(tx, "i_gate_configs", "tx_channel")
	if err != nil {
		return fmt.Errorf("probe i_gate_configs.tx_channel: %w", err)
	}
	var igTx uint32
	if hasCol {
		if err := tx.Raw(`SELECT tx_channel FROM i_gate_configs WHERE id=1`).Scan(&igTx).Error; err != nil {
			// igate row may not exist on a fresh install -- seed empty.
			igTx = 0
		}
	} else {
		// Column removed by a later migration without first deleting
		// this one. Loud-log so operators notice the lost setting
		// rather than silently defaulting to 0.
		fmt.Printf("WARN: migrate_messages_config: i_gate_configs.tx_channel absent; seeding messages_configs.tx_channel=0. Operators must reselect TX channel under Messages preferences.\n")
	}
	if err := tx.Exec(
		`INSERT INTO messages_configs (id, tx_channel, created_at, updated_at)
		 VALUES (1, ?, datetime('now'), datetime('now'))`, igTx).Error; err != nil {
		return fmt.Errorf("seed messages_configs: %w", err)
	}
	return nil
}
