package gps

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"
)

// fixedRefreshInterval is how often RunFixed re-stamps the configured
// coordinate into the cache. A fixed coordinate never changes, but
// consumers that reason about fix freshness (and the /api/position
// timestamp) should keep seeing a recent fix, so we re-publish it
// periodically the way a live receiver would.
const fixedRefreshInterval = 30 * time.Second

// FixedConfig is a manually-entered station coordinate used as the GPS
// source. Latitude/Longitude are decimal degrees; Altitude is metres
// above MSL and is optional (0 means "not specified" and is reported as
// no altitude, matching the fixed-beacon convention).
type FixedConfig struct {
	Latitude  float64
	Longitude float64
	Altitude  float64
}

// ValidateCoordinates checks that lat/lon fall within the valid WGS-84
// ranges. Shared by the REST DTO and RunFixed so the same error text is
// surfaced whether a bad value arrives over the API or from the store.
func ValidateCoordinates(lat, lon float64) error {
	if math.IsNaN(lat) || math.IsInf(lat, 0) || lat < -90 || lat > 90 {
		return fmt.Errorf("latitude %g out of range (-90..90)", lat)
	}
	if math.IsNaN(lon) || math.IsInf(lon, 0) || lon < -180 || lon > 180 {
		return fmt.Errorf("longitude %g out of range (-180..180)", lon)
	}
	return nil
}

// fix builds the Fix published by RunFixed, stamped at t.
func (c FixedConfig) fix(t time.Time) Fix {
	f := Fix{
		Latitude:  c.Latitude,
		Longitude: c.Longitude,
		Timestamp: t,
		FixMode:   3, // treat a known coordinate as a 3D fix
	}
	if c.Altitude != 0 {
		f.Altitude = c.Altitude
		f.HasAlt = true
	}
	return f
}

// RunFixed publishes a static coordinate into cache and keeps it fresh
// until ctx is cancelled. It returns an error immediately if the
// coordinate is invalid, mirroring how the live readers fail fast on a
// bad configuration.
func RunFixed(ctx context.Context, cfg FixedConfig, cache PositionCache, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	if err := ValidateCoordinates(cfg.Latitude, cfg.Longitude); err != nil {
		return fmt.Errorf("gps: fixed coordinate: %w", err)
	}

	cache.Update(cfg.fix(time.Now().UTC()))
	logger.Info("fixed gps coordinate published", "lat", cfg.Latitude, "lon", cfg.Longitude, "alt_m", cfg.Altitude)

	ticker := time.NewTicker(fixedRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			cache.Update(cfg.fix(time.Now().UTC()))
		}
	}
}
