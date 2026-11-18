package configstore

import "gorm.io/gorm"

// Older Graywolf versions could store an inert FX.25 flag on 300-baud channels.
// Do not turn that legacy flag into experimental RF transmission on upgrade.
// This data migration runs once; later explicit opt-ins survive restarts.
func migrateFX25300OptIn(tx *gorm.DB) error {
	return tx.Exec("UPDATE channels SET fx25_encode = 0 WHERE bit_rate = 300 AND fx25_encode <> 0").Error
}
