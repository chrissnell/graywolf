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
		} else if positionMoved(s.Positions[0], newPos) {
			// Station moved — prepend new position, cap trail.
			s.Positions = append(s.Positions, Position{}) // grow
			copy(s.Positions[1:], s.Positions)
			s.Positions[0] = newPos
			if len(s.Positions) > MaxTrailLen {
				s.Positions = s.Positions[:MaxTrailLen]
			}
		} else {
			// Static re-beacon — advance the last-heard timestamp and the
			// free-form comment from the latest packet. Reception-path
			// metadata (via/path/hops/direction/channel/gated), however, is
			// kept at the *most RF-reachable* copy seen for this fix (see
			// rfRank). A station heard both directly and via a digipeater
			// would otherwise have its direct copy clobbered by a later
			// digipeated one, hiding it from the "Direct RX" filter (issue
			// #130); likewise an RF-heard copy must not be clobbered by a
			// later Internet-to-RF gated copy, which would hide it from the
			// "RF Only" filter. Ties refresh (latest wins).
			s.Positions[0].Timestamp = e.Timestamp
			s.Positions[0].Comment = e.Comment
			if rfRank(e.Direction, e.Hops, e.Gated) >= rfRank(s.Positions[0].Direction, s.Positions[0].Hops, s.Positions[0].Gated) {
				s.Positions[0].Via = e.Via
				s.Positions[0].Path = e.Path
				s.Positions[0].Hops = e.Hops
				s.Positions[0].Direction = e.Direction
				s.Positions[0].Gated = e.Gated
				s.Positions[0].Channel = e.Channel
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
		p := s.Positions[0]
		if p.Lat < bbox.SwLat || p.Lat > bbox.NeLat ||
			p.Lon < bbox.SwLon || p.Lon > bbox.NeLon {
			continue
		}
		out = append(out, snapshotStation(s))
	}
	return out
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
	s.Via = e.Via
	s.Path = e.Path
	s.Hops = e.Hops
	s.Direction = e.Direction
	s.Gated = e.Gated
	s.Channel = e.Channel
	s.Comment = e.Comment
	s.LastHeard = now
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
