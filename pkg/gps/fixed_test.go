package gps

import (
	"context"
	"testing"
	"time"
)

func TestValidateCoordinates(t *testing.T) {
	cases := []struct {
		name    string
		lat     float64
		lon     float64
		wantErr bool
	}{
		{"origin", 0, 0, false},
		{"home", 40.7128, -74.0060, false},
		{"north pole", 90, 180, false},
		{"south pole", -90, -180, false},
		{"lat too high", 90.001, 0, true},
		{"lat too low", -90.001, 0, true},
		{"lon too high", 0, 180.001, true},
		{"lon too low", 0, -180.001, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateCoordinates(c.lat, c.lon)
			if (err != nil) != c.wantErr {
				t.Fatalf("ValidateCoordinates(%g,%g) err=%v, wantErr=%v", c.lat, c.lon, err, c.wantErr)
			}
		})
	}
}

// TestRunFixedPublishesImmediately verifies the configured coordinate is
// pushed into the cache as soon as RunFixed starts, before any refresh
// tick, and that altitude is carried through as a 3D fix.
func TestRunFixedPublishesImmediately(t *testing.T) {
	cache := NewMemCache()
	cfg := FixedConfig{Latitude: 39.7392, Longitude: -104.9903, Altitude: 1609}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- RunFixed(ctx, cfg, cache, nil) }()

	fix := waitForFix(t, cache)
	if fix.Latitude != cfg.Latitude || fix.Longitude != cfg.Longitude {
		t.Fatalf("cache fix = (%g,%g), want (%g,%g)", fix.Latitude, fix.Longitude, cfg.Latitude, cfg.Longitude)
	}
	if !fix.HasAlt || fix.Altitude != cfg.Altitude {
		t.Fatalf("altitude = %g (hasAlt=%v), want %g/true", fix.Altitude, fix.HasAlt, cfg.Altitude)
	}
	if fix.FixMode != 3 {
		t.Fatalf("fix mode = %d, want 3", fix.FixMode)
	}
	if fix.Timestamp.IsZero() {
		t.Fatal("fix timestamp is zero; want stamped")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunFixed returned %v, want nil on cancel", err)
		}
	case <-time.After(time.Second):
		t.Fatal("RunFixed did not return after cancel")
	}
}

// TestRunFixedNoAltitude verifies that a zero altitude is treated as
// "unspecified" so a 0m altitude is never mixed into beacons.
func TestRunFixedNoAltitude(t *testing.T) {
	cache := NewMemCache()
	cfg := FixedConfig{Latitude: 51.5074, Longitude: -0.1278}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = RunFixed(ctx, cfg, cache, nil) }()

	fix := waitForFix(t, cache)
	if fix.HasAlt {
		t.Fatalf("HasAlt = true for zero altitude; want false")
	}
}

// TestRunFixedRejectsBadCoordinate verifies RunFixed fails fast on an
// invalid coordinate rather than publishing garbage.
func TestRunFixedRejectsBadCoordinate(t *testing.T) {
	cache := NewMemCache()
	cfg := FixedConfig{Latitude: 200, Longitude: 0}

	err := RunFixed(context.Background(), cfg, cache, nil)
	if err == nil {
		t.Fatal("RunFixed accepted an out-of-range latitude")
	}
	if _, ok := cache.Get(); ok {
		t.Fatal("RunFixed published a fix despite an invalid coordinate")
	}
}

// waitForFix polls the cache briefly for the first published fix.
func waitForFix(t *testing.T, cache *MemCache) Fix {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if fix, ok := cache.Get(); ok {
			return fix
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("no fix published within deadline")
	return Fix{}
}
