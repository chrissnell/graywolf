package configstore

import (
	"context"
	"testing"
)

func TestActionCRUDAndFKSetNull(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cred := &OTPCredential{Name: "c1", SecretB32: "X"}
	if err := s.CreateOTPCredential(ctx, cred); err != nil {
		t.Fatal(err)
	}

	a := &Action{
		Name:            "TurnOnGarageLights",
		Type:            "command",
		CommandPath:     "/usr/local/bin/lights",
		OTPRequired:     true,
		OTPCredentialID: &cred.ID,
		ArgSchema:       `[{"key":"state","regex":"^(on|off)$","required":true}]`,
		RateLimitSec:    5,
		QueueDepth:      8,
		TimeoutSec:      10,
		Enabled:         true,
	}
	if err := s.CreateAction(ctx, a); err != nil {
		t.Fatalf("create: %v", err)
	}
	if a.ID == 0 {
		t.Fatal("expected ID set after Create")
	}

	got, err := s.GetActionByName(ctx, "TurnOnGarageLights")
	if err != nil {
		t.Fatalf("get by name: %v", err)
	}
	if got.OTPCredentialID == nil || *got.OTPCredentialID != cred.ID {
		t.Fatalf("FK not stored, got %+v", got)
	}
	if got.RateLimitSec != 5 || got.QueueDepth != 8 || got.TimeoutSec != 10 {
		t.Fatalf("scalars not stored: %+v", got)
	}

	// Update path: rename + flip Enabled.
	got.Description = "garage"
	got.Enabled = false
	if err := s.UpdateAction(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	got2, err := s.GetAction(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got2.Description != "garage" || got2.Enabled {
		t.Fatalf("update did not stick: %+v", got2)
	}

	// FK ON DELETE SET NULL: deleting the credential must clear the FK,
	// not cascade-delete the action.
	if err := s.DeleteOTPCredential(ctx, cred.ID); err != nil {
		t.Fatal(err)
	}
	got3, err := s.GetActionByName(ctx, "TurnOnGarageLights")
	if err != nil {
		t.Fatalf("action gone after credential delete: %v", err)
	}
	if got3.OTPCredentialID != nil {
		t.Fatalf("expected FK SET NULL on credential delete, got %v", *got3.OTPCredentialID)
	}

	// Listing returns name-ordered rows.
	b := &Action{Name: "AAA", Type: "webhook", WebhookMethod: "POST", WebhookURL: "http://x"}
	if err := s.CreateAction(ctx, b); err != nil {
		t.Fatal(err)
	}
	all, err := s.ListActions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].Name != "AAA" {
		t.Fatalf("unexpected list ordering: %+v", all)
	}

	// Delete by id.
	if err := s.DeleteAction(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAction(ctx, b.ID); !IsNotFound(err) {
		t.Fatalf("expected not-found, got %v", err)
	}
}

// gorm cannot tell a Go bool zero from "field not set"; without
// Select("*") on the insert, the column default (`true`) silently
// overrides Enabled=false / OTPRequired=false on first save. This
// guards the regression: a fresh Action created with both flags
// false must read back with both flags false.
func TestCreateActionPersistsBoolZero(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a := &Action{
		Name:        "Disabled",
		Type:        "command",
		CommandPath: "/bin/true",
		OTPRequired: false,
		Enabled:     false,
	}
	if err := s.CreateAction(ctx, a); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.GetActionByName(ctx, "Disabled")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.OTPRequired {
		t.Errorf("OTPRequired flipped to true after Create")
	}
	if got.Enabled {
		t.Errorf("Enabled flipped to true after Create")
	}
}

func TestActionDuplicateName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a := &Action{Name: "x", Type: "command", CommandPath: "/bin/true"}
	b := &Action{Name: "x", Type: "command", CommandPath: "/bin/true"}
	if err := s.CreateAction(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateAction(ctx, b); err == nil {
		t.Fatal("expected unique-violation on duplicate name")
	}
}
