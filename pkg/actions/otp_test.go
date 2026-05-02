package actions

import (
	"errors"
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
	if !errors.Is(err, ErrOTPReplay) {
		t.Fatalf("expected ErrOTPReplay, got %v", err)
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

func TestVerifyOTPEmptyCode(t *testing.T) {
	v := NewOTPVerifier(OTPVerifierConfig{Now: time.Now})
	ok, err := v.Verify(1, "JBSWY3DPEHPK3PXP", "")
	if ok || err != nil {
		t.Fatalf("expected (false, nil) for empty code; got (%v, %v)", ok, err)
	}
}

func TestVerifyOTPExpiredCode(t *testing.T) {
	// Code generated three minutes ago is outside the ±1 step window
	// regardless of replay state.
	now := time.Now()
	clock := &fakeClock{t: now}
	v := NewOTPVerifier(OTPVerifierConfig{Now: clock.Now})
	secret := "JBSWY3DPEHPK3PXP"
	staleCode := generateAndVerify(t, secret, now.Add(-3*time.Minute))
	if ok, err := v.Verify(1, secret, staleCode); ok || err != nil {
		t.Fatalf("expected reject of expired code; got (%v, %v)", ok, err)
	}
}

func TestVerifyOTPReplayAcrossStepBoundary(t *testing.T) {
	// A code generated at step S is valid at step S and step S+1
	// (Skew=1). A successful verify at step S must reject the same
	// code presented at step S+1 — the ±1 step probe in Verify covers
	// this even though the entry was written under key(S, code).
	stepStart := time.Unix((time.Now().Unix()/otpStepSeconds)*otpStepSeconds, 0)
	clock := &fakeClock{t: stepStart.Add(1 * time.Second)} // mid-step S
	v := NewOTPVerifier(OTPVerifierConfig{Now: clock.Now})
	secret := "JBSWY3DPEHPK3PXP"
	code := generateAndVerify(t, secret, clock.t)
	if ok, _ := v.Verify(1, secret, code); !ok {
		t.Fatal("first verify failed")
	}
	clock.t = stepStart.Add(31 * time.Second) // step S+1, code still valid
	ok, err := v.Verify(1, secret, code)
	if ok {
		t.Fatal("replay across step boundary must be rejected")
	}
	if !errors.Is(err, ErrOTPReplay) {
		t.Fatalf("expected ErrOTPReplay, got %v", err)
	}
}

func TestVerifyOTPReplayBlockedAcrossFullWindow(t *testing.T) {
	// Replay-TTL contract: a code first used at the very start of
	// step S remains in the ring until well after step S+1 closes
	// (~120s of valid wall-clock time). Without the bumped TTL this
	// would let a replay slip through after ~90s.
	stepStart := time.Unix((time.Now().Unix()/otpStepSeconds)*otpStepSeconds, 0)
	clock := &fakeClock{t: stepStart}
	v := NewOTPVerifier(OTPVerifierConfig{Now: clock.Now})
	secret := "JBSWY3DPEHPK3PXP"
	code := generateAndVerify(t, secret, clock.t)
	if ok, _ := v.Verify(1, secret, code); !ok {
		t.Fatal("first verify failed")
	}
	// Move to T+89s (still inside legacy 90s TTL boundary; with the
	// fix, ring entry is still alive). Replay from that moment, even
	// though TOTP itself would still accept the code at T+30 .. T+59.
	// We pick T+59 so the code is still TOTP-valid at S+1.
	clock.t = stepStart.Add(59 * time.Second)
	ok, err := v.Verify(1, secret, code)
	if ok || !errors.Is(err, ErrOTPReplay) {
		t.Fatalf("expected ErrOTPReplay at T+59s; got (%v, %v)", ok, err)
	}
}

type fakeClock struct{ t time.Time }

func (f *fakeClock) Now() time.Time { return f.t }
