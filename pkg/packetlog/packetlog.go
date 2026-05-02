// Package packetlog is a bounded in-memory ring buffer of RX/TX/IS
// packet records with a filtered query API. Other packages register as
// hooks via the Hook interface and every subsystem on the packet path
// (modembridge RX, txgovernor TX, iGate IS upload) funnels through a
// single Log instance so the web UI and REST API can render a unified
// packet stream.
package packetlog

import (
	"sync"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

// Direction labels a packet's flow.
type Direction string

const (
	DirRX Direction = "RX" // heard on the air
	DirTX Direction = "TX" // transmitted by us
	DirIS Direction = "IS" // APRS-IS upload/download (iGate)
)

// Entry is one recorded packet.
type Entry struct {
	// Timestamp is the UTC RFC3339 time the packet was recorded.
	Timestamp time.Time `json:"timestamp"`
	// Channel is the graywolf channel ID that observed or transmitted the packet.
	Channel uint32 `json:"channel"`
	// Direction labels the flow: "RX" (heard on air), "TX" (transmitted by us), or "IS" (APRS-IS upload/download).
	Direction Direction `json:"direction"`
	// Source identifies the subsystem that produced this entry: "kiss", "agw", "digi", "igate-tx", "beacon", "modem", or "igate-is".
	Source string `json:"source"`
	// Raw is the on-air AX.25 frame bytes with FCS stripped; omitted for entries without raw framing.
	Raw []byte `json:"raw,omitempty"`
	// Display is a direwolf-style human-readable rendering: "SRC>DEST[,DIGI*]:info".
	Display string `json:"display"`
	// Type is the APRS packet type (position, message, status, ...) when the payload decoded successfully.
	Type string `json:"type,omitempty"`
	// Decoded is the parsed APRS payload when decoding succeeded; nil otherwise.
	Decoded *aprs.DecodedAPRSPacket `json:"decoded,omitempty"`
	// Notes is a short annotation describing how this entry was handled (e.g. "deduped", "rate-limited", "digi consumed WIDE1-1").
	Notes string `json:"notes,omitempty"`
}

// Hook lets other packages record packets into the log without taking
// a hard dependency on *Log's concrete type.
type Hook interface {
	Record(e Entry)
}

// Config tunes a Log.
type Config struct {
	// Capacity is the maximum number of entries retained. Default 1000.
	Capacity int
	// MaxAge bounds how old entries can be before GC. Default 30 minutes.
	// Entries are pruned lazily on Record and on Query.
	MaxAge time.Duration
}

// Log is a thread-safe ring buffer of Entry values with a time-limited
// retention window.
type Log struct {
	cap    int
	maxAge time.Duration

	mu      sync.RWMutex
	entries []Entry // circular buffer
	head    int     // next write index
	size    int     // number of valid entries (<= cap)
	nextID  uint64  // monotonic id (unused externally; kept for future pagination)

	// Subscribe fanout state. subMu guards the map and the per-subscriber
	// channels. Held inside Record after the ring write, so Record stays
	// O(num-subscribers).
	subMu sync.Mutex
	subs  map[*subscriber]struct{}
}

// New builds an empty Log.
func New(cfg Config) *Log {
	if cfg.Capacity <= 0 {
		cfg.Capacity = 1000
	}
	if cfg.MaxAge <= 0 {
		cfg.MaxAge = 30 * time.Minute
	}
	return &Log{
		cap:     cfg.Capacity,
		maxAge:  cfg.MaxAge,
		entries: make([]Entry, cfg.Capacity),
	}
}

// Record inserts e into the ring, evicting the oldest entry if full.
// Safe for concurrent use. Zero-valued Timestamp is filled in with now.
func (l *Log) Record(e Entry) {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries[l.head] = e
	l.head = (l.head + 1) % l.cap
	if l.size < l.cap {
		l.size++
	}
	l.nextID++
	l.gcLocked(e.Timestamp)
	// Fan out under the ring lock so a slow subscriber can never see
	// out-of-order entries (records publish in the same order they hit
	// the ring). The fanout itself is non-blocking; see subscribe.go.
	l.fanout(e)
}

// gcLocked drops entries older than maxAge. Callers hold l.mu.
// Because entries are written in time order, we can find the oldest
// logical index and advance past stale ones. For simplicity we scan
// linearly from the oldest; the buffer is bounded so this is cheap.
func (l *Log) gcLocked(now time.Time) {
	if l.size == 0 || l.maxAge <= 0 {
		return
	}
	cutoff := now.Add(-l.maxAge)
	// Oldest entry index.
	start := (l.head - l.size + l.cap) % l.cap
	dropped := 0
	for dropped < l.size {
		idx := (start + dropped) % l.cap
		if !l.entries[idx].Timestamp.Before(cutoff) {
			break
		}
		// Zero out to release pointers held by Decoded/Raw.
		l.entries[idx] = Entry{}
		dropped++
	}
	l.size -= dropped
}

// Len returns the current number of stored entries.
func (l *Log) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.size
}

// Capacity returns the configured maximum.
func (l *Log) Capacity() int { return l.cap }

// Filter narrows a Query.
type Filter struct {
	Since     time.Time // zero = no lower bound
	Source    string    // empty = any
	Type      string    // empty = any; matches Entry.Type
	Direction Direction // empty = any
	Channel   int       // -1 = any; otherwise match Channel exactly
	Limit     int       // <=0 = no cap (beyond ring size)
}

// Query returns entries matching f in chronological order (oldest
// first). A copy is made under the lock so callers can iterate without
// holding it.
func (l *Log) Query(f Filter) []Entry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.size == 0 {
		return nil
	}
	out := make([]Entry, 0, l.size)
	start := (l.head - l.size + l.cap) % l.cap
	for i := 0; i < l.size; i++ {
		idx := (start + i) % l.cap
		e := l.entries[idx]
		if !f.Since.IsZero() && e.Timestamp.Before(f.Since) {
			continue
		}
		if f.Source != "" && e.Source != f.Source {
			continue
		}
		if f.Type != "" && e.Type != f.Type {
			continue
		}
		if f.Direction != "" && e.Direction != f.Direction {
			continue
		}
		if f.Channel > 0 && uint32(f.Channel) != e.Channel {
			continue
		}
		out = append(out, e)
	}
	// Return the newest entries when limit is set
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[len(out)-f.Limit:]
	}
	return out
}

// Stats exposes gauges for metrics.
type Stats struct {
	Entries  int
	Capacity int
}

// Stats returns a snapshot.
func (l *Log) Stats() Stats {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return Stats{Entries: l.size, Capacity: l.cap}
}
