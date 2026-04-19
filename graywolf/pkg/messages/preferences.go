package messages

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// MessagePreferencesReader is the narrow read interface the preferences
// wrapper consumes. *configstore.Store satisfies it via
// GetMessagePreferences; tests pass a fake.
type MessagePreferencesReader interface {
	GetMessagePreferences(ctx context.Context) (*configstore.MessagePreferences, error)
}

// Preferences is a cached snapshot of the MessagePreferences singleton.
// The sender, retry manager, and service consult it on every outbound
// decision; Load replaces the snapshot atomically so the
// messagesReload consumer (Phase 4) can refresh without locking the
// hot path.
//
// Callers construct via NewPreferences(reader) and call Load(ctx) at
// startup and on every reload signal. Current() returns the most
// recent successfully-loaded snapshot; if Load has never succeeded it
// returns a non-nil pointer to the configstore defaults so the sender
// always has a policy to evaluate.
type Preferences struct {
	reader  MessagePreferencesReader
	current atomic.Pointer[configstore.MessagePreferences]
}

// NewPreferences constructs an unloaded cache. Callers invoke Load
// before using Current; if they forget, Current returns the built-in
// defaults.
func NewPreferences(reader MessagePreferencesReader) *Preferences {
	if reader == nil {
		return nil
	}
	p := &Preferences{reader: reader}
	p.current.Store(defaultPrefs())
	return p
}

// Load fetches the latest singleton from the reader and replaces the
// cached snapshot. A DB error leaves the previous snapshot in place
// so a transient read failure doesn't take down the sender.
func (p *Preferences) Load(ctx context.Context) (*configstore.MessagePreferences, error) {
	if p == nil || p.reader == nil {
		return nil, errors.New("messages: Preferences not initialized")
	}
	prefs, err := p.reader.GetMessagePreferences(ctx)
	if err != nil {
		return nil, err
	}
	if prefs == nil {
		// Singleton absent — use defaults and succeed. Migration seeds
		// the row, but a fresh-start-before-seed race is survivable.
		prefs = defaultPrefs()
	}
	p.current.Store(prefs)
	return prefs, nil
}

// Current returns the most recently loaded snapshot. Never returns nil
// — if Load has never succeeded, returns a pointer to the default
// configuration so the sender and retry manager always see a policy.
// The returned pointer is owned by the cache; callers must NOT mutate
// it. Treat it as read-only.
func (p *Preferences) Current() *configstore.MessagePreferences {
	if p == nil {
		return defaultPrefs()
	}
	v := p.current.Load()
	if v == nil {
		return defaultPrefs()
	}
	return v
}

// defaultPrefs returns a MessagePreferences populated with the
// seed values Phase 1 writes on first migrate.
func defaultPrefs() *configstore.MessagePreferences {
	return &configstore.MessagePreferences{
		FallbackPolicy:   FallbackPolicyISFallback,
		DefaultPath:      "WIDE1-1,WIDE2-1",
		RetryMaxAttempts: 4,
		RetentionDays:    0,
	}
}

// Fallback policy wire values — mirror configstore's column semantics.
const (
	FallbackPolicyRFOnly     = "rf_only"
	FallbackPolicyISFallback = "is_fallback"
	FallbackPolicyISOnly     = "is_only"
	FallbackPolicyBoth       = "both"
)

// NormalizeFallbackPolicy returns a canonical wire value for p.
// Unknown values fall back to "is_fallback" (the seeded default) so
// the sender never sees an empty policy.
func NormalizeFallbackPolicy(p string) string {
	switch p {
	case FallbackPolicyRFOnly, FallbackPolicyISFallback, FallbackPolicyISOnly, FallbackPolicyBoth:
		return p
	default:
		return FallbackPolicyISFallback
	}
}
