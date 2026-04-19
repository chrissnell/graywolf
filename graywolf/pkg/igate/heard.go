package igate

import (
	"context"
	"strings"
	"sync"
	"time"
)

// heardDirectTTL is how long a station remains eligible as an IS→RF
// message recipient after the last time we heard it directly on RF.
// 30 minutes is the conventional window established by the APRS iGate
// spec (http://www.aprs-is.net/IGateDetails.aspx) and matched by
// direwolf and aprsc: long enough for a natural message round-trip,
// short enough that we won't blast RF bandwidth trying to deliver to
// a station that has gone QRT.
const heardDirectTTL = 30 * time.Minute

// heardSweepInterval is the cadence at which expired entries are purged
// from the tracker. The TTL lookup already rejects stale entries, so
// this exists only to bound memory growth on busy channels with lots of
// transient callsigns.
const heardSweepInterval = 5 * time.Minute

// heardDirect tracks callsigns the iGate has heard directly on RF
// (no digipeater relayed the frame yet, i.e. no '*' in the path).
// Entries are refreshed on every direct RF arrival and consulted by
// the IS→RF message gate to decide whether an addressee is reachable.
//
// The APRS iGate spec gates messages IS→RF only when the addressee has
// been heard directly on RF recently — otherwise the iGate would blast
// remote-origin message traffic onto the local channel toward stations
// that aren't listening. The tracker does not apply to non-message
// IS→RF traffic, which graywolf gates via the ownership rule instead
// (see shouldForwardISToRF).
type heardDirect struct {
	mu  sync.Mutex
	m   map[string]time.Time
	now func() time.Time
}

func newHeardDirect() *heardDirect {
	return &heardDirect{m: make(map[string]time.Time), now: time.Now}
}

// Record marks call as heard directly on RF at the current clock. The
// callsign is normalized (trimmed, upper-cased) and also recorded under
// its base-call (no SSID) form, so a message addressed to "N0CALL"
// still matches after we heard "N0CALL-1" — addressees are often
// base-call only while the station transmits with an SSID.
func (h *heardDirect) Record(call string) {
	call = strings.TrimSpace(strings.ToUpper(call))
	if call == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	h.m[call] = now
	if i := strings.IndexByte(call, '-'); i > 0 {
		h.m[call[:i]] = now
	}
}

// HeardWithin reports whether call was heard directly on RF within
// ttl, matching case-insensitively on either the exact call or its
// base (no-SSID) form. A zero or negative ttl always returns false.
func (h *heardDirect) HeardWithin(call string, ttl time.Duration) bool {
	if ttl <= 0 {
		return false
	}
	call = strings.TrimSpace(strings.ToUpper(call))
	if call == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	if t, ok := h.m[call]; ok && now.Sub(t) < ttl {
		return true
	}
	// Try the base-call form of the addressee too: an addressee of
	// "N0CALL-1" should match a recorded "N0CALL" base entry.
	if i := strings.IndexByte(call, '-'); i > 0 {
		if t, ok := h.m[call[:i]]; ok && now.Sub(t) < ttl {
			return true
		}
	}
	return false
}

// sweep removes entries older than ttl. Intended to be invoked on a
// cadence by a long-running sweeper goroutine; the hot-path lookups
// don't rely on it for correctness.
func (h *heardDirect) sweep(ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	cutoff := h.now().Add(-ttl)
	for k, t := range h.m {
		if t.Before(cutoff) {
			delete(h.m, k)
		}
	}
}

// startSweeper launches a goroutine that periodically removes entries
// older than ttl. The goroutine exits when ctx is cancelled, so it's
// naturally tied to the APRS-IS session lifecycle.
func (h *heardDirect) startSweeper(ctx context.Context, ttl, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				h.sweep(ttl)
			}
		}
	}()
}

// pathIsDirect returns true when no path element ends in "*". An '*'
// on any element means some digipeater has already repeated the frame,
// so the source is not guaranteed to be in direct RF range. Only
// digipeater-less ("direct") arrivals qualify the source for the
// heard-direct tracker.
func pathIsDirect(path []string) bool {
	for _, p := range path {
		if strings.HasSuffix(strings.TrimSpace(p), "*") {
			return false
		}
	}
	return true
}
