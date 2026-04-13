package stationcache

import (
	"math"
	"sync"
	"time"
)

// posEpsilon is the threshold below which two positions are considered
// identical (~1m at the equator). Used to deduplicate static re-beacons.
const posEpsilon = 0.00001

// MemCache is an in-memory StationStore for RF-scale traffic.
// Safe for concurrent use.
type MemCache struct {
	mu       sync.RWMutex
	stations map[string]*Station
	gen      uint64        // monotonic generation counter
	maxAge   time.Duration // station TTL for pruning
	done     chan struct{}  // signals pruning goroutine to stop
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
			// Static re-beacon — update timestamp only.
			s.Positions[0].Timestamp = e.Timestamp
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
}

func updateMetadata(s *Station, e *CacheEntry, now time.Time) {
	if e.Symbol != [2]byte{} {
		s.Symbol = e.Symbol
	}
	s.Via = e.Via
	s.Path = e.Path
	s.Hops = e.Hops
	s.Direction = e.Direction
	s.Channel = e.Channel
	s.Comment = e.Comment
	s.LastHeard = now
	if e.Weather != nil {
		s.Weather = e.Weather
	}
}

func positionMoved(old, new Position) bool {
	return math.Abs(old.Lat-new.Lat) > posEpsilon ||
		math.Abs(old.Lon-new.Lon) > posEpsilon
}

// snapshotStation returns a shallow copy of Station with its own
// Positions slice so the caller can't mutate cache state.
func snapshotStation(s *Station) Station {
	cp := *s
	cp.Positions = make([]Position, len(s.Positions))
	copy(cp.Positions, s.Positions)
	if s.Path != nil {
		cp.Path = make([]string, len(s.Path))
		copy(cp.Path, s.Path)
	}
	return cp
}
