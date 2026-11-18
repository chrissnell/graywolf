package beacon

import (
	"context"
	"github.com/chrissnell/graywolf/pkg/gps"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type freshnessCache struct {
	fix   gps.Fix
	valid bool
}

func (c *freshnessCache) Get() (gps.Fix, bool) { return c.fix, c.valid }
func (c *freshnessCache) Update(f gps.Fix)     { c.fix = f; c.valid = true }

func TestTrackerFreshnessSendNowAllRoutes(t *testing.T) {
	now := time.Now()
	for _, route := range []string{SendPathRF, SendPathBoth, SendPathISOnly} {
		for _, format := range []string{"compressed", "uncompressed", "mic_e"} {
			t.Run(route+"/"+format, func(t *testing.T) {
				cache := &freshnessCache{fix: gps.Fix{Latitude: 39.8, Longitude: -84.8, Timestamp: now.Add(-time.Hour), ReceivedAt: now.Add(-31 * time.Second)}, valid: true}
				sink := newMockSink(0)
				is := &fakeISSink{}
				clock := newFakeClock(now)
				s, err := New(Options{Sink: sink, ISSink: is, Cache: cache, Clock: clock, Logger: slog.New(slog.NewTextHandler(logSink{}, nil))})
				if err != nil {
					t.Fatal(err)
				}
				cfg := Config{ID: 1, Type: TypeTracker, RequireFreshGPS: true, Source: mustAddr(t, "TEST1"), Dest: mustAddr(t, "APGRWO"), Format: format, SendPath: route, Slot: -1, Enabled: true}
				s.SetBeacons([]Config{cfg})
				if err := s.SendNow(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "stale") {
					t.Fatalf("stale send error: %v", err)
				}
				if sink.Len() != 0 || len(is.Lines()) != 0 {
					t.Fatal("stale tracker transmitted")
				}
				// Position is current locally even if receiver/system UTC disagree.
				cache.fix.ReceivedAt = now
				if err := s.SendNow(context.Background(), 1); err != nil {
					t.Fatal(err)
				}
				if route != SendPathISOnly && sink.Len() != 1 {
					t.Fatal("fresh RF send missing")
				}
				if route != SendPathRF && len(is.Lines()) != 1 {
					t.Fatal("fresh IS send missing")
				}
			})
		}
	}
}

func TestTrackerFreshnessScheduledSuppressionDoesNotConsumeInterval(t *testing.T) {
	now := time.Now()
	cache := &freshnessCache{fix: gps.Fix{ReceivedAt: now.Add(-time.Hour)}, valid: true}
	s, _ := New(Options{Sink: newMockSink(0), Cache: cache, Clock: newFakeClock(now), Logger: slog.New(slog.NewTextHandler(logSink{}, nil))})
	for _, smart := range []*SmartBeaconConfig{nil, {Enabled: true}} {
		p := &beaconPlan{cfg: Config{ID: 1, Type: TypeTracker, RequireFreshGPS: true, Every: time.Hour, SmartBeacon: smart}}
		if s.shouldFire(p, now) {
			t.Fatal("stale scheduled tracker fired")
		}
		if !p.lastSent.IsZero() {
			t.Fatal("suppression consumed send interval")
		}
		if wake := s.nextWake(p, now); wake.Sub(now) != smartPollInterval {
			t.Fatalf("recovery poll: %v", wake)
		}
		cache.fix.ReceivedAt = now
		if !s.shouldFire(p, now) || p.gpsBlocked {
			t.Fatal("fresh updates did not resume tracker")
		}
		cache.fix.ReceivedAt = now.Add(-time.Hour)
	}
}

func TestTrackerFreshnessDefaultAndNonTrackerUnaffected(t *testing.T) {
	now := time.Now()
	stale := gps.Fix{ReceivedAt: now.Add(-time.Hour)}
	for _, b := range []Config{{Type: TypeTracker}, {Type: TypePosition, RequireFreshGPS: true}, {Type: TypeObject, RequireFreshGPS: true}, {Type: TypeCustom, RequireFreshGPS: true}} {
		if err := trackerFreshness(b, stale, now); err != nil {
			t.Fatal(err)
		}
	}
	for _, age := range []time.Duration{0, 5 * time.Second, time.Minute} {
		b := Config{Type: TypeTracker, RequireFreshGPS: true, GPSMaxAge: age}
		limit := age
		if limit == 0 {
			limit = DefaultGPSMaxAge
		}
		if err := trackerFreshness(b, gps.Fix{ReceivedAt: now.Add(-limit)}, now); err != nil {
			t.Fatal(err)
		}
		for _, fix := range []gps.Fix{{}, {ReceivedAt: now.Add(time.Second)}, {ReceivedAt: now.Add(-limit - time.Nanosecond)}} {
			if err := trackerFreshness(b, fix, now); err == nil {
				t.Fatal("unverifiable/stale fix allowed")
			}
		}
	}
}

func TestTrackerFreshnessRunResumesWhenUpdatesAdvance(t *testing.T) {
	cache := gps.NewMemCache()
	sink := newMockSink(1)
	clock := newFakeClock(time.Now())
	s, err := New(Options{Sink: sink, Cache: cache, Clock: clock, Logger: slog.New(slog.NewTextHandler(logSink{}, nil))})
	if err != nil {
		t.Fatal(err)
	}
	s.SetBeacons([]Config{{ID: 1, Type: TypeTracker, RequireFreshGPS: true, Every: time.Hour, Slot: -1, Enabled: true, Source: mustAddr(t, "TEST1"), Dest: mustAddr(t, "APGRWO")}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	clock.waitForWaiters(t, 1, time.Second)
	if sink.Len() != 0 {
		t.Fatal("empty GPS cache transmitted")
	}
	epoch := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	cache.Update(gps.Fix{Latitude: 39.8, Longitude: -84.8, Timestamp: epoch})
	clock.Advance(smartPollInterval)
	clock.waitForWaiters(t, 1, time.Second)
	if sink.Len() != 0 {
		t.Fatal("single cached report transmitted")
	}
	cache.Update(gps.Fix{Latitude: 39.8, Longitude: -84.8, Timestamp: epoch.Add(time.Second)})
	clock.Advance(smartPollInterval)
	select {
	case <-sink.done:
	case <-time.After(time.Second):
		t.Fatal("fresh updates did not resume scheduled send")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop")
	}
}
