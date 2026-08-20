package configstore

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateWeatherCountyPrefFIPS renames the f_ip_s column to fips in
// weather_county_prefs. GORM split the all-caps FIPS field on the "IP"
// common initialism, creating f_ip_s instead of the intended fips. The
// struct now carries an explicit column tag; this migration fixes existing
// databases. Idempotent: no-op when f_ip_s is already gone.
//
// This is a selfTxn migration. On fresh databases (f_ip_s never existed,
// AutoMigrate will create the correct fips column) it returns without
// bumping user_version so that postAutoMigrate migrations with version
// numbers < 31 (e.g. migration 11 which adds callsign/passcode to
// i_gate_configs) are not skipped. Only databases that actually need the
// rename bump user_version to 31.
func migrateWeatherCountyPrefFIPS(db *gorm.DB) error {
	has, err := columnExists(db, "weather_county_prefs", "f_ip_s")
	if err != nil {
		return fmt.Errorf("probe weather_county_prefs.f_ip_s: %w", err)
	}
	if !has {
		// No-op: do NOT bump user_version so postAutoMigrate migrations
		// with lower version numbers still run on fresh databases.
		return nil
	}
	if err := db.Exec("BEGIN").Error; err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	commit := false
	defer func() {
		if !commit {
			_ = db.Exec("ROLLBACK").Error
		}
	}()
	if err := db.Exec(`ALTER TABLE weather_county_prefs RENAME COLUMN f_ip_s TO fips`).Error; err != nil {
		return fmt.Errorf("rename f_ip_s to fips: %w", err)
	}
	if err := db.Exec("PRAGMA user_version = 31").Error; err != nil {
		return err
	}
	commit = true
	return db.Exec("COMMIT").Error
}
