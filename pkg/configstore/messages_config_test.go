package configstore

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMigrateMessagesConfigCopiesFromIgateOnFirstRun(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "messages_config.db")
	ctx := context.Background()

	// Open once to bring the DB to current schema.
	pre, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := pre.UpsertIGateConfig(ctx, &IGateConfig{
		ID: 1, Server: "rotate.aprs2.net", Port: 14580,
		TxChannel: 5, GateRfToIs: true,
	}); err != nil {
		t.Fatalf("seed igate: %v", err)
	}
	// Simulate a pre-Phase-0 database: clear the messages_configs row
	// the first migration just inserted, and reset user_version so the
	// v13 migration runs again on the next Open.
	if err := pre.DB().Exec(`DELETE FROM messages_configs`).Error; err != nil {
		t.Fatalf("delete messages_configs: %v", err)
	}
	if err := pre.DB().Exec(`PRAGMA user_version = 12`).Error; err != nil {
		t.Fatalf("reset user_version: %v", err)
	}
	pre.Close()

	// Re-open: migration v13 runs again, sees IGateConfig.tx_channel=5,
	// seeds messages_configs.tx_channel = 5.
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	defer store.Close()

	mc, err := store.GetMessagesConfig(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if mc.TxChannel != 5 {
		t.Fatalf("TxChannel=%d, want 5", mc.TxChannel)
	}
}

func TestMigrateMessagesConfigSeedsZeroOnFreshInstall(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "messages_config_fresh.db")
	ctx := context.Background()

	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	mc, err := store.GetMessagesConfig(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if mc.TxChannel != 0 {
		t.Fatalf("fresh install: TxChannel=%d, want 0", mc.TxChannel)
	}
}
