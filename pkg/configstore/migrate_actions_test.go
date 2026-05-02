package configstore

import (
	"testing"
)

// TestMigrateActionsTables asserts that migration 15 creates the four
// Actions feature tables and that the FK on actions.otp_credential_id is
// declared with ON DELETE SET NULL — the contract that lets a credential
// disappear without taking the Action rows that reference it down with
// it. Verified against sqlite_master / PRAGMA so a future AutoMigrate
// reshape can't silently drop the FK semantics.
func TestMigrateActionsTables(t *testing.T) {
	s := newTestStore(t)
	want := []string{"actions", "otp_credentials", "action_listener_addressees", "action_invocations"}
	for _, tbl := range want {
		var n int
		row := s.DB().Raw("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", tbl).Row()
		if err := row.Scan(&n); err != nil {
			t.Fatalf("scan %s: %v", tbl, err)
		}
		if n != 1 {
			t.Fatalf("table %s missing", tbl)
		}
	}
	rows, err := s.DB().Raw("PRAGMA foreign_key_list(actions)").Rows()
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var id, seq int
		var table, from, to, onUpdate, onDelete, match string
		if err := rows.Scan(&id, &seq, &table, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			t.Fatalf("scan fk row: %v", err)
		}
		if table == "otp_credentials" && from == "otp_credential_id" {
			if onDelete != "SET NULL" {
				t.Fatalf("expected ON DELETE SET NULL, got %q", onDelete)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("FK actions.otp_credential_id -> otp_credentials missing")
	}
}
