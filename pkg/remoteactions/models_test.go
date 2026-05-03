package remoteactions

import (
	"testing"
	"time"
)

func TestModelTableNames(t *testing.T) {
	if (&RemoteOTPCredential{}).TableName() != "remote_otp_credentials" {
		t.Fatalf("RemoteOTPCredential.TableName mismatch")
	}
	if (&RemoteActionMacro{}).TableName() != "remote_action_macros" {
		t.Fatalf("RemoteActionMacro.TableName mismatch")
	}
}

func TestModelZeroValues(t *testing.T) {
	c := RemoteOTPCredential{Name: "NW5W OTP", SecretB32: "JBSWY3DPEHPK3PXP", CreatedAt: time.Now()}
	if c.Algorithm != "" || c.Digits != 0 || c.Period != 0 {
		t.Fatalf("model defaults should be zero-valued; DB defaults set them")
	}
	m := RemoteActionMacro{TargetCall: "KK7XYZ-9", Label: "x", ActionName: "x"}
	if m.Position != 0 {
		t.Fatalf("Position zero")
	}
	if m.RemoteOTPCredentialID != nil {
		t.Fatalf("FK should be nil")
	}
}
