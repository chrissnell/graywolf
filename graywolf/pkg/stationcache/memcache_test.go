package stationcache

import (
	"testing"
	"time"
)

func newTestCache(t *testing.T) *MemCache {
	t.Helper()
	c := NewMemCache(2 * time.Hour)
	t.Cleanup(c.Close)
	return c
}

func stationEntry(key, callsign string, lat, lon float64) CacheEntry {
	return CacheEntry{
		Key:       key,
		Callsign:  callsign,
		HasPos:    true,
		Lat:       lat,
		Lon:       lon,
		Symbol:    [2]byte{'/', '>'},
		Via:       "rf",
		Direction: "RX",
		Timestamp: time.Now(),
	}
}

func TestMemCache_UpdateAndQueryBBox(t *testing.T) {
	c := newTestCache(t)

	c.Update([]CacheEntry{
		stationEntry("stn:W1ABC", "W1ABC", 40.0, -105.0),
		stationEntry("stn:W2DEF", "W2DEF", 42.0, -103.0),
		stationEntry("stn:W3GHI", "W3GHI", 50.0, -80.0), // outside bbox
	})

	bbox := BBox{SwLat: 39, SwLon: -106, NeLat: 43, NeLon: -102}
	results := c.QueryBBox(bbox, 1*time.Hour)
	if len(results) != 2 {
		t.Fatalf("expected 2 stations in bbox, got %d", len(results))
	}

	// Verify snapshot isolation — mutating result shouldn't affect cache
	results[0].Comment = "mutated"
	results2 := c.QueryBBox(bbox, 1*time.Hour)
	if results2[0].Comment == "mutated" || (len(results2) > 1 && results2[1].Comment == "mutated") {
		t.Fatal("QueryBBox did not return isolated snapshot")
	}
}

func TestMemCache_UpdateKilledObject(t *testing.T) {
	c := newTestCache(t)

	c.Update([]CacheEntry{
		{Key: "obj:SHELTER1", Callsign: "SHELTER1", IsObject: true,
			HasPos: true, Lat: 40, Lon: -105, Symbol: [2]byte{'\\', 'S'},
			Via: "rf", Direction: "RX", Timestamp: time.Now()},
	})

	results := c.QueryBBox(BBox{SwLat: 39, SwLon: -106, NeLat: 41, NeLon: -104}, 1*time.Hour)
	if len(results) != 1 {
		t.Fatalf("expected 1 station, got %d", len(results))
	}

	// Kill the object
	c.Update([]CacheEntry{
		{Key: "obj:SHELTER1", Killed: true},
	})

	results = c.QueryBBox(BBox{SwLat: 39, SwLon: -106, NeLat: 41, NeLon: -104}, 1*time.Hour)
	if len(results) != 0 {
		t.Fatalf("expected 0 stations after kill, got %d", len(results))
	}
}

func TestMemCache_StaticDedup(t *testing.T) {
	c := newTestCache(t)

	// Beacon same position 5 times
	for i := 0; i < 5; i++ {
		c.Update([]CacheEntry{stationEntry("stn:DIGI1", "DIGI1", 40.0, -105.0)})
	}

	results := c.QueryBBox(BBox{SwLat: 39, SwLon: -106, NeLat: 41, NeLon: -104}, 1*time.Hour)
	if len(results) != 1 {
		t.Fatalf("expected 1 station, got %d", len(results))
	}
	if len(results[0].Positions) != 1 {
		t.Fatalf("static station should have 1 position, got %d", len(results[0].Positions))
	}
}

func TestMemCache_MovingStationTrail(t *testing.T) {
	c := newTestCache(t)

	// Station moves through 5 distinct positions
	for i := 0; i < 5; i++ {
		lat := 40.0 + float64(i)*0.01
		c.Update([]CacheEntry{stationEntry("stn:CAR1", "CAR1", lat, -105.0)})
	}

	results := c.QueryBBox(BBox{SwLat: 39, SwLon: -106, NeLat: 41, NeLon: -104}, 1*time.Hour)
	if len(results) != 1 {
		t.Fatalf("expected 1 station, got %d", len(results))
	}
	if len(results[0].Positions) != 5 {
		t.Fatalf("moving station should have 5 positions, got %d", len(results[0].Positions))
	}
	// Newest first
	assertFloat(t, "newest lat", results[0].Positions[0].Lat, 40.04)
	assertFloat(t, "oldest lat", results[0].Positions[4].Lat, 40.0)
}

