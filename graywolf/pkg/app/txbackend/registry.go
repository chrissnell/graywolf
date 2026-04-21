package txbackend

import "sync/atomic"

// Snapshot is the immutable per-frame view of the channel →
// backends map. Published by the dispatcher's watcher goroutine via
// one atomic store; read by every Dispatcher.Send via one atomic load.
// Callers MUST NOT mutate the returned maps or slices.
//
// The type is exported so wiring code in other packages can construct
// a snapshot via BuildSnapshot and pass it back to the dispatcher via
// the StartWatcher build closure. Callers outside this package should
// treat the fields as read-only.
type Snapshot struct {
	// ByChannel maps channel ID → every registered Backend serving
	// that channel. The slice is typically length 1 (modem-only or a
	// single kiss-tnc) but may be >1 when a KISS-TNC channel has
	// multiple TNC interfaces attached. Nil / missing entry means no
	// backend — Dispatcher.Send returns ErrNoBackend.
	ByChannel map[uint32][]Backend
	// CsmaSkip[ch] is true when the channel has zero modem backends
	// (KISS-only). The governor uses this to bypass its p-persistence /
	// slot-time / DCD wait: there is no carrier to sense on a TCP
	// link, so CSMA math is meaningless. A channel with both a modem
	// and kiss-tnc backends (forbidden by the validator but defended
	// here) keeps CSMA enabled because the modem half still needs it.
	CsmaSkip map[uint32]bool
}

// newSnapshot returns an empty snapshot with non-nil maps. Used as the
// initial published value so Dispatcher.Send never observes a nil map.
func newSnapshot() *Snapshot {
	return &Snapshot{
		ByChannel: make(map[uint32][]Backend),
		CsmaSkip:  make(map[uint32]bool),
	}
}

// Registry holds the current snapshot behind an atomic.Pointer so the
// hot-path Send has no lock. Writers are expected to build a complete
// new snapshot and Publish it — never mutate a live snapshot in place,
// because a reader may still hold a pointer to the old one.
type Registry struct {
	cur atomic.Pointer[Snapshot]
}

// NewRegistry returns a Registry initialised with an empty snapshot so
// Load is always safe to call.
func NewRegistry() *Registry {
	r := &Registry{}
	r.cur.Store(newSnapshot())
	return r
}

// Publish atomically installs s as the current snapshot. Subsequent
// Load calls observe s. Older snapshots remain valid until every
// reader that already loaded them has released them (Go GC handles
// the lifetime — readers should not retain snapshots beyond the span
// of a single Send call).
func (r *Registry) Publish(s *Snapshot) {
	if s == nil {
		s = newSnapshot()
	}
	r.cur.Store(s)
}

// Load returns the current snapshot. Pointer is never nil because
// NewRegistry seeds the atomic with an empty snapshot.
func (r *Registry) Load() *Snapshot {
	return r.cur.Load()
}
