package logbuffer

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

func newTestHandler(t *testing.T, innerLevel slog.Level) (*Handler, *DB, *bytes.Buffer) {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "graywolf-logs.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: innerLevel})
	h := New(inner, db, Config{RingSize: 100, MaintenanceEvery: 0})
	return h, db, &buf
}

func TestHandlerTeesToConsoleAndDB(t *testing.T) {
	h, db, console := newTestHandler(t, slog.LevelInfo)
	logger := slog.New(h)

	logger.Info("hello world", "k", "v")

	// Console gets the record (inner handler is INFO-and-above).
	if !strings.Contains(console.String(), "hello world") {
		t.Fatalf("console missing record: %q", console.String())
	}

	// DB gets the record too.
	var msg, level string
	row := db.gorm.Raw("SELECT msg, level FROM logs ORDER BY id DESC LIMIT 1").Row()
	if err := row.Scan(&msg, &level); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if msg != "hello world" {
		t.Fatalf("db msg = %q, want %q", msg, "hello world")
	}
	if level != "INFO" {
		t.Fatalf("db level = %q, want INFO", level)
	}
}

func TestHandlerCapturesDebugEvenWhenInnerIsInfo(t *testing.T) {
	h, db, console := newTestHandler(t, slog.LevelInfo)
	logger := slog.New(h)

	logger.Debug("low-level detail", "x", 1)

	// Console must NOT contain the debug record (inner handler is INFO).
	if strings.Contains(console.String(), "low-level detail") {
		t.Fatalf("console should not contain debug record, got: %q", console.String())
	}

	// DB MUST contain it — capture level is always DEBUG.
	var count int64
	db.gorm.Raw("SELECT COUNT(*) FROM logs WHERE msg = ?", "low-level detail").Scan(&count)
	if count != 1 {
		t.Fatalf("db count for debug record = %d, want 1", count)
	}
}

func TestHandlerForwardsAttrs(t *testing.T) {
	h, db, _ := newTestHandler(t, slog.LevelDebug)
	logger := slog.New(h).With("conn_id", "abc123")
	logger.Info("connected", "remote", "10.0.0.1")

	var attrs string
	db.gorm.Raw("SELECT attrs_json FROM logs ORDER BY id DESC LIMIT 1").Row().Scan(&attrs)
	if !strings.Contains(attrs, `"conn_id":"abc123"`) {
		t.Fatalf("attrs missing conn_id: %s", attrs)
	}
	if !strings.Contains(attrs, `"remote":"10.0.0.1"`) {
		t.Fatalf("attrs missing remote: %s", attrs)
	}
}

func TestHandlerEnabledAlwaysTrueAtDebug(t *testing.T) {
	h, _, _ := newTestHandler(t, slog.LevelInfo)
	if !h.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("handler must report Enabled(DEBUG)=true so slog forwards records")
	}
}

func TestHandlerWithGroupChainFlattensToDottedKeys(t *testing.T) {
	h, db, _ := newTestHandler(t, slog.LevelDebug)
	logger := slog.New(h).WithGroup("a").WithGroup("b").With("x", 1)
	logger.Info("nested", "y", 2)

	var attrs string
	db.gorm.Raw("SELECT attrs_json FROM logs ORDER BY id DESC LIMIT 1").Row().Scan(&attrs)
	// Both the chain attr (x) and the record attr (y) should land
	// under the dotted prefix built from the group chain.
	if !strings.Contains(attrs, `"a.b.x":1`) {
		t.Fatalf("attrs missing a.b.x: %s", attrs)
	}
	if !strings.Contains(attrs, `"a.b.y":2`) {
		t.Fatalf("attrs missing a.b.y: %s", attrs)
	}
}

func TestHandlerInlineGroupAttrFlattens(t *testing.T) {
	h, db, _ := newTestHandler(t, slog.LevelDebug)
	logger := slog.New(h)
	logger.Info("grouped", slog.Group("conn", "remote", "10.0.0.1", "port", 8080))

	var attrs string
	db.gorm.Raw("SELECT attrs_json FROM logs ORDER BY id DESC LIMIT 1").Row().Scan(&attrs)
	if !strings.Contains(attrs, `"conn.remote":"10.0.0.1"`) {
		t.Fatalf("attrs missing conn.remote: %s", attrs)
	}
	if !strings.Contains(attrs, `"conn.port":8080`) {
		t.Fatalf("attrs missing conn.port: %s", attrs)
	}
}

func TestHandlerWritesComponentFromGroup(t *testing.T) {
	h, db, _ := newTestHandler(t, slog.LevelDebug)
	logger := slog.New(h.WithGroup("ptt"))
	logger.Info("ptt asserted")

	var component string
	db.gorm.Raw("SELECT component FROM logs ORDER BY id DESC LIMIT 1").Row().Scan(&component)
	if component != "ptt" {
		t.Fatalf("component = %q, want %q", component, "ptt")
	}
}

func TestHandlerWritesEmptyComponentWhenNoGroup(t *testing.T) {
	h, db, _ := newTestHandler(t, slog.LevelDebug)
	logger := slog.New(h)
	logger.Info("startup banner")

	var component string
	db.gorm.Raw("SELECT component FROM logs ORDER BY id DESC LIMIT 1").Row().Scan(&component)
	if component != "" {
		t.Fatalf("component = %q, want empty", component)
	}
}

func TestHandlerWritesNestedComponent(t *testing.T) {
	h, db, _ := newTestHandler(t, slog.LevelDebug)
	logger := slog.New(h.WithGroup("ptt").WithGroup("serial"))
	logger.Info("opened device")

	var component string
	db.gorm.Raw("SELECT component FROM logs ORDER BY id DESC LIMIT 1").Row().Scan(&component)
	if component != "ptt.serial" {
		t.Fatalf("component = %q, want %q", component, "ptt.serial")
	}
}

func TestHandlerSkipsLeadingEmptyGroup(t *testing.T) {
	h, db, _ := newTestHandler(t, slog.LevelDebug)
	// slog permits WithGroup("") (it inlines children at the parent
	// scope). The component column must agree with collectAttrs's
	// dotted prefix — neither should produce a leading dot.
	logger := slog.New(h.WithGroup("").WithGroup("ptt"))
	logger.Info("filtered")

	var component string
	db.gorm.Raw("SELECT component FROM logs ORDER BY id DESC LIMIT 1").Row().Scan(&component)
	if component != "ptt" {
		t.Fatalf("component = %q, want %q", component, "ptt")
	}
}
