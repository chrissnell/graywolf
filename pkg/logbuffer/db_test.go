package logbuffer

import (
	"os"
	"path/filepath"
	"strings"
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

func TestOpenFailsClearlyWhenDirIsReadOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod ro: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, err := Open(filepath.Join(dir, "graywolf-logs.db"))
	if err == nil {
		t.Fatal("Open against read-only dir should fail")
	}
	// The pre-flight produces a recognizable message; the alternative
	// (SQLite's "out of memory" obfuscation) is exactly what we are
	// trying to avoid surfacing.
	if !strings.Contains(err.Error(), "not writable") {
		t.Fatalf("error = %q, want it to mention 'not writable'", err.Error())
	}
}
