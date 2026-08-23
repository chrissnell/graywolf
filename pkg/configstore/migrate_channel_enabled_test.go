package configstore

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// TestMigrateChannelsEnabled_BackfillsTrue exercises migrateChannelsEnabled
// directly on a legacy-shaped database: it drops the channels.enabled
// column on a populated table, invokes the migration body, and asserts
// the column was re-added and every pre-existing row backfilled to
// enabled=1 (so an upgrade keeps all channels running). Going through the
// body directly is the only way to reach the ADD COLUMN branch — a
// re-Open would let AutoMigrate add the column from the Go struct first.
func TestMigrateChannelsEnabled_BackfillsTrue(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "channel_enabled.db")
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
	if err := store.DB().Exec(`ALTER TABLE channels DROP COLUMN enabled`).Error; err != nil {
		t.Fatalf("drop enabled column: %v", err)
	}

	hasCol, err := columnExists(store.DB(), "channels", "enabled")
	if err != nil {
		t.Fatalf("probe pre-migration: %v", err)
	}
	if hasCol {
		t.Fatalf("pre-migration: enabled column unexpectedly present")
	}

	if err := migrateChannelsEnabled(store.DB()); err != nil {
		t.Fatalf("migrateChannelsEnabled: %v", err)
	}

	var enabled sql.NullBool
	if err := store.DB().Raw(`SELECT enabled FROM channels WHERE id=1`).Scan(&enabled).Error; err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !enabled.Valid || !enabled.Bool {
		t.Fatalf("enabled=%v, want true", enabled)
	}

	// Idempotence: a second invocation must be a no-op (column present, no
	// row to change).
	if err := migrateChannelsEnabled(store.DB()); err != nil {
		t.Fatalf("second invocation: %v", err)
	}
}

// TestChannelEnabled_CreateAndToggle pins the store-level contract behind
// the per-channel enable/disable feature: a channel created without
// touching the flag defaults to enabled (the Enabled column defaults
// true; GORM omits a zero-value false, so the store cannot honor an
// explicit create-disabled — that is resolved one layer up in the REST
// handler off the request's *bool), and SetChannelEnabled flips the flag
// in place and persists it.
func TestChannelEnabled_CreateAndToggle(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer s.Close()

	// Default (flag untouched) → enabled.
	def := &Channel{Name: "def", ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200, Profile: "A", NumSlicers: 1, FixBits: "none"}
	if err := s.CreateChannel(ctx, def); err != nil {
		t.Fatalf("create default: %v", err)
	}
	if got, _ := s.GetChannel(ctx, def.ID); got == nil || !got.Enabled {
		t.Fatalf("default channel Enabled=false, want true")
	}

	// SetChannelEnabled flips the flag in place and it must survive a
	// re-read (the persistence-across-restart contract).
	if err := s.SetChannelEnabled(ctx, def.ID, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if got, _ := s.GetChannel(ctx, def.ID); got.Enabled {
		t.Fatalf("SetChannelEnabled(false) did not disable")
	}
	if err := s.SetChannelEnabled(ctx, def.ID, true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if got, _ := s.GetChannel(ctx, def.ID); !got.Enabled {
		t.Fatalf("SetChannelEnabled(true) did not enable")
	}

	// Full-resource UpdateChannel (GORM Save) must also persist an
	// explicit Enabled=false, since the REST full-PUT path relies on it.
	def.Enabled = false
	if err := s.UpdateChannel(ctx, def); err != nil {
		t.Fatalf("update disable: %v", err)
	}
	if got, _ := s.GetChannel(ctx, def.ID); got.Enabled {
		t.Fatalf("UpdateChannel(Enabled=false) did not persist disabled")
	}
}
