package configstore

import (
	"testing"
)

// TestMigration17AddsArgModeColumn verifies that migration 17 adds
// arg_mode to the actions table with NOT NULL DEFAULT 'kv', so existing
// rows materialize the default at read time without an explicit UPDATE.
func TestMigration17AddsArgModeColumn(t *testing.T) {
	s := newTestStore(t)

	// Insert a row with NO arg_mode value via raw SQL — proving the
	// column is materialized with the documented default for legacy
	// rows that predate the migration field on the model.
	if err := s.DB().Exec(`INSERT INTO actions
		(name, type, command_path, timeout_sec, otp_required,
		 sender_allowlist, arg_schema, rate_limit_sec, queue_depth,
		 enabled, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,datetime('now'),datetime('now'))`,
		"legacy", "command", "/bin/true", 10, 0, "", "[]", 5, 8, 1).Error; err != nil {
		t.Fatalf("insert legacy: %v", err)
	}

	var mode string
	if err := s.DB().Raw(`SELECT arg_mode FROM actions WHERE name = 'legacy'`).Scan(&mode).Error; err != nil {
		t.Fatalf("scan arg_mode: %v", err)
	}
	if mode != "kv" {
		t.Fatalf("arg_mode = %q, want %q", mode, "kv")
	}

	// Round-trip a freeform row through the model layer.
	a := &Action{
		Name: "fm", Type: "command", CommandPath: "/bin/true",
		TimeoutSec: 5, ArgSchema: `[]`, RateLimitSec: 0, QueueDepth: 1,
		Enabled: true, ArgMode: "freeform",
	}
	if err := s.DB().Create(a).Error; err != nil {
		t.Fatalf("create freeform action: %v", err)
	}
	var got Action
	if err := s.DB().Where("name = ?", "fm").First(&got).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.ArgMode != "freeform" {
		t.Fatalf("ArgMode round-trip = %q, want %q", got.ArgMode, "freeform")
	}
}
