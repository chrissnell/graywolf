package app

import (
	"github.com/chrissnell/graywolf/pkg/configstore"
	"testing"
	"time"
)

func TestBeaconConfigFromStorePreservesTrackerFreshness(t *testing.T) {
	cfg, err := beaconConfigFromStore(configstore.Beacon{Type: "tracker", Destination: "APGRWO", RequireFreshGPS: true, GPSMaxAgeSeconds: 45}, nil, "TEST1")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.RequireFreshGPS || cfg.GPSMaxAge != 45*time.Second {
		t.Fatalf("freshness settings lost: %+v", cfg)
	}
}
