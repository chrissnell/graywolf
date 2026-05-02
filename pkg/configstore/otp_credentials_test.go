package configstore

import (
	"context"
	"testing"
	"time"
)

func TestOTPCredentialCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cred := &OTPCredential{
		Name:      "chris-phone",
		Issuer:    "Graywolf",
		Account:   "NW5W:chris-phone",
		Algorithm: "SHA1",
		Digits:    6,
		Period:    30,
		SecretB32: "JBSWY3DPEHPK3PXP",
	}
	if err := s.CreateOTPCredential(ctx, cred); err != nil {
		t.Fatalf("create: %v", err)
	}
	if cred.ID == 0 {
		t.Fatal("expected ID set after Create")
	}

	got, err := s.GetOTPCredential(ctx, cred.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "chris-phone" || got.SecretB32 != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	gotByName, err := s.GetOTPCredentialByName(ctx, "chris-phone")
	if err != nil {
		t.Fatalf("get by name: %v", err)
	}
	if gotByName.ID != cred.ID {
		t.Fatalf("get-by-name returned id=%d, want %d", gotByName.ID, cred.ID)
	}

	all, err := s.ListOTPCredentials(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("want 1, got %d", len(all))
	}

	when := time.Now().UTC()
	if err := s.TouchOTPCredentialUsed(ctx, cred.ID, when); err != nil {
		t.Fatalf("touch: %v", err)
	}
	got2, err := s.GetOTPCredential(ctx, cred.ID)
	if err != nil {
		t.Fatalf("get after touch: %v", err)
	}
	if got2.LastUsedAt == nil {
		t.Fatal("expected LastUsedAt set after touch")
	}

	if err := s.DeleteOTPCredential(ctx, cred.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetOTPCredential(ctx, cred.ID); err == nil {
		t.Fatal("expected not-found after delete")
	} else if !IsNotFound(err) {
		t.Fatalf("expected IsNotFound to identify the error, got %v", err)
	}
}

func TestOTPCredentialDuplicateName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	c1 := &OTPCredential{Name: "x", SecretB32: "A"}
	c2 := &OTPCredential{Name: "x", SecretB32: "B"}
	if err := s.CreateOTPCredential(ctx, c1); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateOTPCredential(ctx, c2); err == nil {
		t.Fatal("expected unique-violation on duplicate name")
	}
}
