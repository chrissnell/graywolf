package configstore

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateBeaconPositionFormat backfills the new beacons.position_format
// column from the legacy beacons.compress boolean and then drops
// compress. Runs post-AutoMigrate: AutoMigrate has already added
// position_format with default 'compressed' (from the GORM struct tag),
// so this migration only needs to flip rows where compress=0 to
// 'uncompressed' and then remove the legacy column.
//
// Idempotent via the user_version gate (it runs exactly once per DB)
// and via the columnExists probe (a fresh DB AutoMigrate created
// without the legacy column is a no-op).
func migrateBeaconPositionFormat(tx *gorm.DB) error {
	hasCompress, err := columnExists(tx, "beacons", "compress")
	if err != nil {
		return fmt.Errorf("probe beacons.compress: %w", err)
	}
	if !hasCompress {
		return nil
	}
	if err := tx.Exec(
		`UPDATE beacons SET position_format = 'uncompressed' WHERE compress = 0`,
	).Error; err != nil {
		return fmt.Errorf("backfill uncompressed: %w", err)
	}
	if err := tx.Exec(`ALTER TABLE beacons DROP COLUMN compress`).Error; err != nil {
		return fmt.Errorf("drop compress: %w", err)
	}
	return nil
}
