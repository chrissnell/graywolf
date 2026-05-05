package configstore

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateActionsMaxReplyLines adds two columns:
//
//   - actions.max_reply_lines: per-Action ceiling on the number of
//     stdout lines that get sent as separate APRS messages on success.
//     Default 1 preserves pre-change behavior. NOT NULL so the
//     application layer never has to check a sentinel.
//   - action_invocations.reply_line_count: how many lines were actually
//     dispatched for the audit row. Default 1 backfills legacy rows.
//
// Idempotent in the same shape as migrateActionsArgMode: a column
// probe via columnExists short-circuits the ALTER on rerun.
func migrateActionsMaxReplyLines(tx *gorm.DB) error {
	type colDef struct{ table, col, sql string }
	defs := []colDef{
		{"actions", "max_reply_lines", `ALTER TABLE actions ADD COLUMN max_reply_lines INTEGER NOT NULL DEFAULT 1`},
		{"action_invocations", "reply_line_count", `ALTER TABLE action_invocations ADD COLUMN reply_line_count INTEGER NOT NULL DEFAULT 1`},
	}
	for _, d := range defs {
		hasCol, err := columnExists(tx, d.table, d.col)
		if err != nil {
			return fmt.Errorf("probe %s.%s: %w", d.table, d.col, err)
		}
		if hasCol {
			continue
		}
		if err := tx.Exec(d.sql).Error; err != nil {
			return fmt.Errorf("add %s.%s: %w", d.table, d.col, err)
		}
	}
	return nil
}
