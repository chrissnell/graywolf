package dto

import (
	"strings"
	"testing"
)

func TestBeaconRequest_Validate_PositionFormat(t *testing.T) {
	mkPos := func() BeaconRequest {
		return BeaconRequest{
			Type:           "position",
			UseGps:         true,
			PositionFormat: "compressed",
		}
	}

	cases := []struct {
		name    string
		mutate  func(*BeaconRequest)
		wantErr string // substring; "" means expect nil
	}{
		{"compressed_zero_amb_ok", func(r *BeaconRequest) {
			r.PositionFormat = "compressed"
			r.Ambiguity = 0
		}, ""},
		{"compressed_with_amb_rejected", func(r *BeaconRequest) {
			r.PositionFormat = "compressed"
			r.Ambiguity = 1
		}, "ambiguity must be 0 when position_format is compressed"},
		{"uncompressed_ok", func(r *BeaconRequest) {
			r.PositionFormat = "uncompressed"
			r.Ambiguity = 2
		}, ""},
		{"uncompressed_amb_too_high", func(r *BeaconRequest) {
			r.PositionFormat = "uncompressed"
			r.Ambiguity = 5
		}, "ambiguity must be 0..4"},
		{"mic_e_accepted", func(r *BeaconRequest) {
			r.PositionFormat = "mic_e"
			r.Ambiguity = 2
		}, ""},
		{"mic_e_amb_too_high", func(r *BeaconRequest) {
			r.PositionFormat = "mic_e"
			r.Ambiguity = 5
		}, "ambiguity must be 0..4"},
		{"unknown_format", func(r *BeaconRequest) {
			r.PositionFormat = "bogus"
		}, "position_format must be one of"},
		{"empty_format_defaults_compressed", func(r *BeaconRequest) {
			r.PositionFormat = ""
		}, ""},
		{"object_format_ignored", func(r *BeaconRequest) {
			r.Type = "object"
			r.PositionFormat = "mic_e"
			r.Latitude = 37
			r.Longitude = -122
			r.UseGps = false
		}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := mkPos()
			tc.mutate(&r)
			err := r.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}
