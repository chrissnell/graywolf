package actions

import (
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func generateAndVerify(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, at)
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func TestVerifyOTPHappyPath(t *testing.T) {
	v := NewOTPVerifier(OTPVerifierConfig{Now: time.Now})
	secret := "JBSWY3DPEHPK3PXP"
	code := generateAndVerify(t, secret, time.Now())
	ok, err := v.Verify(1, secret, code)
	if err != nil || !ok {
		t.Fatalf("happy path failed: ok=%v err=%v", ok, err)
	}
}

func TestVerifyOTPReplayRejected(t *testing.T) {
	v := NewOTPVerifier(OTPVerifierConfig{Now: time.Now})
	secret := "JBSWY3DPEHPK3PXP"
	code := generateAndVerify(t, secret, time.Now())
	ok, _ := v.Verify(1, secret, code)
	if !ok {
		t.Fatal("first verify failed")
	}
	ok, err := v.Verify(1, secret, code)
	if ok {
		t.Fatal("replay should have been rejected")
	}
	if err == nil || err.Error() != "actions: code already used" {
		t.Fatalf("expected replay error, got %v", err)
	}
}

func TestVerifyOTPWrongCode(t *testing.T) {
	v := NewOTPVerifier(OTPVerifierConfig{Now: time.Now})
	ok, _ := v.Verify(1, "JBSWY3DPEHPK3PXP", "000000")
	if ok {
		t.Fatal("expected reject of wrong code")
	}
}

func TestVerifyOTPRingExpiry(t *testing.T) {
	now := time.Now()
	clock := &fakeClock{t: now}
	v := NewOTPVerifier(OTPVerifierConfig{Now: clock.Now})
	secret := "JBSWY3DPEHPK3PXP"
	code := generateAndVerify(t, secret, now)
	if ok, _ := v.Verify(1, secret, code); !ok {
		t.Fatal("first verify failed")
	}
	clock.t = now.Add(3 * time.Minute) // beyond TTL
	v.Sweep()
	// Still rejected because the code itself is past the ±1 window.
	if ok, _ := v.Verify(1, secret, code); ok {
		t.Fatal("expected stale code reject")
	}
}

type fakeClock struct{ t time.Time }

func (f *fakeClock) Now() time.Time { return f.t }
