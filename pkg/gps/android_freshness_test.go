//go:build android

package gps

import (
	pb "github.com/chrissnell/graywolf/pkg/platformproto"
	"testing"
	"time"
)

func TestAndroidPlatformFixAdvancesSharedFreshness(t *testing.T) {
	cache := NewMemCache()
	first := &pb.GpsFix{Lat: 39.8, Lon: -84.8, TimeUnixMs: 1700000000000}
	cache.Update(toFix(first))
	f, _ := cache.Get()
	if !f.ReceivedAt.IsZero() {
		t.Fatal("one cached platform location claimed live updates")
	}
	next := &pb.GpsFix{Lat: 39.8, Lon: -84.8, TimeUnixMs: first.TimeUnixMs + 1000}
	cache.Update(toFix(next))
	f, _ = cache.Get()
	if f.ReceivedAt.IsZero() || time.Since(f.ReceivedAt) > time.Second {
		t.Fatal("platform location did not refresh local position age")
	}
	at := f.ReceivedAt
	cache.Update(toFix(next))
	f, _ = cache.Get()
	if !f.ReceivedAt.Equal(at) {
		t.Fatal("replayed platform location refreshed position age")
	}
}
