package logbuffer

import (
	"path/filepath"
	"testing"
)

func TestEvictKeepsRingSize(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "graywolf-logs.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Insert 50 rows.
	for i := 0; i < 50; i++ {
		if err := db.gorm.Exec(
			"INSERT INTO logs (ts_ns, level, component, msg, attrs_json) VALUES (?,?,?,?,?)",
			int64(i), "INFO", "test", "msg", "{}",
		).Error; err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	// Cap to 10.
	if err := evict(db, 10); err != nil {
		t.Fatalf("evict: %v", err)
	}
	var count int64
	if err := db.gorm.Raw("SELECT COUNT(*) FROM logs").Scan(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 10 {
		t.Fatalf("count after evict = %d, want 10", count)
	}

	// Surviving rows should be the most recent ones (highest ts_ns).
	var minTs int64
	if err := db.gorm.Raw("SELECT MIN(ts_ns) FROM logs").Scan(&minTs).Error; err != nil {
		t.Fatalf("min ts: %v", err)
	}
	if minTs != 40 {
		t.Fatalf("min ts after evict = %d, want 40 (rows 40..49 should survive)", minTs)
	}
}

func TestEvictNoOpUnderRing(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "graywolf-logs.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	for i := 0; i < 5; i++ {
		if err := db.gorm.Exec(
			"INSERT INTO logs (ts_ns, level, component, msg, attrs_json) VALUES (?,?,?,?,?)",
			int64(i), "INFO", "test", "msg", "{}",
		).Error; err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	if err := evict(db, 100); err != nil {
		t.Fatalf("evict: %v", err)
	}
	var count int64
	if err := db.gorm.Raw("SELECT COUNT(*) FROM logs").Scan(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 5 {
		t.Fatalf("count = %d, want 5 (no eviction expected)", count)
	}
}

func TestEvictDisabled(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "graywolf-logs.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	for i := 0; i < 3; i++ {
		if err := db.gorm.Exec(
			"INSERT INTO logs (ts_ns, level, component, msg, attrs_json) VALUES (?,?,?,?,?)",
			int64(i), "INFO", "test", "msg", "{}",
		).Error; err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	// ringSize <= 0 means "do not evict" (caller's responsibility to avoid
	// inserts entirely if persistence is disabled).
	if err := evict(db, 0); err != nil {
		t.Fatalf("evict(0): %v", err)
	}
	var count int64
	db.gorm.Raw("SELECT COUNT(*) FROM logs").Scan(&count)
	if count != 3 {
		t.Fatalf("count = %d, want 3 (evict(0) should be a no-op)", count)
	}
}
