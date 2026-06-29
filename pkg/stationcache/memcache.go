package stationcache

import (
	"math"
	"sort"
	"sync"
	"time"
)

// posEpsilon is the threshold below which two positions are considered
// identical (~1m at the equator). Used to deduplicate static re-beacons.
const posEpsilon = 0.00001

// dupWindow bounds how far apart in time two same-location fixes may be
// and still be treated as the same physical beacon re-received over a
// different path (a digipeated, APRS-IS, or second-channel copy that
// arrived after the station has moved on). Copies of a single beacon
// normally arrive within seconds of each other; this window leaves slack
// for APRS-IS and store-and-forward digipeater delays. See issue #421.
const dupWindow = 2 * time.Minute

// MaxStations is the hard upper bound on resident MemCache entries.
// The TTL-based prune (memMaxAge, default 24h) is the primary eviction
// path for routine operation; this cap is a safety bound for the
// pathological case of an IS feed with a broad filter heard over a
// long window. When exceeded, prune evicts oldest-LastHeard first
// until the cap is satisfied. Sized to bound resident memory at
// roughly 25 MiB even with full 200-position trails on every station.
const MaxStations = 50000

// MemCache is an in-memory StationStore for RF-scale traffic.
// Safe for concurrent use.
type MemCache struct {
	mu       sync.RWMutex
	stations map[string]*Station
	gen      uint64        // monotonic generation counter
	maxAge   time.Duration // station TTL for pruning
	done     chan struct{} // signals pruning goroutine to stop
}

var _ StationStore = (*MemCache)(nil)

func NewMemCache(maxAge time.Duration) *MemCache {
	c := &MemCache{
		stations: make(map[string]*Station),
		maxAge:   maxAge,
		done:     make(chan struct{}),
	}
	go c.pruneLoop()
	return c
}

// Update applies cache entries produced by ExtractEntry.
func (c *MemCache) Update(entries []CacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++

	now := time.Now()
	for i := range entries {
		e := &entries[i]

		if e.Killed {
			delete(c.stations, e.Key)
			continue
		}

		s, exists := c.stations[e.Key]

		if !e.HasPos && !exists {
			// Weather-only packet for unknown station — skip.
			continue
		}

		if !e.HasPos && exists {
			// Update metadata only — no position change.
			updateMetadata(s, e, now)
			continue
		}

		if !exists {
			s = &Station{
				Key:      e.Key,
				Callsign: e.Callsign,
				IsObject: e.IsObject,
			}
			c.stations[e.Key] = s
		}

		updateMetadata(s, e, now)

		newPos := Position{
			Lat:       e.Lat,
			Lon:       e.Lon,
			Alt:       e.Alt,
			HasAlt:    e.HasAlt,
			Speed:     e.Speed,
			Course:    e.Course,
			HasCourse: e.HasCourse,
			Via:       e.Via,
			Path:      e.Path,
			Hops:      e.Hops,
			Direction: e.Direction,
			Gated:     e.Gated,
			Channel:   e.Channel,
			Comment:   e.Comment,
			Timestamp: e.Timestamp,
		}

		if len(s.Positions) == 0 {
			s.Positions = []Position{newPos}
		} else if !positionMoved(s.Positions[0], newPos) {
			// Static re-beacon at the current head — advance the last-heard
			// timestamp and the free-form comment from the latest packet.
			// Reception-path metadata (via/path/hops/direction/channel/gated),
			// however, is kept at the *most RF-reachable* copy seen for this
			// fix (see rfRank). A station heard both directly and via a
			// digipeater would otherwise have its direct copy clobbered by a
			// later digipeated one, hiding it from the "Direct RX" filter
			// (issue #130); likewise an RF-heard copy must not be clobbered by
			// a later Internet-to-RF gated copy, which would hide it from the
			// "RF Only" filter. Ties refresh (latest wins).
			mergeReception(&s.Positions[0], e)
			s.Positions[0].Timestamp = e.Timestamp
			s.Positions[0].Comment = e.Comment
		} else if i := duplicateFix(s.Positions, newPos); i >= 0 {
			// Late duplicate of an earlier fix: a digipeated, APRS-IS, or
			// second-channel copy of a beacon the station has since moved on
			// from. Merge its reception metadata into the existing fix rather
			// than inserting a new trail point. Inserting one would make the
			// track double back on itself — a spurious line out to the stale
			// position and back — instead of the chronological dot-to-dot
			// path (issue #421). The existing fix keeps its original
			// timestamp and comment; only path/RF metadata is reconciled.
			mergeReception(&s.Positions[i], e)
		} else {
			// Genuinely new fix. Insert it in timestamp order (newest first)
			// so an out-of-order arrival — common on APRS-IS and digipeated
			// feeds — lands in its correct chronological slot rather than at
			// the head, which would also draw a backward line (issue #421).
			// In the common case the new fix is the newest and this is a
			// plain prepend.
			insertPositionByTime(&s.Positions, newPos)
			if len(s.Positions) > MaxTrailLen {
				s.Positions = s.Positions[:MaxTrailLen]
			}
		}
	}
}

