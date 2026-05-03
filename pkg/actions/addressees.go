package actions

import (
	"strings"
	"sync/atomic"
)

// AddresseeSet is a lock-free read-side cache of operator-defined
// listener addressees. The classifier hits Contains on every inbound
// message packet; the REST handler swaps in a new snapshot via
// Replace. Mirrors messages.TacticalSet semantics so the hot path is
// allocation-free.
type AddresseeSet struct {
	current atomic.Pointer[map[string]struct{}]
}

// NewAddresseeSet constructs an empty set. Seed via Replace.
func NewAddresseeSet() *AddresseeSet {
	s := &AddresseeSet{}
	empty := make(map[string]struct{})
	s.current.Store(&empty)
	return s
}

// Replace atomically swaps in a new snapshot. items are normalized to
// uppercase/trimmed; empty strings are dropped. A nil items slice is
// treated as "empty" — readers never observe nil.
func (s *AddresseeSet) Replace(items []string) {
	m := make(map[string]struct{}, len(items))
	for _, i := range items {
		n := strings.ToUpper(strings.TrimSpace(i))
		if n == "" {
			continue
		}
		m[n] = struct{}{}
	}
	s.current.Store(&m)
}

// Contains reports whether name (case-insensitively) is a member of
// the current snapshot.
func (s *AddresseeSet) Contains(name string) bool {
	n := strings.ToUpper(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	p := s.current.Load()
	if p == nil {
		return false
	}
	_, ok := (*p)[n]
	return ok
}
