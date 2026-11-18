package dto

import (
	"github.com/chrissnell/graywolf/pkg/configstore"
	"testing"
)

func TestBeaconFreshnessDefaultsAndUpdatePreservation(t *testing.T) {
	r := BeaconRequest{Type: "tracker"}
	m := r.ToModel()
	if m.RequireFreshGPS || m.GPSMaxAgeSeconds != 0 {
		t.Fatal("legacy default changed")
	}
	existing := configstore.Beacon{RequireFreshGPS: true, GPSMaxAgeSeconds: 45}
	m = r.ApplyToUpdate(1, existing)
	if !m.RequireFreshGPS || m.GPSMaxAgeSeconds != 45 {
		t.Fatal("old client cleared opt-in settings")
	}
	enabled := false
	age := uint32(60)
	r.RequireFreshGPS = &enabled
	r.GPSMaxAgeSeconds = &age
	m = r.ApplyToUpdate(1, existing)
	if m.RequireFreshGPS || m.GPSMaxAgeSeconds != 60 {
		t.Fatal("explicit settings not applied")
	}
	response := BeaconFromModel(m)
	if response.RequireFreshGPS || response.GPSMaxAgeSeconds != 60 {
		t.Fatal("response lost settings")
	}
	for _, n := range []uint32{0, 1, 30, 3600, 3601} {
		r.GPSMaxAgeSeconds = &n
		err := r.Validate()
		if (err != nil) != (n > 3600) {
			t.Fatalf("age %d: %v", n, err)
		}
	}
}
