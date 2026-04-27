package messages

import (
	"strings"
	"sync"
	"time"
)

// LocalTxRing tracks recent (source, msg_id) tuples submitted by our
// sender so that the router's self-filter and the iGate gating filter
// can recognize packets that originated locally and avoid acting on
// them as if they were inbound.
//
// Entries have a TTL (default 5 minutes) and the ring caps the number
// of live entries (default 256). Eviction happens lazily on insert and
// on lookup — no dedicated goroutine. The hot path (Contains) reads
// under an RLock; inserts and expirations take the write lock.
type LocalTxRing struct {
	mu     sync.RWMutex
	ttl    time.Duration
	cap    int
	entries map[string]*ringEntry // key -> node
	head   *ringEntry             // oldest (front)
	tail   *ringEntry             // newest (back)
	now    func() time.Time
}

type ringEntry struct {
	key        string
	expiresAt  time.Time
	prev, next *ringEntry
}

// Defaults for the LocalTxRing. Tuned for messaging traffic: a 5-minute
// TTL covers the entire DM ack cycle (max 5 attempts × ~10min backoff =
// ~20min is longer, but acks only need to be swallowed for a few
// retransmits — the ring is a self-filter, not a durable record) and
// 256 entries holds roughly 5 minutes of sustained outbound messaging
// comfortably.
const (
	DefaultLocalTxRingTTL  = 5 * time.Minute
	DefaultLocalTxRingSize = 256
)

// NewLocalTxRing returns an empty ring with the given capacity and
// TTL. Values <= 0 fall back to defaults.
func NewLocalTxRing(size int, ttl time.Duration) *LocalTxRing {
	if size <= 0 {
		size = DefaultLocalTxRingSize
	}
	if ttl <= 0 {
		ttl = DefaultLocalTxRingTTL
	}
	return &LocalTxRing{
		ttl:     ttl,
		cap:     size,
		entries: make(map[string]*ringEntry, size),
		now:     time.Now,
	}
}

// ringKey normalizes (source, msg_id) into a map key. Source is
// upper-cased and trimmed; msg_id passes through unchanged to match the
// store's canonical form.
func ringKey(source, msgID string) string {
	return strings.ToUpper(strings.TrimSpace(source)) + "\x00" + msgID
}

// Add records a newly-submitted (source, msg_id). Existing entries
// with the same key are refreshed (TTL reset, moved to tail).
func (r *LocalTxRing) Add(source, msgID string) {
	if msgID == "" {
		// An empty msgid would match every peer; refuse to record it.
		return
	}
	key := ringKey(source, msgID)
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	r.evictExpiredLocked(now)

	if node, ok := r.entries[key]; ok {
		node.expiresAt = now.Add(r.ttl)
		r.moveToTailLocked(node)
		return
	}
	// Cap eviction — drop the oldest when at capacity.
	for len(r.entries) >= r.cap && r.head != nil {
		r.removeLocked(r.head)
	}
	node := &ringEntry{key: key, expiresAt: now.Add(r.ttl)}
	r.appendTailLocked(node)
	r.entries[key] = node
}

// Contains reports whether (source, msg_id) is in the ring. Expired
// entries are evicted before the check so a stale positive never
// leaks.
func (r *LocalTxRing) Contains(source, msgID string) bool {
	if msgID == "" {
		return false
	}
	key := ringKey(source, msgID)
	// Fast path — RLock, check without touching state. If the node is
	// expired we must acquire the write lock to evict.
	r.mu.RLock()
	node, ok := r.entries[key]
	if !ok {
		r.mu.RUnlock()
		return false
	}
	expired := r.now().After(node.expiresAt)
	r.mu.RUnlock()
	if !expired {
		return true
	}
	// Upgrade to write lock to evict. Re-check in case a concurrent
	// writer refreshed the entry.
	r.mu.Lock()
	defer r.mu.Unlock()
	node, ok = r.entries[key]
	if !ok {
		return false
	}
	if r.now().After(node.expiresAt) {
		r.removeLocked(node)
		return false
	}
	return true
}

// Len returns the number of live entries. Useful for metrics.
func (r *LocalTxRing) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}

// evictExpiredLocked removes all expired nodes from the front of the
// list. Called with the write lock held.
func (r *LocalTxRing) evictExpiredLocked(now time.Time) {
	for r.head != nil && now.After(r.head.expiresAt) {
		r.removeLocked(r.head)
	}
}

// moveToTailLocked relocates node to the tail position (newest).
func (r *LocalTxRing) moveToTailLocked(node *ringEntry) {
	if r.tail == node {
		return
	}
	// Unlink.
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		r.head = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	}
	node.prev, node.next = nil, nil
	r.appendTailLocked(node)
}

// appendTailLocked links node at the tail.
func (r *LocalTxRing) appendTailLocked(node *ringEntry) {
	if r.tail == nil {
		r.head = node
		r.tail = node
		return
	}
	node.prev = r.tail
	r.tail.next = node
	r.tail = node
}

// removeLocked unlinks node from the list and the map.
func (r *LocalTxRing) removeLocked(node *ringEntry) {
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		r.head = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	} else {
		r.tail = node.prev
	}
	node.prev, node.next = nil, nil
	delete(r.entries, node.key)
}
