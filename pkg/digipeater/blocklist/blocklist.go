package blocklist

import (
	"strings"
	"sync"

	"github.com/chrissnell/graywolf/pkg/ax25"
)

// Entry is one decoded, canonical block-list pattern plus its operator-
// supplied reason. Pattern is the canonical form returned by
// ValidatePattern (uppercase, trimmed).
type Entry struct {
	Pattern string
	Reason  string
}

// List is a thread-safe collection of block-list entries. The matcher
// is a linear scan; block lists are tiny in practice (single-digit to
// low-double-digit entries) so an index would only add complexity.
type List struct {
	mu      sync.RWMutex
	entries []Entry
}

// New returns a List seeded with the given entries. Nil or empty means
// "no block list" — Matches always returns hit=false.
func New(entries []Entry) *List {
	l := &List{}
	l.Set(entries)
	return l
}

// Set atomically replaces the entries. Safe for live reconfig.
func (l *List) Set(entries []Entry) {
	dup := append([]Entry(nil), entries...)
	l.mu.Lock()
	l.entries = dup
	l.mu.Unlock()
}

// Matches reports whether src is on the block list. On a hit it
// returns the matching entry so the caller can log which pattern (and
// reason) fired. First match wins.
func (l *List) Matches(src ax25.Address) (Entry, bool) {
	l.mu.RLock()
	entries := l.entries
	l.mu.RUnlock()
	for _, e := range entries {
		if matchesEntry(e.Pattern, src) {
			return e, true
		}
	}
	return Entry{}, false
}

// matchesEntry implements the CALL / CALL-N / CALL-* semantics. Inputs
// are assumed to come from ValidatePattern; this function is defensive
// (case-insensitive Call compare, defends against unexpected suffix).
func matchesEntry(pattern string, src ax25.Address) bool {
	call := pattern
	suffix := ""
	if i := strings.IndexByte(pattern, '-'); i >= 0 {
		call = pattern[:i]
		suffix = pattern[i+1:]
	}
	if !strings.EqualFold(call, src.Call) {
		return false
	}
	if suffix == "" {
		return src.SSID == 0
	}
	if suffix == "*" {
		return src.SSID > 0
	}
	for i := 0; i < len(suffix); i++ {
		if suffix[i] < '0' || suffix[i] > '9' {
			return false
		}
	}
	n := 0
	for i := 0; i < len(suffix); i++ {
		n = n*10 + int(suffix[i]-'0')
	}
	return n >= 0 && n <= 15 && uint8(n) == src.SSID
}
