package configstore

import (
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// migrateStationConfigSSID backfills station_configs rows whose callsign
// embeds an SSID suffix (e.g. "KE7XYZ-9" becomes callsign="KE7XYZ", ssid=9).
// AutoMigrate already adds the ssid column from the struct tag on existing
// databases; this migration only splits the embedded SSID out of callsign.
// Idempotent: rows without a dash in callsign are untouched.
func migrateStationConfigSSID(tx *gorm.DB) error {
	// Backfill: parse any existing callsign that contains a "-N" SSID
	// suffix, extract the SSID int, and split it into the new column.
	type row struct {
		ID       uint32
		Callsign string
	}
	var rows []row
	if err := tx.Raw("SELECT id, callsign FROM station_configs").Scan(&rows).Error; err != nil {
		return fmt.Errorf("read station_configs for backfill: %w", err)
	}
	for _, r := range rows {
		idx := strings.LastIndexByte(r.Callsign, '-')
		if idx < 0 {
			continue
		}
		ssidStr := r.Callsign[idx+1:]
		ssidVal, convErr := strconv.Atoi(ssidStr)
		if convErr != nil || ssidVal < 0 || ssidVal > 15 {
			continue
		}
		base := r.Callsign[:idx]
		if err := tx.Exec(
			"UPDATE station_configs SET callsign = ?, ssid = ? WHERE id = ?",
			strings.ToUpper(strings.TrimSpace(base)), ssidVal, r.ID,
		).Error; err != nil {
			return fmt.Errorf("backfill row %d: %w", r.ID, err)
		}
	}
	return nil
}
