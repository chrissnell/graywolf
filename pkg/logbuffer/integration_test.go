package logbuffer

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
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

	// Peak observable count between eviction passes is exactly
	// RingSize + MaintenanceEvery = 60: insert N+10 fires eviction
	// after the row is already in. Anything above 60 means eviction
	// is silently broken; below 40 means inserts are being dropped.
	var count int64
	if err := db.gorm.Raw("SELECT COUNT(*) FROM logs").Scan(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count > 60 {
		t.Fatalf("ring exceeded peak: count=%d, want <=60", count)
	}
	if count < 40 {
		t.Fatalf("ring under-populated: count=%d, want >=40 (writes dropped?)", count)
	}

	// Surviving rows must be the most recent ones — check the last row
	// holds the highest seq we emitted.
	var lastAttrs string
	if err := db.gorm.Raw("SELECT attrs_json FROM logs ORDER BY id DESC LIMIT 1").Row().Scan(&lastAttrs); err != nil {
		t.Fatalf("scan attrs: %v", err)
	}
	if !strings.Contains(lastAttrs, `"ptt.seq":499`) {
		t.Fatalf("last row attrs = %q; expected to contain ptt.seq:499", lastAttrs)
	}

	// Component column must reflect the group.
	var lastComponent string
	if err := db.gorm.Raw("SELECT component FROM logs ORDER BY id DESC LIMIT 1").Row().Scan(&lastComponent); err != nil {
		t.Fatalf("scan component: %v", err)
	}
	if lastComponent != "ptt" {
		t.Fatalf("component = %q, want ptt", lastComponent)
	}
}

func TestConcurrentChainsShareThrottleAndStayBounded(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "graywolf-logs.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	var console bytes.Buffer
	inner := slog.NewTextHandler(&console, &slog.HandlerOptions{Level: slog.LevelInfo})
	root := New(inner, db, Config{RingSize: 50, MaintenanceEvery: 10})

	// Two chains derived from root via WithGroup. The throttle counter
	// lives on root.shared and must be aliased by both children, so
	// MaintenanceEvery=10 fires regardless of which child logged the
	// 10th record.
	chainA := slog.New(root.WithGroup("ptt"))
	chainB := slog.New(root.WithGroup("kiss"))

	const goroutines = 8
	const perGoroutine = 250
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		g := g
		go func() {
			defer wg.Done()
			logger := chainA
			if g%2 == 1 {
				logger = chainB
			}
			for i := 0; i < perGoroutine; i++ {
				logger.Info("burst", "g", g, "i", i)
			}
		}()
	}
	wg.Wait()

	// Bound check: same shape as TestEndToEndBurstStaysBounded but
	// across N concurrent producers. If the throttle counter were
	// per-chain, MaintenanceEvery would only fire on whichever chain
	// hit the threshold first and the count would blow past the bound.
	var count int64
	if err := db.gorm.Raw("SELECT COUNT(*) FROM logs").Scan(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count > 60 {
		t.Fatalf("ring exceeded peak: count=%d, want <=60 (throttle counter not shared?)", count)
	}
	if count < 40 {
		t.Fatalf("ring under-populated: count=%d, want >=40 (concurrent writes lost?)", count)
	}

	// Both components must be represented in the survivors — proves
	// neither chain was starved by the other.
	var componentsSeen int64
	if err := db.gorm.Raw("SELECT COUNT(DISTINCT component) FROM logs").Scan(&componentsSeen).Error; err != nil {
		t.Fatalf("distinct components: %v", err)
	}
	if componentsSeen != 2 {
		t.Fatalf("distinct components = %d, want 2 (ptt+kiss)", componentsSeen)
	}
}
