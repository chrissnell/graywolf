package configstore

import (
	"path/filepath"
	"testing"
)

// TestMigrateKissTcpClientTxDefault exercises migrateKissTcpClientTxDefault
// directly against a populated database (issue #128). It verifies that a
// legacy tcp-client row stuck at the receive-only default is flipped to a
// TX-capable TNC link, while rows that must not change are left alone:
//   - a non-tcp-client (tcp server) row,
//   - an already-correct tcp-client (mode=tnc, governor TX on),
//   - a tcp-client whose channel also has an audio input device (a modem
//     backend) -- flipping that one would double-transmit every frame.
//
// A second invocation must be a no-op (idempotence).
func TestMigrateKissTcpClientTxDefault(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "kiss_tx_default.db")
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	// Channel 3 is modem-backed (input_device_id set). Channel 2 (no
	// row at all) is a KISS-only channel: the migration's subquery only
	// excludes channels that have an audio input device. The audio
	// device row satisfies the channels.input_device_id foreign key.
	// kiss_interfaces.channel is a soft FK (not SQLite-enforced), so
	// rows referencing the absent channel 2 insert fine — if a future
	// schema adds a hard FK there this insert fails loudly here.
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
		{"broken-client", KissTypeTCPClient, 2, KissModeModem, 0},      // -> flips
		{"good-client", KissTypeTCPClient, 2, KissModeTnc, 1},          // already correct
		{"server", KissTypeTCP, 2, KissModeModem, 0},                   // not tcp-client
		{"client-on-modem-ch", KissTypeTCPClient, 3, KissModeModem, 0}, // modem-backed channel
	}
	for _, r := range rows {
		if err := store.DB().Exec(
			`INSERT INTO kiss_interfaces(name, interface_type, channel, mode, allow_tx_from_governor,
			created_at, updated_at) VALUES (?, ?, ?, ?, ?, datetime('now'), datetime('now'))`,
			r.name, r.ifaceType, r.channel, r.mode, r.allow).Error; err != nil {
			t.Fatalf("insert %s: %v", r.name, err)
		}
	}

	if err := migrateKissTcpClientTxDefault(store.DB()); err != nil {
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

	check("broken-client", KissModeTnc, 1)        // repaired
	check("good-client", KissModeTnc, 1)          // untouched (already correct)
	check("server", KissModeModem, 0)             // untouched (not tcp-client)
	check("client-on-modem-ch", KissModeModem, 0) // untouched (modem-backed channel)

	// Idempotence: second run changes nothing.
	if err := migrateKissTcpClientTxDefault(store.DB()); err != nil {
		t.Fatalf("second invocation: %v", err)
	}
	check("broken-client", KissModeTnc, 1)
	check("client-on-modem-ch", KissModeModem, 0)
}
