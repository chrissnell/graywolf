package dto

import (
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// A fixed-source request must round-trip its coordinate through ToModel
// and mark the config enabled.
func TestGPSRequestToModel_Fixed(t *testing.T) {
	req := GPSRequest{SourceType: "fixed", FixedLat: 39.7392, FixedLon: -104.9903, FixedAlt: 1609}
	m := req.ToModel()
	if m.SourceType != "fixed" {
		t.Fatalf("SourceType = %q, want fixed", m.SourceType)
	}
	if m.FixedLat != req.FixedLat || m.FixedLon != req.FixedLon || m.FixedAlt != req.FixedAlt {
		t.Fatalf("coordinate = (%g,%g,%g), want (%g,%g,%g)", m.FixedLat, m.FixedLon, m.FixedAlt, req.FixedLat, req.FixedLon, req.FixedAlt)
	}
	if !m.Enabled {
		t.Fatal("fixed source should be enabled")
	}
}

func TestGPSFromModel_Fixed(t *testing.T) {
	resp := GPSFromModel(configstore.GPSConfig{SourceType: "fixed", FixedLat: 1.5, FixedLon: 2.5, FixedAlt: 30, Enabled: true})
	if resp.FixedLat != 1.5 || resp.FixedLon != 2.5 || resp.FixedAlt != 30 {
		t.Fatalf("coordinate round-trip = (%g,%g,%g), want (1.5,2.5,30)", resp.FixedLat, resp.FixedLon, resp.FixedAlt)
	}
}

func TestGPSRequestValidate(t *testing.T) {
	cases := []struct {
		name    string
		req     GPSRequest
		wantErr bool
	}{
		{"fixed valid", GPSRequest{SourceType: "fixed", FixedLat: 45, FixedLon: -93}, false},
		{"fixed bad lat", GPSRequest{SourceType: "fixed", FixedLat: 91, FixedLon: 0}, true},
		{"fixed bad lon", GPSRequest{SourceType: "fixed", FixedLat: 0, FixedLon: 181}, true},
		{"serial ignores coord", GPSRequest{SourceType: "serial", FixedLat: 999, FixedLon: 999}, false},
		{"gpsd ignores coord", GPSRequest{SourceType: "gpsd", FixedLat: 999, FixedLon: 999}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.req.Validate(); (err != nil) != c.wantErr {
				t.Fatalf("Validate() err=%v, wantErr=%v", err, c.wantErr)
			}
		})
	}
}
