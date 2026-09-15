package configstore

import "gorm.io/gorm"

// migrateMessagesRetryInterval adds the retry_interval_secs column to
// messages_preferences (default 30 seconds). Runs post-AutoMigrate because
// the table is created by AutoMigrate, not by an earlier migration.
//
// Guard order:
//  1. If the table does not exist yet (partial-schema test DBs, or any path
//     where AutoMigrate hasn't run), return early — AutoMigrate will create
//     the table with the correct column from the Go struct.
//  2. If the column already exists (AutoMigrate added it on a fresh install),
//     skip the ALTER TABLE.
//  3. Otherwise ALTER TABLE to add the column with DEFAULT 30.
//  4. Backfill any row that ended up with 0 (GORM's SQLite ALTER TABLE ADD
//     COLUMN sometimes omits the DEFAULT clause on existing rows).
func migrateMessagesRetryInterval(tx *gorm.DB) error {
	var tblCount int
	if err := tx.Raw(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='messages_preferences'`,
	).Scan(&tblCount).Error; err != nil {
		return err
	}
	if tblCount == 0 {
		return nil // table absent; AutoMigrate will create it with the right schema
	}
	ok, err := columnExists(tx, "messages_preferences", "retry_interval_secs")
	if err != nil {
		return err
	}
	if !ok {
		if err := tx.Exec(
			`ALTER TABLE messages_preferences ADD COLUMN retry_interval_secs INTEGER NOT NULL DEFAULT 30`,
		).Error; err != nil {
			return err
		}
	}
	return tx.Exec(
		`UPDATE messages_preferences SET retry_interval_secs = 30 WHERE retry_interval_secs = 0`,
	).Error
}
