package gps

import (
	"sync"
	"time"
)

// StationPos is a PositionCache that layers a GPS cache over an
// optional fixed fallback position. Get returns the GPS fix when
// available; otherwise it returns the fallback (typically the
// station's fixed beacon coordinates). Update delegates to the
// underlying GPS cache.
type StationPos struct {
	gps *MemCache

	mu       sync.RWMutex
	fallback Fix
	hasFB    bool
}

// NewStationPos wraps a GPS cache with an optional fixed-position
// fallback. The fallback is initially empty; call SetFallback to
// populate it from beacon configs.
func NewStationPos(gps *MemCache) *StationPos {
	return &StationPos{gps: gps}
}

// PositionSource indicates where a position came from.
type PositionSource int

const (
	SourceNone  PositionSource = iota // no position available
	SourceGPS                         // live GPS receiver
	SourceFixed                       // static beacon coordinates
)

// Get returns the latest GPS fix if available, otherwise the fallback.
func (s *StationPos) Get() (Fix, bool) {
	if fix, ok := s.gps.Get(); ok {
		return fix, true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.fallback, s.hasFB
}

// GetWithSource is like Get but also reports the position source.
func (s *StationPos) GetWithSource() (Fix, PositionSource) {
	if fix, ok := s.gps.Get(); ok {
		return fix, SourceGPS
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.hasFB {
		return s.fallback, SourceFixed
	}
	return Fix{}, SourceNone
}

// Update delegates to the underlying GPS cache.
func (s *StationPos) Update(f Fix) {
	s.gps.Update(f)
}

// SetFallback sets the fixed-position fallback from a beacon's static
// coordinates. Pass nil to clear.
func (s *StationPos) SetFallback(f *Fix) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f == nil {
		s.fallback = Fix{}
		s.hasFB = false
		return
	}
	fb := *f
	if fb.Timestamp.IsZero() {
		fb.Timestamp = time.Now().UTC()
	}
	s.fallback = fb
	s.hasFB = true
}

// GetSatellites delegates to the underlying GPS cache.
func (s *StationPos) GetSatellites() (SatelliteView, bool) {
	return s.gps.GetSatellites()
}

// UpdateSatellites delegates to the underlying GPS cache.
func (s *StationPos) UpdateSatellites(v SatelliteView) {
	s.gps.UpdateSatellites(v)
}
