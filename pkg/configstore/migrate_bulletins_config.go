package configstore

import "gorm.io/gorm"

// migrateBulletinsConfig seeds the bulletins_configs singleton row (id=1)
// with defaults. AutoMigrate creates the table; this migration just ensures
// the row exists so GetBulletinsConfig never races a FirstOrCreate on a
// freshly-opened database.
func migrateBulletinsConfig(tx *gorm.DB) error {
	return tx.Exec(`INSERT OR IGNORE INTO bulletins_configs
		(id, tx_channel, send_path, created_at, updated_at)
		VALUES (1, 0, 'rf', datetime('now'), datetime('now'))`).Error
}
