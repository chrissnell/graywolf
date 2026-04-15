package gps

import "testing"

func TestStationPos_GPSOverridesFallback(t *testing.T) {
	cache := NewMemCache()
	sp := NewStationPos(cache)

	sp.SetFallback(&Fix{Latitude: 35.0, Longitude: -106.0})

	// Fallback should be returned when GPS has no fix.
	fix, ok := sp.Get()
	if !ok {
		t.Fatal("expected fallback position")
	}
	if fix.Latitude != 35.0 || fix.Longitude != -106.0 {
		t.Fatalf("got %v/%v, want 35/-106", fix.Latitude, fix.Longitude)
	}
	if fix.Timestamp.IsZero() {
		t.Fatal("fallback timestamp should be set")
	}

	// GPS fix should override fallback.
	cache.Update(Fix{Latitude: 47.6, Longitude: -122.3})
	fix, ok = sp.Get()
	if !ok {
		t.Fatal("expected GPS position")
	}
	if fix.Latitude != 47.6 {
		t.Fatalf("got lat %v, want 47.6", fix.Latitude)
	}
}

func TestStationPos_NoFallbackNoGPS(t *testing.T) {
	sp := NewStationPos(NewMemCache())
	_, ok := sp.Get()
	if ok {
		t.Fatal("expected no position")
	}
}

func TestStationPos_ClearFallback(t *testing.T) {
	sp := NewStationPos(NewMemCache())
	sp.SetFallback(&Fix{Latitude: 35.0, Longitude: -106.0})

	sp.SetFallback(nil)
	_, ok := sp.Get()
	if ok {
		t.Fatal("expected no position after clearing fallback")
	}
}
