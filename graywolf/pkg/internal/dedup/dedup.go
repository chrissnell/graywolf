// Package dedup provides a time-windowed deduplication cache suitable for
// suppressing recently-seen APRS/AX.25 frames across the transmit
// governor, the digipeater, and the iGate.
//
// Window is generic over a key type and an optional per-entry value
// type. Callers that only need presence tracking use struct{} for the
// value; callers that need to carry a small amount of metadata (e.g.
// the iGate's "was the previous packet a fixed-station beacon" bit)
// parameterize Window with a richer value type.
package dedup

import (
	"sync"
	"time"
)

// defaultGCThreshold is the single source of truth for the map size at
// which opportunistic eviction kicks in. Previously this constant was
// duplicated as a magic 64 in three packages.
const defaultGCThreshold = 64

// Config controls Window behavior.
type Config struct {
	// TTL is the suppression window. Required; a non-positive value
	// panics in New to surface mis-configuration early.
	TTL time.Duration
	// GCThreshold is the map size at which opportunistic eviction of
	// expired entries runs. Zero means defaultGCThreshold.
	GCThreshold int
	// Now overrides the clock for tests. Zero means time.Now.
	Now func() time.Time
}

// Window is a time-based dedup cache. Keys are compared for equality;
// entries are evicted lazily when the caller touches the Window and
// the entry is older than the configured TTL. A background goroutine
// is not used.
type Window[K comparable, V any] struct {
	mu          sync.Mutex
	ttl         time.Duration
	gcThreshold int
	now         func() time.Time
	seen        map[K]entry[V]
}

type entry[V any] struct {
	when  time.Time
	value V
}

// New builds a Window. Panics if cfg.TTL <= 0.
func New[K comparable, V any](cfg Config) *Window[K, V] {
	if cfg.TTL <= 0 {
		panic("dedup: non-positive TTL")
	}
	gc := cfg.GCThreshold
	if gc <= 0 {
		gc = defaultGCThreshold
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Window[K, V]{
		ttl:         cfg.TTL,
		gcThreshold: gc,
		now:         now,
		seen:        make(map[K]entry[V]),
	}
}

// Seen records (k, v) at the current time and returns the previous value
// and true if k was already present within TTL before this call;
// otherwise returns (zero, false) after recording (k, v). The clock is
// sampled once per call.
func (w *Window[K, V]) Seen(k K, v V) (V, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	w.gcLocked(now)
	if e, ok := w.seen[k]; ok && now.Sub(e.when) < w.ttl {
		// Refresh the timestamp so a steady stream of duplicates keeps
		// the entry alive. Preserves the previous value so callers can
		// read it on the duplicate path.
		w.seen[k] = entry[V]{when: now, value: e.value}
		return e.value, true
	}
	w.seen[k] = entry[V]{when: now, value: v}
	var zero V
	return zero, false
}

// Has reports whether k is present within TTL without recording.
func (w *Window[K, V]) Has(k K) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	w.gcLocked(now)
	e, ok := w.seen[k]
	if !ok {
		return false
	}
	return now.Sub(e.when) < w.ttl
}

// Peek returns the most recent recorded value and time for k, and
// whether k is currently tracked. Does not evict or refresh. The
// returned time may be older than the TTL; callers that care about
// TTL-bounded presence should compare against the configured window.
func (w *Window[K, V]) Peek(k K) (V, time.Time, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	w.gcLocked(now)
	e, ok := w.seen[k]
	if !ok {
		var zero V
		return zero, time.Time{}, false
	}
	return e.value, e.when, true
}

// Record stores (k, v) at the current time without returning whether k
// was previously present. Use after a capacity or condition check that
// is independent of the dedup state.
func (w *Window[K, V]) Record(k K, v V) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	w.gcLocked(now)
	w.seen[k] = entry[V]{when: now, value: v}
}

// Len returns the current number of tracked entries. Intended for
// metrics and tests; does not run GC.
func (w *Window[K, V]) Len() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.seen)
}

// Reset clears all entries.
func (w *Window[K, V]) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.seen = make(map[K]entry[V])
}

// SetTTL replaces the suppression window. Existing entries are kept and
// will be re-evaluated against the new TTL on the next touch. A
// non-positive duration is ignored so callers driving this from a
// config reload cannot accidentally disable suppression.
func (w *Window[K, V]) SetTTL(ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ttl = ttl
}

// gcLocked drops expired entries when the map exceeds the threshold.
// Callers must hold w.mu.
func (w *Window[K, V]) gcLocked(now time.Time) {
	if len(w.seen) < w.gcThreshold {
		return
	}
	for k, e := range w.seen {
		if now.Sub(e.when) >= w.ttl {
			delete(w.seen, k)
		}
	}
}