func TestMemCache_TrailCap(t *testing.T) {
	c := newTestCache(t)

	for i := 0; i < MaxTrailLen+50; i++ {
		lat := 40.0 + float64(i)*0.001
		c.Update([]CacheEntry{stationEntry("stn:CAR1", "CAR1", lat, -105.0)})
	}

	results := c.QueryBBox(BBox{SwLat: 0, SwLon: -180, NeLat: 90, NeLon: 180}, 1*time.Hour)
	if len(results[0].Positions) != MaxTrailLen {
		t.Fatalf("trail should be capped at %d, got %d", MaxTrailLen, len(results[0].Positions))
	}
}

func TestMemCache_WeatherOnlyForExistingStation(t *testing.T) {
	c := newTestCache(t)

	// Create station with position
	c.Update([]CacheEntry{stationEntry("stn:WX1", "WX1", 40.0, -105.0)})

	// Weather-only update (no position)
	c.Update([]CacheEntry{
		{Key: "stn:WX1", Callsign: "WX1", HasPos: false,
			Via: "rf", Direction: "RX", Timestamp: time.Now(),
			Weather: &Weather{Temp: 72.5, HasTemp: true}},
	})

	results := c.QueryBBox(BBox{SwLat: 39, SwLon: -106, NeLat: 41, NeLon: -104}, 1*time.Hour)
	if len(results) != 1 {
		t.Fatalf("expected 1 station, got %d", len(results))
	}
	if results[0].Weather == nil || !results[0].Weather.HasTemp {
		t.Fatal("expected weather data after metadata-only update")
	}
	// Position should remain unchanged
	assertFloat(t, "Lat", results[0].Positions[0].Lat, 40.0)
}

func TestMemCache_WeatherOnlyForUnknownStation(t *testing.T) {
	c := newTestCache(t)

	// Weather-only update for unknown station — should be skipped
	c.Update([]CacheEntry{
		{Key: "stn:WXUNK", Callsign: "WXUNK", HasPos: false,
			Via: "rf", Direction: "RX", Timestamp: time.Now(),
			Weather: &Weather{Temp: 72.5, HasTemp: true}},
	})

	results := c.QueryBBox(BBox{SwLat: -90, SwLon: -180, NeLat: 90, NeLon: 180}, 1*time.Hour)
	if len(results) != 0 {
		t.Fatalf("expected 0 stations for weather-only unknown, got %d", len(results))
	}
}

func TestMemCache_Lookup(t *testing.T) {
	c := newTestCache(t)

	c.Update([]CacheEntry{
		stationEntry("stn:DIGI1", "DIGI1", 40.0, -105.0),
		stationEntry("stn:DIGI2", "DIGI2", 41.0, -106.0),
	})

	result := c.Lookup([]string{"DIGI1", "DIGI2", "UNKNOWN"})
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	assertFloat(t, "DIGI1 lat", result["DIGI1"].Lat, 40.0)
	assertFloat(t, "DIGI2 lat", result["DIGI2"].Lat, 41.0)
	if _, ok := result["UNKNOWN"]; ok {
		t.Fatal("UNKNOWN should not be in result")
	}
}

func TestMemCache_Gen(t *testing.T) {
	c := newTestCache(t)

	g0 := c.Gen()
	c.Update([]CacheEntry{stationEntry("stn:W1ABC", "W1ABC", 40.0, -105.0)})
	g1 := c.Gen()
	if g1 <= g0 {
		t.Fatalf("gen should increase: %d -> %d", g0, g1)
	}

	c.Update([]CacheEntry{stationEntry("stn:W1ABC", "W1ABC", 40.0, -105.0)})
	g2 := c.Gen()
	if g2 <= g1 {
		t.Fatalf("gen should increase even with same data: %d -> %d", g1, g2)
	}
}

func TestMemCache_QueryBBoxMaxAge(t *testing.T) {
	c := newTestCache(t)

	c.Update([]CacheEntry{stationEntry("stn:W1OLD", "W1OLD", 40.0, -105.0)})

	bbox := BBox{SwLat: 39, SwLon: -106, NeLat: 41, NeLon: -104}

	// Should be visible with 1-hour maxAge
	results := c.QueryBBox(bbox, 1*time.Hour)
	if len(results) != 1 {
		t.Fatalf("expected 1 station, got %d", len(results))
	}

	// Manually age the station's LastHeard
	c.mu.Lock()
	c.stations["stn:W1OLD"].LastHeard = time.Now().Add(-2 * time.Hour)
	c.mu.Unlock()

	results = c.QueryBBox(bbox, 1*time.Hour)
	if len(results) != 0 {
		t.Fatalf("expected 0 stations after aging, got %d", len(results))
	}
}

