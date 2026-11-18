package gps

import (
	"testing"
	"time"
)

func TestReceiptTracksAdvancingEpochWithoutRequiringSynchronizedUTC(t *testing.T) {
	c := NewMemCache()
	epoch := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	c.Update(Fix{Latitude: 39, Longitude: -84, Timestamp: epoch})
	first, _ := c.Get()
	if !first.ReceivedAt.IsZero() {
		t.Fatal("single possibly cached sample claimed live updates")
	}
	c.Update(Fix{Latitude: 39, Longitude: -84, Timestamp: epoch})
	duplicate, _ := c.Get()
	if !duplicate.ReceivedAt.Equal(first.ReceivedAt) {
		t.Fatal("same GPS epoch refreshed freshness")
	}
	c.Update(Fix{Latitude: 39, Longitude: -84, Timestamp: epoch.Add(time.Second)})
	next, _ := c.Get()
	if next.ReceivedAt.IsZero() || time.Since(next.ReceivedAt) > time.Second {
		t.Fatal("advancing update lost receipt time")
	}
	if !next.Timestamp.Equal(epoch.Add(time.Second)) {
		t.Fatal("GNSS timestamp overwritten")
	}
}
