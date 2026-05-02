package configstore

import (
	"context"
	"path/filepath"
	"testing"
)

func TestGetAX25TerminalConfigSeedsDefaults(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "ax25_term_cfg.db")
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	cfg, err := store.GetAX25TerminalConfig(context.Background())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if cfg.ID != 1 {
		t.Fatalf("ID=%d want 1", cfg.ID)
	}
	if cfg.ScrollbackRows != 1000 {
		t.Fatalf("ScrollbackRows=%d want 1000", cfg.ScrollbackRows)
	}
	if cfg.DefaultModulo != 8 {
		t.Fatalf("DefaultModulo=%d want 8", cfg.DefaultModulo)
	}
	if cfg.DefaultPaclen != 256 {
		t.Fatalf("DefaultPaclen=%d want 256", cfg.DefaultPaclen)
	}
	if cfg.MacrosJSON != "[]" {
		t.Fatalf("MacrosJSON=%q want []", cfg.MacrosJSON)
	}
}

func TestUpsertAX25TerminalConfigPreservesNonZeroFields(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "ax25_term_cfg_upsert.db")
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.UpsertAX25TerminalConfig(ctx, &AX25TerminalConfig{
		ID:             1,
		ScrollbackRows: 4096,
		CursorBlink:    true,
		DefaultModulo:  128,
		DefaultPaclen:  64,
		MacrosJSON:     `[{"label":"login","payload":"aGVsbG8="}]`,
		RawTailFilter:  "icall=W6XYZ",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := store.GetAX25TerminalConfig(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ScrollbackRows != 4096 || !got.CursorBlink || got.DefaultModulo != 128 ||
		got.DefaultPaclen != 64 || got.MacrosJSON == "[]" || got.RawTailFilter != "icall=W6XYZ" {
		t.Fatalf("upsert lost fields: %+v", got)
	}
}

// TestAX25TerminalConfigMigrationSeedsRow re-opens an empty database
// and verifies migration v14 (or the FirstOrCreate fallback) yields a
// row with id=1.
func TestAX25TerminalConfigMigrationSeedsRow(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "ax25_term_cfg_seed.db")
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	var count int64
	if err := store.DB().Table("ax25_terminal_configs").Where("id = 1").Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one seeded row, got %d", count)
	}
}
