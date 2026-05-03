package actions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/pquerna/otp/totp"
)

const (
	otpStepSeconds = 30
	// Covers worst-case validation window (±1 step around current step
	// = up to ~120s of wall-clock validity) plus a small grace margin.
	otpReplayTTL = 3*otpStepSeconds*time.Second + 30*time.Second
)

// ErrOTPReplay is returned by OTPVerifier.Verify when a code matches but
// has already been observed within the replay TTL. Callers should map
// this to StatusBadOTP and an audit detail of "replay".
var ErrOTPReplay = errors.New("actions: code already used")

// OTPVerifier validates TOTP codes and rejects replays.
type OTPVerifier struct {
	now func() time.Time

	mu   sync.Mutex
	used map[string]time.Time // key: cred|step|hash → expiry
}

type OTPVerifierConfig struct {
	Now func() time.Time
}

func NewOTPVerifier(cfg OTPVerifierConfig) *OTPVerifier {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &OTPVerifier{now: cfg.Now, used: map[string]time.Time{}}
}

// Verify returns (true, nil) when code matches secret within the
// ±1-step window AND has not been used before within the replay TTL.
// Returns (false, nil) on any plain mismatch; (false, err) on replay.
func (v *OTPVerifier) Verify(credID uint, secretB32, code string) (bool, error) {
	now := v.now()
	valid, err := totp.ValidateCustom(code, secretB32, now, totp.ValidateOpts{
		Period: otpStepSeconds, Skew: 1, Digits: 6,
	})
	if err != nil || !valid {
		return false, nil
	}
	step := now.Unix() / otpStepSeconds
	key := replayKey(credID, step, code)
	v.mu.Lock()
	defer v.mu.Unlock()
	if exp, ok := v.used[key]; ok && exp.After(now) {
		return false, ErrOTPReplay
	}
	// Also check ±1 step keys to cover the full validation window.
	for _, s := range []int64{step - 1, step + 1} {
		k := replayKey(credID, s, code)
		if exp, ok := v.used[k]; ok && exp.After(now) {
			return false, ErrOTPReplay
		}
	}
	v.used[key] = now.Add(otpReplayTTL)
	return true, nil
}

// Sweep purges expired ring entries. Safe to call from a background
// goroutine. Service owns the periodic ticker via StartOTPSweeper;
// NewOTPVerifier intentionally does not start one.
func (v *OTPVerifier) Sweep() {
	now := v.now()
	v.mu.Lock()
	defer v.mu.Unlock()
	for k, exp := range v.used {
		if !exp.After(now) {
			delete(v.used, k)
		}
	}
}

// DefaultOTPSweepInterval is how often the Service-owned ticker
// invokes Sweep. The replay-ring TTL is ~150s; 5 minutes is a small
// constant multiple, keeping the residual entry count bounded
// without burning a goroutine on a tight loop.
const DefaultOTPSweepInterval = 5 * time.Minute

// StartOTPSweeper runs Sweep at interval (or DefaultOTPSweepInterval
// when zero) until the returned stop func is called or ctx is
// cancelled. Idempotent stop.
func StartOTPSweeper(ctx context.Context, v *OTPVerifier, interval time.Duration) func() {
	if interval <= 0 {
		interval = DefaultOTPSweepInterval
	}
	stopCh := make(chan struct{})
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			case <-t.C:
				v.Sweep()
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(stopCh) }) }
}

func replayKey(credID uint, step int64, code string) string {
	h := sha256.Sum256([]byte(code))
	return hex.EncodeToString(h[:8]) + "|" +
		strconv.FormatUint(uint64(credID), 10) + "|" +
		strconv.FormatInt(step, 10)
}
