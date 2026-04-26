package logbuffer

import (
	"path/filepath"
	"testing"
)

func TestOpenCreatesSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graywolf-logs.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Schema sanity: insert one row via the gorm handle and read it back.
	if err := db.gorm.Exec(
		"INSERT INTO logs (ts_ns, level, component, msg, attrs_json) VALUES (?,?,?,?,?)",
		int64(1), "INFO", "test", "hello", "{}",
	).Error; err != nil {
		t.Fatalf("insert: %v", err)
	}
	var count int64
	if err := db.gorm.Raw("SELECT COUNT(*) FROM logs").Scan(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graywolf-logs.db")
	for i := 0; i < 3; i++ {
		db, err := Open(path)
		if err != nil {
			t.Fatalf("Open #%d: %v", i, err)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("Close #%d: %v", i, err)
		}
	}
}