func TestMemCache_Prune(t *testing.T) {
	c := newTestCache(t)

	c.Update([]CacheEntry{stationEntry("stn:W1ABC", "W1ABC", 40.0, -105.0)})

	// Manually age it past maxAge
	c.mu.Lock()
	c.stations["stn:W1ABC"].LastHeard = time.Now().Add(-3 * time.Hour)
	c.mu.Unlock()

	c.prune()

	c.mu.RLock()
	_, exists := c.stations["stn:W1ABC"]
	c.mu.RUnlock()
	if exists {
		t.Fatal("station should have been pruned")
	}
}

func TestMemCache_MetadataUpdate(t *testing.T) {
	c := newTestCache(t)

	// Initial entry
	c.Update([]CacheEntry{
		{Key: "stn:W1ABC", Callsign: "W1ABC", HasPos: true,
			Lat: 40.0, Lon: -105.0, Symbol: [2]byte{'/', '>'},
			Via: "rf", Direction: "RX", Channel: 0, Comment: "first",
			Timestamp: time.Now()},
	})

	// Update with new metadata but same position
	c.Update([]CacheEntry{
		{Key: "stn:W1ABC", Callsign: "W1ABC", HasPos: true,
			Lat: 40.0, Lon: -105.0, Symbol: [2]byte{'/', 'k'},
			Via: "is", Direction: "IS", Channel: 1, Comment: "updated",
			Timestamp: time.Now()},
	})

	results := c.QueryBBox(BBox{SwLat: 39, SwLon: -106, NeLat: 41, NeLon: -104}, 1*time.Hour)
	if len(results) != 1 {
		t.Fatalf("expected 1 station, got %d", len(results))
	}
	s := results[0]
	assertEqual(t, "Symbol", s.Symbol, [2]byte{'/', 'k'})
	assertEqual(t, "Via", s.Via, "is")
	assertEqual(t, "Direction", s.Direction, "IS")
	assertEqual(t, "Channel", s.Channel, uint32(1))
	assertEqual(t, "Comment", s.Comment, "updated")
	// Position trail should still be 1 (didn't move)
	if len(s.Positions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(s.Positions))
	}
}

func TestMemCache_CompositeKeyIsolation(t *testing.T) {
	c := newTestCache(t)

	// Station and object with same name must be separate entries
	c.Update([]CacheEntry{
		stationEntry("stn:W1ABC", "W1ABC", 40.0, -105.0),
		{Key: "obj:W1ABC", Callsign: "W1ABC", IsObject: true, HasPos: true,
			Lat: 42.0, Lon: -103.0, Symbol: [2]byte{'\\', 'S'},
			Via: "rf", Direction: "RX", Timestamp: time.Now()},
	})

	results := c.QueryBBox(BBox{SwLat: 39, SwLon: -106, NeLat: 43, NeLon: -102}, 1*time.Hour)
	if len(results) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(results))
	}
}

func TestMemCache_PositionEpsilon(t *testing.T) {
	c := newTestCache(t)

	// Move within epsilon — should NOT create a new trail point
	c.Update([]CacheEntry{stationEntry("stn:W1ABC", "W1ABC", 40.0, -105.0)})
	c.Update([]CacheEntry{stationEntry("stn:W1ABC", "W1ABC", 40.0+posEpsilon*0.5, -105.0)})

	c.mu.RLock()
	positions := len(c.stations["stn:W1ABC"].Positions)
	c.mu.RUnlock()
	if positions != 1 {
		t.Fatalf("sub-epsilon movement should not create trail point, got %d positions", positions)
	}

	// Move beyond epsilon — should create a new trail point
	c.Update([]CacheEntry{stationEntry("stn:W1ABC", "W1ABC", 40.0+posEpsilon*2, -105.0)})

	c.mu.RLock()
	positions = len(c.stations["stn:W1ABC"].Positions)
	c.mu.RUnlock()
	if positions != 2 {
		t.Fatalf("above-epsilon movement should create trail point, got %d positions", positions)
	}
}
