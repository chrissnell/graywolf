package configstore

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigrateChannelMode_BackfillsAPRSDefault(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "channel_mode.db")
	// Seed the DB *before* migration runs by stripping the column.
	preStore, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := preStore.DB().Exec(`ALTER TABLE channels DROP COLUMN mode`).Error; err != nil {
		t.Logf("drop column (expected on fresh schemas): %v", err)
	}
	if err := preStore.DB().Exec(
		`INSERT INTO channels(id, name, modem_type, bit_rate, mark_freq, space_freq, profile,
		num_slicers, fix_bits, num_decoders, decoder_offset, created_at, updated_at)
		VALUES (1, 'legacy', 'afsk', 1200, 1200, 2200, 'A', 1, 'none', 1, 0,
		datetime('now'), datetime('now'))`).Error; err != nil {
		t.Fatalf("insert legacy: %v", err)
	}
	preStore.Close()

	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("re-open after migrations: %v", err)
	}
	defer store.Close()
	var mode sql.NullString
	if err := store.DB().Raw(`SELECT mode FROM channels WHERE id=1`).Scan(&mode).Error; err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !mode.Valid || mode.String != ChannelModeAPRS {
		t.Fatalf("mode=%v, want %q", mode, ChannelModeAPRS)
	}
}
