package diagcollect

import (
	"path/filepath"
	"testing"

	"github.com/chrissnell/graywolf/pkg/logbuffer"
)

// freshLogDB opens a logbuffer-shaped SQLite DB at a tempfile so the
// collector has a real DB to read from.
func freshLogDB(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "graywolf-logs.db")
	db, err := logbuffer.Open(path)
	if err != nil {
		t.Fatalf("logbuffer.Open: %v", err)
	}
	_ = db.Close() // close before the collector reopens read-only
	_ = n          // future-use; kept to match plan signature
	return path
}

func TestCollectLogs_DBPresent(t *testing.T) {
	path := freshLogDB(t, 0)
	if err := writeOneLogRow(path, 1714000000000000000, "INFO", "boot", "started", `{"k":"v"}`); err != nil {
		t.Fatalf("writeOneLogRow: %v", err)
	}
	res := collectLogsAt(path, 100)
	if len(res.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(res.Entries))
	}
	if res.Source != "graywolf-logs.db" {
		t.Fatalf("Source = %q", res.Source)
	}
	if res.Entries[0].Component != "boot" || res.Entries[0].Msg != "started" {
		t.Fatalf("entry: %+v", res.Entries[0])
	}
	if v, ok := res.Entries[0].Attrs["k"]; !ok || v != "v" {
		t.Fatalf("attrs lost: %+v", res.Entries[0].Attrs)
	}
}

func TestCollectLogs_DBMissingEmitsIssue(t *testing.T) {
	res := collectLogsAt("/no/such/path/graywolf-logs.db", 100)
	if len(res.Entries) != 0 {
		t.Fatalf("entries = %d, want 0", len(res.Entries))
	}
	if len(res.Issues) != 1 || res.Issues[0].Kind != "log_db_unavailable" {
		t.Fatalf("issues = %+v", res.Issues)
	}
}

func TestCollectLogs_OrdersOldestFirstUpToLimit(t *testing.T) {
	path := freshLogDB(t, 0)
	for i := 1; i <= 5; i++ {
		if err := writeOneLogRow(path, int64(1714000000000000000+i), "INFO", "x", "row", `{}`); err != nil {
			t.Fatalf("writeOneLogRow: %v", err)
		}
	}
	res := collectLogsAt(path, 3)
	if len(res.Entries) != 3 {
		t.Fatalf("entries = %d, want 3 (limit honored)", len(res.Entries))
	}
	// Should be the LAST 3 (most recent), but ordered oldest-first
	// inside the result so log readers see chronological order.
	if res.Entries[0].TsNs != 1714000000000000003 {
		t.Fatalf("first entry ts = %d, want 1714000000000000003", res.Entries[0].TsNs)
	}
	if res.Entries[2].TsNs != 1714000000000000005 {
		t.Fatalf("last entry ts = %d, want 1714000000000000005", res.Entries[2].TsNs)
	}
}
