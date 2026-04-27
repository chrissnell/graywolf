package messages

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// fakePrefsReader lets tests drive Load without a real DB.
type fakePrefsReader struct {
	mu     sync.Mutex
	calls  int
	value  *configstore.MessagePreferences
	err    error
	nilRow bool
}

func (f *fakePrefsReader) GetMessagePreferences(_ context.Context) (*configstore.MessagePreferences, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if f.nilRow {
		return nil, nil
	}
	if f.value == nil {
		return &configstore.MessagePreferences{FallbackPolicy: FallbackPolicyISFallback}, nil
	}
	// Return a copy so callers can't mutate the fake's state.
	c := *f.value
	return &c, nil
}

func TestPreferences_CurrentReturnsDefaultsBeforeLoad(t *testing.T) {
	r := &fakePrefsReader{}
	p := NewPreferences(r)
	if p == nil {
		t.Fatal("NewPreferences returned nil")
	}
	cur := p.Current()
	if cur == nil {
		t.Fatal("Current returned nil before Load")
	}
	if cur.FallbackPolicy != FallbackPolicyISFallback {
		t.Errorf("default FallbackPolicy = %q, want %q", cur.FallbackPolicy, FallbackPolicyISFallback)
	}
	if cur.RetryMaxAttempts != 4 {
		t.Errorf("default RetryMaxAttempts = %d, want 4", cur.RetryMaxAttempts)
	}
}

func TestPreferences_LoadReplacesSnapshot(t *testing.T) {
	r := &fakePrefsReader{
		value: &configstore.MessagePreferences{
			FallbackPolicy:   FallbackPolicyRFOnly,
			RetryMaxAttempts: 3,
			DefaultPath:      "WIDE2-2",
		},
	}
	p := NewPreferences(r)
	if _, err := p.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := p.Current()
	if got.FallbackPolicy != FallbackPolicyRFOnly {
		t.Errorf("FallbackPolicy after Load = %q, want %q", got.FallbackPolicy, FallbackPolicyRFOnly)
	}
	if got.RetryMaxAttempts != 3 {
		t.Errorf("RetryMaxAttempts after Load = %d, want 3", got.RetryMaxAttempts)
	}
}

func TestPreferences_LoadKeepsPreviousOnError(t *testing.T) {
	r := &fakePrefsReader{
		value: &configstore.MessagePreferences{
			FallbackPolicy:   FallbackPolicyBoth,
			RetryMaxAttempts: 7,
		},
	}
	p := NewPreferences(r)
	if _, err := p.Load(context.Background()); err != nil {
		t.Fatalf("first Load: %v", err)
	}
	// Switch reader to fail.
	r.mu.Lock()
	r.err = errors.New("db error")
	r.mu.Unlock()
	if _, err := p.Load(context.Background()); err == nil {
		t.Fatalf("expected error on second Load")
	}
	cur := p.Current()
	if cur.FallbackPolicy != FallbackPolicyBoth {
		t.Errorf("snapshot after failing load = %q, want %q", cur.FallbackPolicy, FallbackPolicyBoth)
	}
	if cur.RetryMaxAttempts != 7 {
		t.Errorf("RetryMaxAttempts after failing load = %d, want 7", cur.RetryMaxAttempts)
	}
}

func TestPreferences_LoadNilRowUsesDefaults(t *testing.T) {
	r := &fakePrefsReader{nilRow: true}
	p := NewPreferences(r)
	prefs, err := p.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if prefs.FallbackPolicy != FallbackPolicyISFallback {
		t.Errorf("defaults not used: %q", prefs.FallbackPolicy)
	}
}

// TestPreferences_EffectiveMaxMessageText covers the sender-consulted
// helper. The zero/unset case, the normal-use-case range, and the
// defensive out-of-range normalization are all in one place so a future
// change to the cap constants can re-anchor against this table.
func TestPreferences_EffectiveMaxMessageText(t *testing.T) {
	cases := []struct {
		name     string
		override uint32
		want     int
	}{
		{"zero_uses_default", 0, DefaultMaxMessageText},
		{"below_default_rejected", 50, DefaultMaxMessageText},
		{"at_default_rejected", 67, DefaultMaxMessageText},
		{"just_above_default", 68, 68},
		{"typical_long", 150, 150},
		{"at_ceiling", 200, 200},
		{"above_ceiling", 201, DefaultMaxMessageText},
		{"far_above_ceiling", 1000, DefaultMaxMessageText},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &fakePrefsReader{
				value: &configstore.MessagePreferences{
					FallbackPolicy:         FallbackPolicyISFallback,
					MaxMessageTextOverride: c.override,
				},
			}
			p := NewPreferences(r)
			if _, err := p.Load(context.Background()); err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got := p.EffectiveMaxMessageText(); got != c.want {
				t.Errorf("EffectiveMaxMessageText(override=%d) = %d, want %d",
					c.override, got, c.want)
			}
		})
	}
}

// TestPreferences_EffectiveMaxMessageText_NilPreferences guards the
// belt-and-suspenders path — a nil receiver should never blow up the
// sender. The constructor-returned pointer is always non-nil, but
// defensive coverage here means a future refactor can't silently
// introduce a nil deref in the hot send path.
func TestPreferences_EffectiveMaxMessageText_NilPreferences(t *testing.T) {
	var p *Preferences
	if got := p.EffectiveMaxMessageText(); got != DefaultMaxMessageText {
		t.Errorf("nil Preferences cap = %d, want %d", got, DefaultMaxMessageText)
	}
}

func TestNormalizeFallbackPolicy(t *testing.T) {
	cases := []struct{ in, want string }{
		{FallbackPolicyRFOnly, FallbackPolicyRFOnly},
		{FallbackPolicyISFallback, FallbackPolicyISFallback},
		{FallbackPolicyISOnly, FallbackPolicyISOnly},
		{FallbackPolicyBoth, FallbackPolicyBoth},
		{"", FallbackPolicyISFallback},
		{"garbage", FallbackPolicyISFallback},
	}
	for _, c := range cases {
		got := NormalizeFallbackPolicy(c.in)
		if got != c.want {
			t.Errorf("NormalizeFallbackPolicy(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
