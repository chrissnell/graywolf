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

	// action_invocations carries two ON DELETE SET NULL FKs (audit
	// rows must outlive the rows they reference): action_id ->
	// actions(id), otp_credential_id -> otp_credentials(id).
	rows2, err := s.DB().Raw("PRAGMA foreign_key_list(action_invocations)").Rows()
	if err != nil {
		t.Fatal(err)
	}
	defer rows2.Close()
	var foundAction, foundCred bool
	for rows2.Next() {
		var id, seq int
		var table, from, to, onUpdate, onDelete, match string
		if err := rows2.Scan(&id, &seq, &table, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			t.Fatalf("scan fk row: %v", err)
		}
		switch {
		case table == "actions" && from == "action_id":
			if onDelete != "SET NULL" {
				t.Fatalf("action_invocations.action_id ON DELETE want SET NULL, got %q", onDelete)
			}
			foundAction = true
		case table == "otp_credentials" && from == "otp_credential_id":
			if onDelete != "SET NULL" {
				t.Fatalf("action_invocations.otp_credential_id ON DELETE want SET NULL, got %q", onDelete)
			}
			foundCred = true
		}
	}
	if !foundAction {
		t.Fatal("FK action_invocations.action_id -> actions missing")
	}
	if !foundCred {
		t.Fatal("FK action_invocations.otp_credential_id -> otp_credentials missing")
	}
}

func TestActionInvocationFKSetNullOnDelete(t *testing.T) {
	// Delete the referenced Action; the audit row's action_id must
	// be nulled rather than orphaned. This is the runtime contract
	// the schema declares.
	s := newTestStore(t)
	cred := &OTPCredential{Name: "c", SecretB32: "JBSWY3DPEHPK3PXP"}
	if err := s.CreateOTPCredential(t.Context(), cred); err != nil {
		t.Fatal(err)
	}
	a := &Action{Name: "ping", Type: "command", CommandPath: "/bin/true", OTPRequired: false}
	if err := s.CreateAction(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	id := a.ID
	row := &ActionInvocation{ActionID: &id, ActionNameAt: "ping", Status: "ok"}
	if err := s.InsertActionInvocation(t.Context(), row); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAction(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	var got *uint
	r := s.DB().Raw("SELECT action_id FROM action_invocations WHERE id=?", row.ID).Row()
	if err := r.Scan(&got); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if got != nil {
		t.Fatalf("expected NULL action_id after delete, got %d", *got)
	}
}
