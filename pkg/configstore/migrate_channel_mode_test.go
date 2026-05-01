package configstore

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// TestMigrateChannelMode_BackfillsAPRSDefault exercises migrateChannelsMode
// directly: drops the channels.mode column on a populated database, invokes
// the migration body, and asserts the column was re-added with the
// 'aprs' default applied to existing rows.
//
// Going through the migration body directly (rather than relying on
// re-Open + PRAGMA user_version reset) is the only way to verify the
// migration behaves correctly in the legacy-database scenario it exists
// for: AutoMigrate would otherwise add the column from the Go struct on
// re-Open and short-circuit the migration's columnExists check.
func TestMigrateChannelMode_BackfillsAPRSDefault(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "channel_mode.db")
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	if err := store.DB().Exec(
		`INSERT INTO channels(id, name, modem_type, bit_rate, mark_freq, space_freq, profile,
		num_slicers, fix_bits, num_decoders, decoder_offset, created_at, updated_at)
		VALUES (1, 'legacy', 'afsk', 1200, 1200, 2200, 'A', 1, 'none', 1, 0,
		datetime('now'), datetime('now'))`).Error; err != nil {
		t.Fatalf("insert legacy: %v", err)
	}
	if err := store.DB().Exec(`ALTER TABLE channels DROP COLUMN mode`).Error; err != nil {
		t.Fatalf("drop mode column: %v", err)
	}

	// Probe column-absent invariant before invoking the migration so a
	// future schema change that left the column in place would surface
	// here rather than silently passing the migration's no-op branch.
	hasCol, err := columnExists(store.DB(), "channels", "mode")
	if err != nil {
		t.Fatalf("probe pre-migration: %v", err)
	}
	if hasCol {
		t.Fatalf("pre-migration: mode column unexpectedly present")
	}

	if err := migrateChannelsMode(store.DB()); err != nil {
		t.Fatalf("migrateChannelsMode: %v", err)
	}

	var mode sql.NullString
	if err := store.DB().Raw(`SELECT mode FROM channels WHERE id=1`).Scan(&mode).Error; err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !mode.Valid || mode.String != ChannelModeAPRS {
		t.Fatalf("mode=%v, want %q", mode, ChannelModeAPRS)
	}

	// Idempotence: a second invocation must be a no-op (column already
	// present, columnExists branch returns early).
	if err := migrateChannelsMode(store.DB()); err != nil {
		t.Fatalf("second invocation: %v", err)
	}
}
