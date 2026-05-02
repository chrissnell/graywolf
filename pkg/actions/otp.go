package actions

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/pquerna/otp/totp"
)

const (
	otpStepSeconds = 30
	otpReplayTTL   = 2*otpStepSeconds*time.Second + 30*time.Second
)

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
		return false, errors.New("actions: code already used")
	}
	// Also check ±1 step keys to cover the full validation window.
	for _, s := range []int64{step - 1, step + 1} {
		k := replayKey(credID, s, code)
		if exp, ok := v.used[k]; ok && exp.After(now) {
			return false, errors.New("actions: code already used")
		}
	}
	v.used[key] = now.Add(otpReplayTTL)
	return true, nil
}

// Sweep purges expired ring entries. Safe to call from a background
// goroutine.
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

func replayKey(credID uint, step int64, code string) string {
	h := sha256.Sum256([]byte(code))
	return hex.EncodeToString(h[:8]) + "|" + itoa(credID) + "|" + itoa64(step)
}

func itoa(u uint) string    { return formatInt(int64(u)) }
func itoa64(i int64) string { return formatInt(i) }

func formatInt(i int64) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
