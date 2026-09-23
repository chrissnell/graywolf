package configstore

import (
	"context"
	"testing"
)

// TestBeaconCreateZeroValuesSurvive is the graywolf regression for a bug
// where creating a beacon with Channel=0 (Auto) and an empty Path was
// silently rewritten to Channel=1 and Path="WIDE1-1" on save. GORM's
// Create substitutes a field's parsed `gorm:"default:..."` value
// whenever the Go value equals that type's zero value, so Channel,
// Path, SlotSeconds, and Enabled must NOT carry a `default` tag on the
// Beacon model — see the comments on those fields in models.go.
func TestBeaconCreateZeroValuesSurvive(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	b := &Beacon{
		Type:        "position",
		Channel:     0, // Auto — must not become 1
		Callsign:    "N0CALL-9",
		Path:        "", // no digipeater path — must not become "WIDE1-1"
		SlotSeconds: 0,  // top-of-hour slot — must not become -1 (unset)
		Enabled:     false,
		Latitude:    1,
		Longitude:   1,
	}
	if err := s.CreateBeacon(ctx, b); err != nil {
		t.Fatalf("CreateBeacon: %v", err)
	}

	got, err := s.GetBeacon(ctx, b.ID)
	if err != nil {
		t.Fatalf("GetBeacon: %v", err)
	}
	if got.Channel != 0 {
		t.Errorf("Channel = %d, want 0 (Auto)", got.Channel)
	}
	if got.Path != "" {
		t.Errorf("Path = %q, want empty", got.Path)
	}
	if got.SlotSeconds != 0 {
		t.Errorf("SlotSeconds = %d, want 0", got.SlotSeconds)
	}
	if got.Enabled {
		t.Errorf("Enabled = true, want false")
	}
}
