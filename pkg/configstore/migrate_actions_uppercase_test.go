package configstore

import (
	"testing"
)

// TestMigration18UppercasesExistingNames inserts mixed-case rows via raw
// SQL (bypassing the Action.BeforeSave hook), invokes the migration
// body directly, and asserts every action_name-bearing column is
// canonicalized to uppercase. Idempotence is also exercised.
func TestMigration18UppercasesExistingNames(t *testing.T) {
	s := newTestStore(t)
	db := s.DB()

	// Mixed-case actions row. The model hook would normalize this on
	// .Create(); raw SQL bypasses it to simulate the pre-migration
	// state.
	if err := db.Exec(`INSERT INTO actions
		(name, type, command_path, timeout_sec, otp_required,
		 sender_allowlist, arg_schema, rate_limit_sec, queue_depth,
		 enabled, arg_mode, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,datetime('now'),datetime('now'))`,
		"MixedCase", "command", "/bin/true", 10, 0, "", "[]", 5, 8, 1, "kv",
	).Error; err != nil {
		t.Fatalf("insert action: %v", err)
	}

	if err := db.Exec(`INSERT INTO remote_action_macros
		(target_call, label, action_name, args_string, position,
		 created_at, updated_at)
		VALUES (?,?,?,?,?, datetime('now'), datetime('now'))`,
		"K1ABC-9", "label", "Unlock", "", 0,
	).Error; err != nil {
		t.Fatalf("insert macro: %v", err)
	}

	if err := db.Exec(`INSERT INTO action_invocations
		(action_name_at, sender_call, source, status, created_at)
		VALUES (?,?,?,?, datetime('now'))`,
		"PingMe", "K7XYZ", "rf", "ok",
	).Error; err != nil {
		t.Fatalf("insert audit: %v", err)
	}

	if err := migrateActionsUppercaseNames(db); err != nil {
		t.Fatalf("migration: %v", err)
	}

	check := func(query, want string) {
		t.Helper()
		var got string
		if err := db.Raw(query).Scan(&got).Error; err != nil {
			t.Fatalf("scan %q: %v", query, err)
		}
		if got != want {
			t.Fatalf("%q -> %q, want %q", query, got, want)
		}
	}
	check(`SELECT name FROM actions LIMIT 1`, "MIXEDCASE")
	check(`SELECT action_name FROM remote_action_macros LIMIT 1`, "UNLOCK")
	check(`SELECT action_name_at FROM action_invocations LIMIT 1`, "PINGME")

	// Idempotent: re-run is a no-op.
	if err := migrateActionsUppercaseNames(db); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	check(`SELECT name FROM actions LIMIT 1`, "MIXEDCASE")
}
