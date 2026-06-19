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

func TestBeaconRequest_SendPathDefaultsRF(t *testing.T) {
	r := BeaconRequest{Type: "position", Latitude: 1, Longitude: 1} // SendPath empty
	m := r.ToModel()
	if m.SendPath != "rf" {
		t.Fatalf("empty send_path should normalize to rf, got %q", m.SendPath)
	}
}

func TestBeaconRequest_SendPathISOnly(t *testing.T) {
	r := BeaconRequest{Type: "position", Latitude: 1, Longitude: 1, SendPath: "is_only"}
	m := r.ToModel()
	if m.SendPath != "is_only" {
		t.Fatalf("send_path = %q, want is_only", m.SendPath)
	}
}

func TestBeaconRequest_Validate_BadSendPath(t *testing.T) {
	r := BeaconRequest{Type: "custom", SendPath: "carrier-pigeon"}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for unknown send_path")
	}
}

func TestBeaconRequest_Validate_ISOnlyOK(t *testing.T) {
	r := BeaconRequest{Type: "custom", SendPath: "is_only"}
	if err := r.Validate(); err != nil {
		t.Fatalf("is_only should validate, got %v", err)
	}
}

func strPtr(s string) *string { return &s }

func TestBeaconRequest_Validate_BadCallsignSSID(t *testing.T) {
	r := BeaconRequest{Type: "position", Latitude: 1, Longitude: 1, SendPath: "is_only", Callsign: strPtr("NW5W-17")}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for callsign with SSID 17 (out of APRS range 0-15)")
	}
}

func TestBeaconRequest_Validate_GoodCallsign(t *testing.T) {
	r := BeaconRequest{Type: "position", Latitude: 1, Longitude: 1, SendPath: "is_only", Callsign: strPtr("NW5W-7")}
	if err := r.Validate(); err != nil {
		t.Fatalf("NW5W-7 should validate, got %v", err)
	}
}

func TestBeaconRequest_Validate_InheritCallsignSkipsParse(t *testing.T) {
	// nil/empty override = inherit station callsign; must not be parsed here.
	r := BeaconRequest{Type: "position", Latitude: 1, Longitude: 1, SendPath: "rf"}
	if err := r.Validate(); err != nil {
		t.Fatalf("inherited callsign should validate, got %v", err)
	}
}

func TestBeaconRequest_Validate_BadPath(t *testing.T) {
	r := BeaconRequest{Type: "position", Latitude: 1, Longitude: 1, SendPath: "rf", Path: "WIDE1-1,BADCALLSIGN-99"}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for invalid path element")
	}
}
