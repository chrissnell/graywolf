package configstore

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateKissSerialTncDefault repairs serial and usb-serial KISS
// interfaces that were created before these types defaulted to a
// TX-capable TNC link. Such a row connects to a device that IS the radio
// (a hardware TNC, LoRa modem, or radio with a built-in KISS TNC), but
// stuck at the historical mode=modem / allow_tx_from_governor=0 default
// it misroutes received frames into the TX governor (the operator sees
// no packets) and registers no TX backend (beacons fail with "no backend
// registered for channel"). This is the recurring "serial KISS receives
// nothing" report. Flip those rows to the working tnc + governor-TX
// configuration — the same repair migration 20 applies to tcp-client.
//
// Scoped identically to migration 20 so it cannot create a
// double-transmit channel: a row whose channel also has an audio input
// device (hence a software-modem backend) is left alone — that is the
// legitimate "graywolf-as-soundmodem over a serial/PTY link" case, and
// the store validator (validateKissInterface) forbids tnc+governor-TX
// there anyway. A serial row on a KISS-only channel (input_device_id
// NULL) is already non-functional in modem mode, so flipping it to tnc
// is strictly an improvement. Idempotent: after the flip mode='tnc' no
// longer matches the WHERE clause. No-op when the kiss_interfaces table
// is absent (defensive; it always exists by the post-AutoMigrate phase).
func migrateKissSerialTncDefault(tx *gorm.DB) error {
	var tableExists int
	if err := tx.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='kiss_interfaces'").Scan(&tableExists).Error; err != nil {
		return fmt.Errorf("probe kiss_interfaces: %w", err)
	}
	if tableExists == 0 {
		return nil
	}
	return tx.Exec(
		`UPDATE kiss_interfaces ` +
			`SET mode = 'tnc', allow_tx_from_governor = 1 ` +
			`WHERE interface_type IN ('serial', 'usbserial') ` +
			`AND mode = 'modem' ` +
			`AND allow_tx_from_governor = 0 ` +
			`AND channel NOT IN (SELECT id FROM channels WHERE input_device_id IS NOT NULL)`,
	).Error
}
