package configstore

import "gorm.io/gorm"

// migrateKissBLEDeviceType renames the stored interface_type value from the
// old "ble-mobilinkd" to the generic "ble-device" so existing operator
// configurations survive the rename.
func migrateKissBLEDeviceType(tx *gorm.DB) error {
	return tx.Exec(
		"UPDATE kiss_interfaces SET interface_type = ? WHERE interface_type = ?",
		KissTypeBLEDevice, "ble-mobilinkd",
	).Error
}
