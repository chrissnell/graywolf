package configstore

import "gorm.io/gorm"

func migrateBulletinsTable(tx *gorm.DB) error {
	if err := tx.Exec(`
CREATE TABLE IF NOT EXISTS bulletins (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	direction       TEXT    NOT NULL DEFAULT 'in',
	slot            TEXT    NOT NULL,
	from_call       TEXT    NOT NULL DEFAULT '',
	text            TEXT    NOT NULL DEFAULT '',
	source          TEXT             DEFAULT '',
	channel         INTEGER NOT NULL DEFAULT 0,
	raw_tnc2        TEXT             DEFAULT '',
	is_announcement INTEGER NOT NULL DEFAULT 0,
	expires_at      DATETIME,
	next_send_at    DATETIME,
	send_count      INTEGER NOT NULL DEFAULT 0,
	max_sends       INTEGER NOT NULL DEFAULT 12,
	unread          INTEGER NOT NULL DEFAULT 1,
	created_at      DATETIME NOT NULL DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW')),
	updated_at      DATETIME NOT NULL DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW')),
	deleted_at      DATETIME
)`).Error; err != nil {
		return err
	}
	// Unique index for inbound upsert: one row per (from_call, slot) per active inbound.
	if err := tx.Exec(`
CREATE UNIQUE INDEX IF NOT EXISTS idx_bulletin_inbound_slot
	ON bulletins (from_call, slot)
	WHERE direction = 'in' AND deleted_at IS NULL`).Error; err != nil {
		return err
	}
	// General list index.
	if err := tx.Exec(`
CREATE INDEX IF NOT EXISTS idx_bulletin_direction
	ON bulletins (direction, deleted_at)`).Error; err != nil {
		return err
	}
	// Outbound scheduler index.
	return tx.Exec(`
CREATE INDEX IF NOT EXISTS idx_bulletin_next_send
	ON bulletins (next_send_at)
	WHERE direction = 'out' AND deleted_at IS NULL`).Error
}
