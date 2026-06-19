package configstore

import (
	"context"
	"testing"
)

// TestBeaconSendPathRoundTrip proves send_path survives Create, Get,
// Update (GORM Save), and List unchanged — the data path SendNow depends
// on. If this regresses, an is_only beacon would reach the scheduler as
// "rf" and transmit over the air.
func TestBeaconSendPathRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	b := &Beacon{
		Type:      "position",
		Channel:   0,
		Callsign:  "N0CALL-9",
		SendPath:  "is_only",
		Latitude:  1,
		Longitude: 1,
	}
	if err := s.CreateBeacon(ctx, b); err != nil {
		t.Fatalf("CreateBeacon: %v", err)
	}

	got, err := s.GetBeacon(ctx, b.ID)
	if err != nil {
		t.Fatalf("GetBeacon: %v", err)
	}
	if got.SendPath != "is_only" {
		t.Fatalf("after create: send_path=%q, want is_only", got.SendPath)
	}

	got.SendPath = "both"
	if err := s.UpdateBeacon(ctx, got); err != nil {
		t.Fatalf("UpdateBeacon: %v", err)
	}
	again, err := s.GetBeacon(ctx, b.ID)
	if err != nil {
		t.Fatalf("GetBeacon after update: %v", err)
	}
	if again.SendPath != "both" {
		t.Fatalf("after update: send_path=%q, want both", again.SendPath)
	}

	list, err := s.ListBeacons(ctx)
	if err != nil {
		t.Fatalf("ListBeacons: %v", err)
	}
	var found *Beacon
	for i := range list {
		if list[i].ID == b.ID {
			found = &list[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("beacon %d not in list", b.ID)
	}
	if found.SendPath != "both" {
		t.Fatalf("list: send_path=%q, want both", found.SendPath)
	}
}
