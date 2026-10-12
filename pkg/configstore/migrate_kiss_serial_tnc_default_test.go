package configstore

import (
	"path/filepath"
	"testing"
)

// TestMigrateKissSerialTncDefault exercises migrateKissSerialTncDefault
// directly against a populated database. It verifies that legacy serial
// and usb-serial rows stuck at the receive-only modem default are flipped
// to a TX-capable TNC link, while rows that must not change are left
// alone:
//   - a tcp (server) row (its peer is an APRS app),
//   - an already-correct serial row (mode=tnc, governor TX on),
//   - a serial row whose channel also has an audio input device (a modem
//     backend) -- the legitimate soundmodem-over-serial case, and
//     flipping it would double-transmit.
//
// A second invocation must be a no-op (idempotence).
func TestMigrateKissSerialTncDefault(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "kiss_serial_tnc.db")
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	// Channel 3 is modem-backed (input_device_id set). Channel 2 is
	// KISS-only (no channels row); the migration's subquery only excludes
	// channels that have an audio input device.
	if err := store.DB().Exec(
		`INSERT INTO audio_devices(id, name, direction, source_type, created_at, updated_at)
		VALUES (1, 'card0', 'input', 'soundcard', datetime('now'), datetime('now'))`).Error; err != nil {
		t.Fatalf("insert audio device: %v", err)
	}
	if err := store.DB().Exec(
		`INSERT INTO channels(id, name, modem_type, bit_rate, mark_freq, space_freq, profile,
		num_slicers, fix_bits, num_decoders, decoder_offset, input_device_id, created_at, updated_at)
		VALUES (3, 'modem-ch', 'afsk', 1200, 1200, 2200, 'A', 1, 'none', 1, 0, 1,
		datetime('now'), datetime('now'))`).Error; err != nil {
		t.Fatalf("insert modem channel: %v", err)
	}

	rows := []struct {
		name      string
		ifaceType string
		channel   uint32
		mode      string
		allow     int
	}{
		{"broken-serial", KissTypeSerial, 2, KissModeModem, 0},       // -> flips
		{"broken-usbserial", KissTypeUsbSerial, 2, KissModeModem, 0}, // -> flips
		{"good-serial", KissTypeSerial, 2, KissModeTnc, 1},           // already correct
		{"server", KissTypeTCP, 2, KissModeModem, 0},                 // not serial/usbserial
		{"serial-on-modem-ch", KissTypeSerial, 3, KissModeModem, 0},  // modem-backed channel
	}
	for _, r := range rows {
		if err := store.DB().Exec(
			`INSERT INTO kiss_interfaces(name, interface_type, channel, mode, allow_tx_from_governor,
			created_at, updated_at) VALUES (?, ?, ?, ?, ?, datetime('now'), datetime('now'))`,
			r.name, r.ifaceType, r.channel, r.mode, r.allow).Error; err != nil {
			t.Fatalf("insert %s: %v", r.name, err)
		}
	}

	if err := migrateKissSerialTncDefault(store.DB()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	type got struct {
		Mode  string
		Allow int
	}
	check := func(name, wantMode string, wantAllow int) {
		t.Helper()
		var g got
		if err := store.DB().Raw(
			`SELECT mode AS mode, allow_tx_from_governor AS allow FROM kiss_interfaces WHERE name=?`,
			name).Scan(&g).Error; err != nil {
			t.Fatalf("scan %s: %v", name, err)
		}
		if g.Mode != wantMode || g.Allow != wantAllow {
			t.Errorf("%s: mode=%q allow=%d, want mode=%q allow=%d",
				name, g.Mode, g.Allow, wantMode, wantAllow)
		}
	}

	check("broken-serial", KissModeTnc, 1)        // repaired
	check("broken-usbserial", KissModeTnc, 1)     // repaired
	check("good-serial", KissModeTnc, 1)          // untouched (already correct)
	check("server", KissModeModem, 0)             // untouched (tcp server)
	check("serial-on-modem-ch", KissModeModem, 0) // untouched (modem-backed channel)

	// Idempotence: second run changes nothing.
	if err := migrateKissSerialTncDefault(store.DB()); err != nil {
		t.Fatalf("second invocation: %v", err)
	}
	check("broken-serial", KissModeTnc, 1)
	check("serial-on-modem-ch", KissModeModem, 0)
}
