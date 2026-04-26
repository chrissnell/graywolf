package logbuffer

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func TestEndToEndBurstStaysBounded(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "graywolf-logs.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	var console bytes.Buffer
	inner := slog.NewTextHandler(&console, &slog.HandlerOptions{Level: slog.LevelInfo})
	h := New(inner, db, Config{RingSize: 50, MaintenanceEvery: 10})
	logger := slog.New(h).WithGroup("ptt")

	const burst = 500
	for i := 0; i < burst; i++ {
		logger.Info("emitted", "seq", i, "ts", time.Now().UnixNano())
	}

	// Ring must be capped: count <= ring + maintenance interval (the
	// last few rows live between eviction passes).
	var count int64
	db.gorm.Raw("SELECT COUNT(*) FROM logs").Scan(&count)
	if count > 60 { // 50 + slack for 10-row maintenance interval
		t.Fatalf("ring exceeded slack budget: count=%d, want <=60", count)
	}

	// Surviving rows must be the most recent ones — check the last row
	// holds the highest seq we emitted.
	var lastAttrs string
	db.gorm.Raw("SELECT attrs_json FROM logs ORDER BY id DESC LIMIT 1").Row().Scan(&lastAttrs)
	if !contains(lastAttrs, `"ptt.seq":499`) {
		t.Fatalf("last row attrs = %q; expected to contain ptt.seq:499", lastAttrs)
	}

	// Component column must reflect the group.
	var lastComponent string
	db.gorm.Raw("SELECT component FROM logs ORDER BY id DESC LIMIT 1").Row().Scan(&lastComponent)
	if lastComponent != "ptt" {
		t.Fatalf("component = %q, want ptt", lastComponent)
	}
}

func contains(haystack, needle string) bool {
	return bytes.Contains([]byte(haystack), []byte(needle))
}
