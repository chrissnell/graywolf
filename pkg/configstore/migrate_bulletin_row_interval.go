package configstore

import "gorm.io/gorm"

// migrateBulletinRowInterval (version 28) adds the per-bulletin
// interval_mins column to the bulletins table. Existing outbound rows
// are backfilled to 20 (APRS spec default for 2-hop stations).
func migrateBulletinRowInterval(tx *gorm.DB) error {
	var tblCount int
	if err := tx.Raw(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='bulletins'`,
	).Scan(&tblCount).Error; err != nil {
		return err
	}
	if tblCount == 0 {
		return nil
	}
	ok, err := columnExists(tx, "bulletins", "interval_mins")
	if err != nil {
		return err
	}
	if !ok {
		if err := tx.Exec(
			`ALTER TABLE bulletins ADD COLUMN interval_mins INTEGER NOT NULL DEFAULT 20`,
		).Error; err != nil {
			return err
		}
	}
	// Backfill any rows that have 0 (shouldn't happen after ADD COLUMN
	// DEFAULT 20, but guards against edge cases in test DBs).
	return tx.Exec(
		`UPDATE bulletins SET interval_mins = 20 WHERE interval_mins = 0`,
	).Error
}
