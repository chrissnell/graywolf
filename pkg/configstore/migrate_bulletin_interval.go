package configstore

import "gorm.io/gorm"

// migrateBulletinInterval adds the bulletin_interval_mins column to
// messages_preferences (default 20 minutes per APRS spec). Runs
// post-AutoMigrate because the table is created by AutoMigrate.
//
// Guard order mirrors migrate_messages_retry_interval.go:
//  1. Table absent → return early; AutoMigrate will create it correctly.
//  2. Column exists → skip ALTER TABLE.
//  3. Otherwise ALTER TABLE ADD COLUMN DEFAULT 20.
//  4. Backfill any row still at 0 (GORM SQLite ALTER TABLE sometimes
//     omits the DEFAULT on existing rows).
func migrateBulletinInterval(tx *gorm.DB) error {
	var tblCount int
	if err := tx.Raw(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='messages_preferences'`,
	).Scan(&tblCount).Error; err != nil {
		return err
	}
	if tblCount == 0 {
		return nil
	}
	ok, err := columnExists(tx, "messages_preferences", "bulletin_interval_mins")
	if err != nil {
		return err
	}
	if !ok {
		if err := tx.Exec(
			`ALTER TABLE messages_preferences ADD COLUMN bulletin_interval_mins INTEGER NOT NULL DEFAULT 20`,
		).Error; err != nil {
			return err
		}
	}
	return tx.Exec(
		`UPDATE messages_preferences SET bulletin_interval_mins = 20 WHERE bulletin_interval_mins = 0`,
	).Error
}
