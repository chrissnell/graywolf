package remoteactions

import (
	"context"
	"testing"
)

func TestServiceConstruction(t *testing.T) {
	db := newTestDB(t)
	svc, err := NewService(ServiceConfig{DB: db})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if svc.Creds() == nil || svc.Macros() == nil {
		t.Fatalf("sub-stores not exposed")
	}
}

func TestServiceRequiresDB(t *testing.T) {
	if _, err := NewService(ServiceConfig{}); err == nil {
		t.Fatalf("expected error for nil DB")
	}
}

func TestServiceFireRoundtrip(t *testing.T) {
	db := newTestDB(t)
	svc, _ := NewService(ServiceConfig{DB: db})
	ctx := context.Background()

	cred := &RemoteOTPCredential{Name: "NW5W OTP", SecretB32: "JBSWY3DPEHPK3PXP"}
	if err := svc.Creds().Create(ctx, cred); err != nil {
		t.Fatalf("create cred: %v", err)
	}
	macro := &RemoteActionMacro{
		TargetCall:            "KK7XYZ-9",
		Label:                 "unlock front",
		ActionName:            "unlock",
		ArgsString:            "door=front",
		RemoteOTPCredentialID: &cred.ID,
	}
	if err := svc.Macros().Create(ctx, macro); err != nil {
		t.Fatalf("create macro: %v", err)
	}

	got, err := svc.Macros().ListByTarget(ctx, "KK7XYZ-9")
	if err != nil || len(got) != 1 {
		t.Fatalf("list: %v, %d rows", err, len(got))
	}
	if got[0].RemoteOTPCredentialID == nil || *got[0].RemoteOTPCredentialID != cred.ID {
		t.Fatalf("FK lost in round-trip")
	}
}
