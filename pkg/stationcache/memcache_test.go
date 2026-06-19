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

func TestMemCache_UpdateObjectSource(t *testing.T) {
	c := newTestCache(t)

	c.Update([]CacheEntry{
		{Key: "obj:MTRC FD", Callsign: "MTRC FD", Source: "KB3OMS", IsObject: true,
			HasPos: true, Lat: 38, Lon: -84, Symbol: [2]byte{'/', ';'},
			Via: "rf", Direction: "RX", Timestamp: time.Now()},
	})

	results := c.QueryBBox(BBox{SwLat: 37, SwLon: -85, NeLat: 39, NeLon: -83}, 1*time.Hour)
	if len(results) != 1 {
		t.Fatalf("expected 1 station, got %d", len(results))
	}
	if results[0].Source != "KB3OMS" {
		t.Fatalf("Source = %q, want KB3OMS", results[0].Source)
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

func TestMemCache_PruneHardCap(t *testing.T) {
	c := NewMemCache(24 * time.Hour)
	t.Cleanup(c.Close)

	// Stuff in MaxStations + 100 entries with monotonically increasing
	// LastHeard timestamps. The lower-indexed entries are older and
	// should be evicted first when the cap kicks in.
	now := time.Now()
	c.mu.Lock()
	for i := 0; i < MaxStations+100; i++ {
		key := "stn:T" + intToBase36(i)
		c.stations[key] = &Station{
			Key:       key,
			Callsign:  key,
			LastHeard: now.Add(time.Duration(i) * time.Second),
		}
	}
	c.mu.Unlock()

	c.prune()

	c.mu.RLock()
	got := len(c.stations)
	_, oldestStillThere := c.stations["stn:T"+intToBase36(0)]
	_, newestStillThere := c.stations["stn:T"+intToBase36(MaxStations+99)]
	c.mu.RUnlock()

	if got != MaxStations {
		t.Fatalf("after prune len(stations) = %d, want %d", got, MaxStations)
	}
	if oldestStillThere {
		t.Fatal("oldest entry should have been evicted by hard cap")
	}
	if !newestStillThere {
		t.Fatal("newest entry should remain after hard cap eviction")
	}
}

func intToBase36(i int) string {
	if i == 0 {
		return "0"
	}
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	var buf [16]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = digits[i%36]
		i /= 36
	}
	return string(buf[pos:])
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

// digiEntry builds a static-position entry for a digipeater whose beacon
// arrived over a digipeated path (Hops > 0).
func digiEntry(key, callsign string, lat, lon float64, hops int) CacheEntry {
	e := stationEntry(key, callsign, lat, lon)
	e.Hops = hops
	e.Via = "WIDE2-1"
	e.Path = []string{"DIGI2*", "WIDE2-1"}
	return e
}

// TestMemCache_DirectRFNotMaskedByDigipeat reproduces issue #130: a static
// station heard directly (hops 0) and then via a digipeater (hops > 0) of
// the same beacon must keep its direct reception on the collapsed position
// so the Live Map Direct RX filter still shows it.
func TestMemCache_DirectRFNotMaskedByDigipeat(t *testing.T) {
	c := newTestCache(t)

	// Direct copy first (hops 0), then the digipeated copy arrives.
	c.Update([]CacheEntry{stationEntry("stn:DIGI1", "DIGI1", 40.0, -105.0)}) // RX, hops 0
	c.Update([]CacheEntry{digiEntry("stn:DIGI1", "DIGI1", 40.0, -105.0, 2)}) // RX, hops 2

	results := c.QueryBBox(BBox{SwLat: 39, SwLon: -106, NeLat: 41, NeLon: -104}, 1*time.Hour)
	if len(results) != 1 || len(results[0].Positions) != 1 {
		t.Fatalf("expected 1 station with 1 position, got %+v", results)
	}
	p := results[0].Positions[0]
	if !isDirectRF(p.Direction, p.Hops) {
		t.Fatalf("direct reception masked by digipeated copy: Direction=%q Hops=%d", p.Direction, p.Hops)
	}

	// A subsequent direct copy must refresh (and still be direct).
	c.Update([]CacheEntry{stationEntry("stn:DIGI1", "DIGI1", 40.0, -105.0)})
	results = c.QueryBBox(BBox{SwLat: 39, SwLon: -106, NeLat: 41, NeLon: -104}, 1*time.Hour)
	p = results[0].Positions[0]
	if !isDirectRF(p.Direction, p.Hops) {
		t.Fatalf("expected direct reception after refresh, got Direction=%q Hops=%d", p.Direction, p.Hops)
	}
}

// TestMemCache_DigipeatedThenDirect verifies the latest-wins path still
// applies for non-direct copies and that a direct copy upgrades the fix.
func TestMemCache_DigipeatedThenDirect(t *testing.T) {
	c := newTestCache(t)

	// Only ever heard via a digipeater so far.
	c.Update([]CacheEntry{digiEntry("stn:DIGI1", "DIGI1", 40.0, -105.0, 2)})
	results := c.QueryBBox(BBox{SwLat: 39, SwLon: -106, NeLat: 41, NeLon: -104}, 1*time.Hour)
	if isDirectRF(results[0].Positions[0].Direction, results[0].Positions[0].Hops) {
		t.Fatal("digipeated-only fix must not be classified as direct")
	}

	// Now a direct copy arrives — it should upgrade the fix to direct.
	c.Update([]CacheEntry{stationEntry("stn:DIGI1", "DIGI1", 40.0, -105.0)})
	results = c.QueryBBox(BBox{SwLat: 39, SwLon: -106, NeLat: 41, NeLon: -104}, 1*time.Hour)
	if !isDirectRF(results[0].Positions[0].Direction, results[0].Positions[0].Hops) {
		t.Fatal("direct copy should upgrade a previously-digipeated fix to direct")
	}
}

// gatedEntry is a position fix that reached us as Internet-to-RF gated
// traffic (the inner packet of a third-party gate): heard on RF (RX) but
// flagged gated, so the "RF Only" filter should exclude it.
func gatedEntry(key, callsign string, lat, lon float64) CacheEntry {
	e := stationEntry(key, callsign, lat, lon)
	e.Gated = true
	e.Path = []string{"TCPIP*", "qAR", "IGATE"}
	return e
}

// TestMemCache_RFCopyNotMaskedByGated verifies the merge keeps an RF-heard
// (non-gated) copy of a static fix even when a later Internet-to-RF gated
// copy of the same beacon arrives, so the "RF Only" filter still shows it.
func TestMemCache_RFCopyNotMaskedByGated(t *testing.T) {
	c := newTestCache(t)

	// Heard over RF via a digipeater first, then a gated copy at the same
	// position. The digipeated (non-gated) reception must be retained.
	c.Update([]CacheEntry{digiEntry("stn:GW1", "GW1", 40.0, -105.0, 2)}) // RX, hops 2, not gated
	c.Update([]CacheEntry{gatedEntry("stn:GW1", "GW1", 40.0, -105.0)})   // RX, gated

	results := c.QueryBBox(BBox{SwLat: 39, SwLon: -106, NeLat: 41, NeLon: -104}, 1*time.Hour)
	if len(results) != 1 || len(results[0].Positions) != 1 {
		t.Fatalf("expected 1 station with 1 position, got %+v", results)
	}
	if results[0].Positions[0].Gated {
		t.Fatal("RF-heard copy masked by later gated copy: Gated=true")
	}

	// The reverse order must upgrade: a gated-only fix that is later heard
	// over RF should drop the gated flag.
	c.Update([]CacheEntry{gatedEntry("stn:GW2", "GW2", 41.0, -105.0)})
	c.Update([]CacheEntry{digiEntry("stn:GW2", "GW2", 41.0, -105.0, 2)})
	results = c.QueryBBox(BBox{SwLat: 40, SwLon: -106, NeLat: 42, NeLon: -104}, 1*time.Hour)
	var gw2 *Station
	for i := range results {
		if results[i].Callsign == "GW2" {
			gw2 = &results[i]
		}
	}
	if gw2 == nil {
		t.Fatal("GW2 not found")
	}
	if gw2.Positions[0].Gated {
		t.Fatal("gated-only fix not upgraded by later RF-heard copy: Gated=true")
	}
}

// TestMemCache_LastDirectHeardSetOnDirect verifies a direct RF reception
// (RX, hops 0) records LastDirectHeard, and a digipeated-only station never
// does (issue #349 — the Direct RX filter keys on this timestamp).
func TestMemCache_LastDirectHeardSetOnDirect(t *testing.T) {
	c := newTestCache(t)

	c.Update([]CacheEntry{stationEntry("stn:DIRECT", "DIRECT", 40.0, -105.0)})    // RX, hops 0
	c.Update([]CacheEntry{digiEntry("stn:DIGIONLY", "DIGIONLY", 41.0, -105.0, 2)}) // RX, hops 2

	results := c.QueryBBox(BBox{SwLat: 39, SwLon: -106, NeLat: 42, NeLon: -104}, 1*time.Hour)
	byKey := map[string]Station{}
	for _, s := range results {
		byKey[s.Key] = s
	}
	if byKey["stn:DIRECT"].LastDirectHeard.IsZero() {
		t.Fatal("direct reception did not set LastDirectHeard")
	}
	if !byKey["stn:DIGIONLY"].LastDirectHeard.IsZero() {
		t.Fatal("digipeated-only station must not set LastDirectHeard")
	}
}

// TestMemCache_LastDirectHeardNotAdvancedByDigi verifies a later digipeated
// copy of a station heard directly earlier does NOT advance LastDirectHeard —
// the direct hearing must age out of the Direct RX window on its own schedule
// (issue #349), even though issue #130 keeps the fix classified as direct.
func TestMemCache_LastDirectHeardNotAdvancedByDigi(t *testing.T) {
	c := newTestCache(t)

	direct := stationEntry("stn:MOBILE", "MOBILE", 40.0, -105.0)
	direct.Timestamp = time.Now().Add(-30 * time.Minute)
	c.Update([]CacheEntry{direct})

	digi := digiEntry("stn:MOBILE", "MOBILE", 40.0, -105.0, 2)
	digi.Timestamp = time.Now()
	c.Update([]CacheEntry{digi})

	results := c.QueryBBox(BBox{SwLat: 39, SwLon: -106, NeLat: 41, NeLon: -104}, 1*time.Hour)
	if len(results) != 1 {
		t.Fatalf("expected 1 station, got %d", len(results))
	}
	if !results[0].LastDirectHeard.Equal(direct.Timestamp) {
		t.Fatalf("LastDirectHeard advanced by digipeated copy: got %v want %v",
			results[0].LastDirectHeard, direct.Timestamp)
	}
	// #130 still holds: the displayed fix is still classified direct.
	p := results[0].Positions[0]
	if !isDirectRF(p.Direction, p.Hops) {
		t.Fatalf("issue #130 regressed: fix no longer direct (Direction=%q Hops=%d)", p.Direction, p.Hops)
	}
}
