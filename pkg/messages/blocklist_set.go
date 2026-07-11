package messages

import (
	"strings"
	"sync/atomic"
)

// BlocklistSet is a lock-free read-side cache of the enabled blocked
// call signs. The router hits Blocked() on every inbound message packet
// to decide whether to drop it before persistence; CRUD mutations swap
// in a new snapshot via Store().
//
// The snapshot is kept behind atomic.Pointer[map[...]] so readers never
// contend with writers. Empty state is a non-nil empty map so Blocked()
// never observes nil.
//
// Match semantics (see BlockedCallsign): an entry stored without an SSID
// (a bare base callsign, e.g. "N0CALL") blocks every SSID of that base
// call; an SSID-qualified entry (e.g. "N0CALL-7") blocks only that exact
// station. Blocked() implements both by checking the full source and its
// base call against the set.
type BlocklistSet struct {
	current atomic.Pointer[map[string]struct{}]
}

// NewBlocklistSet constructs an empty set. Seed via Store to populate.
func NewBlocklistSet() *BlocklistSet {
	s := &BlocklistSet{}
	empty := make(map[string]struct{})
	s.current.Store(&empty)
	return s
}

// Store atomically replaces the active set. Keys are normalized to
// uppercase/trimmed so readers don't have to worry about provenance. A
// nil newSet is treated as "empty" — the set never observes nil.
func (s *BlocklistSet) Store(newSet map[string]struct{}) {
	if newSet == nil {
		empty := make(map[string]struct{})
		s.current.Store(&empty)
		return
	}
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

// load returns the current snapshot, never nil.
func (s *BlocklistSet) load() map[string]struct{} {
	p := s.current.Load()
	if p == nil {
		return map[string]struct{}{}
	}
	return *p
}

// Blocked reports whether an inbound message from source should be
// dropped. It matches on the full (SSID-aware) source and, so a
// bare-callsign entry mutes every SSID, on the source's base call.
func (s *BlocklistSet) Blocked(source string) bool {
	full := strings.ToUpper(strings.TrimSpace(source))
	if full == "" {
		return false
	}
	m := s.load()
	if _, ok := m[full]; ok {
		return true
	}
	if base := baseCall(full); base != full {
		if _, ok := m[base]; ok {
			return true
		}
	}
	return false
}
