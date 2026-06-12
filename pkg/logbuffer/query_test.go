package logbuffer

import (
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

// seedDB returns a DB with three rows: a DEBUG, an INFO, and a WARN,
// inserted via the real handler so the on-disk shape matches production.
func seedDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "graywolf-logs.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	h := New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError}), db, Config{RingSize: 100})
	// inner is ERROR-only so nothing prints; persist captures DEBUG+.
	lg := slog.New(h)
	lg.Debug("dbg line", "k", 1)
	lg.Info("info line", "user", "bob")
	lg.Warn("warn line")
	return db
}

func TestQueryReturnsAscendingByID(t *testing.T) {
	db := seedDB(t)
	recs, err := db.Query(QueryOptions{Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(recs) != 3 {
		t.Fatalf("got %d records, want 3", len(recs))
	}
	if recs[0].Message != "dbg line" || recs[2].Message != "warn line" {
		t.Fatalf("not ascending: %q .. %q", recs[0].Message, recs[2].Message)
	}
	if recs[1].Attrs["user"] != "bob" {
		t.Fatalf("attrs not decoded: %+v", recs[1].Attrs)
	}
	if recs[1].Level != "INFO" {
		t.Fatalf("level = %q, want INFO", recs[1].Level)
	}
	if recs[1].Time.IsZero() {
		t.Fatalf("time not decoded")
	}
}

func TestQueryLimitKeepsMostRecent(t *testing.T) {
	db := seedDB(t)
	recs, err := db.Query(QueryOptions{Limit: 1})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(recs) != 1 || recs[0].Message != "warn line" {
		t.Fatalf("limit=1 should yield newest only, got %+v", recs)
	}
}

func TestQueryLevelFilter(t *testing.T) {
	db := seedDB(t)
	recs, err := db.Query(QueryOptions{Limit: 10, Levels: []string{"INFO", "WARN", "ERROR"}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("level filter should drop DEBUG, got %d", len(recs))
	}
}

func TestQuerySinceFilter(t *testing.T) {
	db := seedDB(t)
	all, _ := db.Query(QueryOptions{Limit: 10})
	cutoff := all[2].Time // the WARN row's timestamp
	recs, err := db.Query(QueryOptions{Limit: 10, Since: cutoff})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(recs) != 1 || recs[0].Message != "warn line" {
		t.Fatalf("since filter wrong, got %+v", recs)
	}
}

var _ = time.Now // keep time import if trimmed by tooling
