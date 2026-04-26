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
	if err := db.gorm.Raw("SELECT COUNT(*) FROM logs").Scan(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3 (evict(0) should be a no-op)", count)
	}
}

func TestEvictEmptyTable(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "graywolf-logs.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Evicting an empty table must not error and must leave it empty.
	// The DELETE subquery yields NULL for MAX(id) on an empty table;
	// SQLite evaluates `id <= NULL` as NULL (false) so the statement
	// is a no-op. Locked in here so a future query rewrite that
	// inadvertently uses COALESCE(MAX(id),0) doesn't start nuking
	// rows that haven't been inserted yet.
	if err := evict(db, 10); err != nil {
		t.Fatalf("evict empty: %v", err)
	}
	var count int64
	if err := db.gorm.Raw("SELECT COUNT(*) FROM logs").Scan(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}
}

func TestEvictRingSizeOne(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "graywolf-logs.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// ringSize=1 is the boundary case for the cutoff math: evict deletes
	// any row with id <= MAX(id) - 1, so exactly the most-recent row
	// survives. Locked in here so an off-by-one on the cutoff (e.g.
	// `id < MAX(id) - 1` keeping two rows, or `id <= MAX(id)` keeping
	// none) regresses loudly.

	for i := 0; i < 5; i++ {
		if err := db.gorm.Exec(
			"INSERT INTO logs (ts_ns, level, component, msg, attrs_json) VALUES (?,?,?,?,?)",
			int64(i), "INFO", "test", "msg", "{}",
		).Error; err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	if err := evict(db, 1); err != nil {
		t.Fatalf("evict: %v", err)
	}
	var count int64
	if err := db.gorm.Raw("SELECT COUNT(*) FROM logs").Scan(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("count after ringSize=1 evict = %d, want 1", count)
	}
	var lastTs int64
	if err := db.gorm.Raw("SELECT ts_ns FROM logs ORDER BY id DESC LIMIT 1").Row().Scan(&lastTs); err != nil {
		t.Fatalf("ts: %v", err)
	}
	if lastTs != 4 {
		t.Fatalf("survivor ts = %d, want 4 (newest)", lastTs)
	}
}

func TestEvictRepeated(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "graywolf-logs.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Three insert/evict cycles. Each cycle adds 10 rows then caps to
	// 5. The id column is AUTOINCREMENT, so the cutoff slides forward
	// monotonically across cycles — this catches a regression where
	// the cutoff was computed against COUNT(*) (which would reset to 5
	// every cycle) instead of MAX(id).
	for cycle := 0; cycle < 3; cycle++ {
		for i := 0; i < 10; i++ {
			if err := db.gorm.Exec(
				"INSERT INTO logs (ts_ns, level, component, msg, attrs_json) VALUES (?,?,?,?,?)",
				int64(cycle*100+i), "INFO", "test", "msg", "{}",
			).Error; err != nil {
				t.Fatalf("cycle %d insert %d: %v", cycle, i, err)
			}
		}
		if err := evict(db, 5); err != nil {
			t.Fatalf("cycle %d evict: %v", cycle, err)
		}
		var count int64
		if err := db.gorm.Raw("SELECT COUNT(*) FROM logs").Scan(&count).Error; err != nil {
			t.Fatalf("cycle %d count: %v", cycle, err)
		}
		if count != 5 {
			t.Fatalf("cycle %d count = %d, want 5", cycle, count)
		}
	}
	// Final survivor set should be the last 5 ts values inserted: 205..209.
	var minTs int64
	if err := db.gorm.Raw("SELECT MIN(ts_ns) FROM logs").Scan(&minTs).Error; err != nil {
		t.Fatalf("min ts: %v", err)
	}
	if minTs != 205 {
		t.Fatalf("min ts = %d, want 205", minTs)
	}
}
