package configstore

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateActionsUppercaseNames canonicalizes existing action and remote
// macro names to uppercase. The on-air Action grammar treats names
// case-insensitively (see pkg/configstore Action.BeforeSave and
// pkg/remoteactions RemoteActionMacro.BeforeSave); existing rows from
// before that change may carry mixed-case values that would now miss
// case-normalized lookups.
//
// Idempotent: re-running uppercases already-uppercase strings to the
// same value. The unique index on actions.name could theoretically
// fail if two pre-existing rows differ only in case ("Unlock" and
// "unlock"), but the field's character class is small enough and the
// feature young enough that this collision is not expected in any
// shipped database.
func migrateActionsUppercaseNames(tx *gorm.DB) error {
	if err := tx.Exec(`UPDATE actions SET name = UPPER(name) WHERE name <> UPPER(name)`).Error; err != nil {
		return fmt.Errorf("uppercase actions.name: %w", err)
	}
	// Pre-migration-16 databases lack the remote_action_macros table; the
	// migration runner applies in version order so by the time we get
	// here the table exists. Still gate on a probe to be defensive.
	hasTable := tx.Migrator().HasTable("remote_action_macros")
	if hasTable {
		if err := tx.Exec(`UPDATE remote_action_macros SET action_name = UPPER(action_name) WHERE action_name <> UPPER(action_name)`).Error; err != nil {
			return fmt.Errorf("uppercase remote_action_macros.action_name: %w", err)
		}
	}
	if err := tx.Exec(`UPDATE action_invocations SET action_name_at = UPPER(action_name_at) WHERE action_name_at <> UPPER(action_name_at)`).Error; err != nil {
		return fmt.Errorf("uppercase action_invocations.action_name_at: %w", err)
	}
	return nil
}
