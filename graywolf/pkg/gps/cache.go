// Package gps provides a unified GPS position cache with NMEA-serial and
// gpsd (TCP JSON) reader implementations. Beacon schedulers, SmartBeaconing,
// and REST endpoints read the latest fix through the PositionCache
// interface; reader goroutines push updates via Update.
package gps

import (
	"sync"
	"time"
)

// Fix is a single GPS observation. Zero Timestamp indicates "no fix yet".
type Fix struct {
	Latitude  float64   // degrees, north positive
	Longitude float64   // degrees, east positive
	Altitude  float64   // metres above MSL
	HasAlt    bool
	Speed     float64   // knots (APRS/NMEA canonical unit)
	Heading   float64   // degrees true, 0..360
	HasCourse bool      // true if Speed/Heading are valid for this fix
	Timestamp time.Time // UTC
	FixMode   int       // 0=unknown, 1=no fix, 2=2D, 3=3D (from GSA)
	PDOP      float64   // position dilution of precision
	HDOP      float64   // horizontal dilution of precision
	VDOP      float64   // vertical dilution of precision
	HasDOP    bool      // true if DOP values are valid
}

// PositionCache is the read/write contract shared by readers and consumers.
// Implementations MUST be safe for concurrent use.
type PositionCache interface {
	// Get returns the latest fix and whether any fix has been stored.
	Get() (Fix, bool)
	// Update stores a new fix. Readers call this from their goroutines.
	Update(Fix)
}

// MemCache is a sync.RWMutex-protected in-memory PositionCache. The zero
// value is a valid empty cache.
type MemCache struct {
	mu    sync.RWMutex
	fix   Fix
	valid bool
}

// NewMemCache returns an empty MemCache.
func NewMemCache() *MemCache { return &MemCache{} }

// Get returns a copy of the latest fix.
func (c *MemCache) Get() (Fix, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.fix, c.valid
}

// Update replaces the stored fix. A Fix with zero Timestamp is stamped
// with time.Now() so downstream code always sees a monotonic freshness.
func (c *MemCache) Update(f Fix) {
	if f.Timestamp.IsZero() {
		f.Timestamp = time.Now().UTC()
	}
	c.mu.Lock()
	c.fix = f
	c.valid = true
	c.mu.Unlock()
}
