package configstore

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateBeaconSendPath converts the legacy boolean send_to_aprs_is into
// the send_path enum and drops the old column. AutoMigrate has already
// added send_path (default 'rf') from the struct tag, so every row reads
// 'rf' here; we only need to promote the gated beacons to 'both'.
// Idempotent: a no-op once send_to_aprs_is is gone. Mirrors
// migrateBeaconPositionFormat (migration 23).
func migrateBeaconSendPath(tx *gorm.DB) error {
	has, err := columnExists(tx, "beacons", "send_to_aprs_is")
	if err != nil {
		return fmt.Errorf("probe beacons.send_to_aprs_is: %w", err)
	}
	if !has {
		return nil
	}
	if err := tx.Exec(
		`UPDATE beacons SET send_path = 'both' WHERE send_to_aprs_is = 1`,
	).Error; err != nil {
		return fmt.Errorf("backfill send_path: %w", err)
	}
	if err := tx.Exec(`ALTER TABLE beacons DROP COLUMN send_to_aprs_is`).Error; err != nil {
		return fmt.Errorf("drop send_to_aprs_is: %w", err)
	}
	return nil
}