// QueryBBox returns stations within the bounding box heard within maxAge.
func (c *MemCache) QueryBBox(bbox BBox, maxAge time.Duration) []Station {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cutoff := time.Now().Add(-maxAge)
	var out []Station
	for _, s := range c.stations {
		if s.LastHeard.Before(cutoff) {
			continue
		}
		if len(s.Positions) == 0 {
			continue
		}
		// Include the station when ANY position that will be shipped to
		// the client falls inside the bbox, not just the latest fix. A
		// head-only test made a moving station's entire still-visible
		// trail vanish the moment its newest position left the viewport
		// (GH #413). positions[0] is always shipped; positions[1:] ship
		// only when newer than the trail cutoff, so mirror that trimming
		// here to avoid keeping a station for a breadcrumb that won't
		// actually render.
		if !stationInBBox(s, bbox, cutoff) {
			continue
		}
		out = append(out, snapshotStation(s))
	}
	return out
}

// stationInBBox reports whether any of the station's visible positions lie
// within bbox. The head (positions[0]) always counts; older positions count
// only when newer than cutoff, matching stationToDTO's trail trimming.
func stationInBBox(s *Station, bbox BBox, cutoff time.Time) bool {
	for i, p := range s.Positions {
		if i > 0 && p.Timestamp.Before(cutoff) {
			continue
		}
		if p.Lat >= bbox.SwLat && p.Lat <= bbox.NeLat &&
			p.Lon >= bbox.SwLon && p.Lon <= bbox.NeLon {
			return true
		}
	}
	return false
}

// Lookup returns positions for the given callsigns regardless of bbox.
func (c *MemCache) Lookup(callsigns []string) map[string]LatLon {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]LatLon, len(callsigns))
	for _, call := range callsigns {
		if s, ok := c.stations["stn:"+call]; ok && len(s.Positions) > 0 {
			result[call] = LatLon{Lat: s.Positions[0].Lat, Lon: s.Positions[0].Lon}
		}
	}
	return result
}

// Gen returns the current generation counter.
func (c *MemCache) Gen() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.gen
}

// Hydrate bulk-loads stations into the cache, typically from a
// persistent store on startup. Existing entries are not overwritten.
func (c *MemCache) Hydrate(stations map[string]*Station) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, s := range stations {
		if _, exists := c.stations[key]; !exists {
			c.stations[key] = s
		}
	}
	c.gen++
}

// Close signals the pruning goroutine to exit.
func (c *MemCache) Close() {
	close(c.done)
}

func (c *MemCache) pruneLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.prune()
		}
	}
}

func (c *MemCache) prune() {
	c.mu.Lock()
	defer c.mu.Unlock()
	cutoff := time.Now().Add(-c.maxAge)
	for key, s := range c.stations {
		if s.LastHeard.Before(cutoff) {
			delete(c.stations, key)
		}
	}
	// Safety cap: even when every station is fresh enough to survive
	// the TTL pass, bound the map at MaxStations to prevent unbounded
	// growth on pathological IS filter scopes. Drop the
	// oldest-LastHeard entries until the cap is satisfied. Sort cost
	// is O(N log N) but only runs when the cap is breached, which
	// should be rare; the routine TTL pass keeps N << MaxStations
	// for typical operators.
	if len(c.stations) > MaxStations {
		type entry struct {
			key  string
			when time.Time
		}
		all := make([]entry, 0, len(c.stations))
		for k, s := range c.stations {
			all = append(all, entry{key: k, when: s.LastHeard})
		}
		sort.Slice(all, func(i, j int) bool { return all[i].when.Before(all[j].when) })
		drop := len(c.stations) - MaxStations
		for i := 0; i < drop; i++ {
			delete(c.stations, all[i].key)
		}
	}
}

func updateMetadata(s *Station, e *CacheEntry, now time.Time) {
	if e.Symbol != [2]byte{} {
		s.Symbol = e.Symbol
	}
	// Source is the object/item originator. Only overwrite from a packet
	// that actually carries one, so a malformed/source-less update can't
	// blank a previously-recorded author (mirrors the Symbol guard above).
	// A genuine re-originator arrives with a non-empty Source, so the
	// latest author still wins.
	if e.Source != "" {
		s.Source = e.Source
	}
	s.Via = e.Via
	s.Path = e.Path
	s.Hops = e.Hops
	s.Direction = e.Direction
	s.Gated = e.Gated
	s.Channel = e.Channel
	s.Comment = e.Comment
	s.LastHeard = now
	if isDirectRF(e.Direction, e.Hops) {
		s.LastDirectHeard = e.Timestamp
	}
	if e.Weather != nil {
		s.Weather = e.Weather
	}
}

