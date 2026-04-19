package messages

import (
	"strings"
	"sync/atomic"
)

// TacticalSet is a lock-free read-side cache of the enabled tactical
// callsigns. The router hits Contains() on every inbound message
// packet; preferences reloads swap in a new snapshot via Store().
//
// The snapshot is kept behind atomic.Pointer[map[...]] so readers do
// not contend with writers. Empty state is represented as a non-nil
// empty map so Contains() never observes nil.
type TacticalSet struct {
	current atomic.Pointer[map[string]struct{}]
}

// NewTacticalSet constructs an empty set. Seed via Store to populate.
func NewTacticalSet() *TacticalSet {
	s := &TacticalSet{}
	empty := make(map[string]struct{})
	s.current.Store(&empty)
	return s
}

// Store atomically replaces the active set. The caller keeps
// ownership of newSet only until the call returns; after Store, the
// TacticalSet owns the map and callers must not mutate it.
//
// A nil newSet is treated as "empty" — the set never observes nil.
func (s *TacticalSet) Store(newSet map[string]struct{}) {
	if newSet == nil {
		empty := make(map[string]struct{})
		s.current.Store(&empty)
		return
	}
	// Normalize to uppercase/trimmed keys so readers don't have to
	// worry about the provenance of the snapshot.
	normalized := make(map[string]struct{}, len(newSet))
	for k := range newSet {
		nk := strings.ToUpper(strings.TrimSpace(k))
		if nk == "" {
			continue
		}
		normalized[nk] = struct{}{}
	}
	s.current.Store(&normalized)
}

// Load returns the current snapshot. The returned map is immutable
// from the caller's perspective (the TacticalSet may replace it at
// any time, but the returned reference keeps pointing at the old
// snapshot for the duration of the caller's use).
func (s *TacticalSet) Load() map[string]struct{} {
	p := s.current.Load()
	if p == nil {
		return map[string]struct{}{}
	}
	return *p
}

// Contains reports whether key (case-insensitively) is a member of
// the current snapshot.
func (s *TacticalSet) Contains(key string) bool {
	norm := strings.ToUpper(strings.TrimSpace(key))
	if norm == "" {
		return false
	}
	m := s.Load()
	_, ok := m[norm]
	return ok
}
