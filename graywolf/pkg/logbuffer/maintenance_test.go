package logbuffer

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"testing"
)

func TestMaintenanceTriggersEviction(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "graywolf-logs.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	inner := slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelDebug})
	// RingSize 5, eviction every 3 inserts.
	h := New(inner, db, Config{RingSize: 5, MaintenanceEvery: 3})
	logger := slog.New(h)

	for i := 0; i < 20; i++ {
		logger.Info("msg", "i", i)
	}

	var count int64
	db.gorm.Raw("SELECT COUNT(*) FROM logs").Scan(&count)
	// Eviction at every Nth insert means count peaks at
	// RingSize + MaintenanceEvery - 1 between evictions. With
	// RingSize=5, MaintenanceEvery=3 → upper bound 7.
	if count > 7 {
		t.Fatalf("ring exceeded: count=%d, want <=7", count)
	}
	// Should be at least one eviction by row 20 (eviction triggers at
	// inserts 3,6,9,...). The most recent row's content should reflect
	// the latest write.
	var lastMsg string
	db.gorm.Raw("SELECT msg FROM logs ORDER BY id DESC LIMIT 1").Row().Scan(&lastMsg)
	if lastMsg != "msg" {
		t.Fatalf("last msg = %q, want %q", lastMsg, "msg")
	}
}

func TestMaintenanceEveryZeroDisablesAutoEviction(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "graywolf-logs.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	inner := slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := New(inner, db, Config{RingSize: 5, MaintenanceEvery: 0})
	logger := slog.New(h)

	for i := 0; i < 20; i++ {
		logger.Info("msg")
	}

	var count int64
	db.gorm.Raw("SELECT COUNT(*) FROM logs").Scan(&count)
	if count != 20 {
		t.Fatalf("with MaintenanceEvery=0, no eviction expected; got count=%d", count)
	}
	_ = context.Background()
}