// isDirectRF reports whether a packet was heard directly over RF with no
// digipeater hops. This is the reception the Live Map "Direct RX" filter
// keys on; a fix that has ever been heard this way must not be downgraded
// by a subsequent digipeated copy of the same beacon (issue #130).
func isDirectRF(direction string, hops int) bool {
	return direction == "RX" && hops == 0
}

// rfRank scores a reception copy by how strongly it demonstrates RF
// reachability, so the static-rebeacon merge keeps the best copy seen for
// a fix. Higher wins; equal ranks refresh (latest wins). This generalizes
// the issue-#130 "most direct" rule to also protect the "RF Only" filter:
//
//	2  heard directly on RF (RX, no digi hops, not gated)
//	1  heard over RF via digipeater(s) (RX, not gated)
//	0  Internet-to-RF gated, APRS-IS, or our own TX
//
// A direct copy thus never gets masked by a digipeated one, and an
// RF-heard copy never gets masked by an Internet-to-RF gated one.
func rfRank(direction string, hops int, gated bool) int {
	if direction != "RX" || gated {
		return 0
	}
	if hops == 0 {
		return 2
	}
	return 1
}

func positionMoved(old, new Position) bool {
	return math.Abs(old.Lat-new.Lat) > posEpsilon ||
		math.Abs(old.Lon-new.Lon) > posEpsilon
}

// mergeReception folds a re-received copy of a fix into an existing trail
// point, keeping the most RF-reachable reception metadata (see rfRank).
// Ties refresh (latest wins). It does not touch the point's timestamp,
// comment, or coordinates — callers decide whether those advance.
func mergeReception(p *Position, e *CacheEntry) {
	if rfRank(e.Direction, e.Hops, e.Gated) >= rfRank(p.Direction, p.Hops, p.Gated) {
		p.Via = e.Via
		p.Path = e.Path
		p.Hops = e.Hops
		p.Direction = e.Direction
		p.Gated = e.Gated
		p.Channel = e.Channel
	}
}

// duplicateFix reports the index of an existing trail point that the
// incoming fix is a re-reception of: the same location (within posEpsilon)
// reported within dupWindow of an existing fix. The head (index 0) is
// handled separately by the static-rebeacon path, so the search starts at
// index 1. Returns -1 when the fix is genuinely new. See issue #421.
//
// This scan cannot be skipped for fixes whose timestamp is newer than the
// head: a non-timestamped APRS packet is stamped with its reception time,
// so a delayed copy of an earlier beacon arrives with the newest timestamp
// of all and is only identifiable by location + dupWindow, never by
// ordering. The scan is bounded, not full-trail: positions are newest-first
// by timestamp, so once an entry falls below the window's lower edge no
// older entry can match and we stop.
func duplicateFix(positions []Position, p Position) int {
	lowerBound := p.Timestamp.Add(-dupWindow)
	for i := 1; i < len(positions); i++ {
		if positions[i].Timestamp.Before(lowerBound) {
			break
		}
		if positionMoved(positions[i], p) {
			continue
		}
		dt := positions[i].Timestamp.Sub(p.Timestamp)
		if dt < 0 {
			dt = -dt
		}
		if dt <= dupWindow {
			return i
		}
	}
	return -1
}

// insertPositionByTime inserts p into a newest-first-by-timestamp slice,
// preserving that ordering. Equal timestamps place p ahead of the existing
// entry, so the common in-order arrival is a plain prepend.
func insertPositionByTime(positions *[]Position, p Position) {
	ps := *positions
	idx := 0
	for idx < len(ps) && ps[idx].Timestamp.After(p.Timestamp) {
		idx++
	}
	ps = append(ps, Position{}) // grow
	copy(ps[idx+1:], ps[idx:])
	ps[idx] = p
	*positions = ps
}

// snapshotStation returns a deep-enough copy of Station so the caller
// can't mutate cache state. Slice-typed fields (Path on both Station
// and each Position) are duplicated.
func snapshotStation(s *Station) Station {
	cp := *s
	cp.Positions = make([]Position, len(s.Positions))
	copy(cp.Positions, s.Positions)
	for i := range cp.Positions {
		if cp.Positions[i].Path != nil {
			cp.Positions[i].Path = append([]string(nil), cp.Positions[i].Path...)
		}
	}
	if s.Path != nil {
		cp.Path = make([]string, len(s.Path))
		copy(cp.Path, s.Path)
	}
	return cp
}
