package dto

import "testing"

func TestMapsConfigRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		wantErr bool
	}{
		{"osm ok", "osm", false},
		{"graywolf ok", "graywolf", false},
		{"empty rejected", "", true},
		{"unknown rejected", "google", true},
		{"case-sensitive", "OSM", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := MapsConfigRequest{Source: tt.source}.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate(%q) err=%v wantErr=%v", tt.source, err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeCallsign(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"lowercase uppercased", "nw5w", "NW5W", false},
		{"whitespace trimmed", "  NW5W  ", "NW5W", false},
		{"ssid stripped", "NW5W-9", "NW5W", false},
		{"ssid with letters stripped", "K1ABC-WIDE1", "K1ABC", false},
		{"3 chars accepted with digit", "AB1", "AB1", false},
		{"9 chars accepted with digit", "ABCDEFGH1", "ABCDEFGH1", false},
		{"2 chars rejected", "A1", "", true},
		{"10 chars rejected", "ABCDEFGHI1", "", true},
		{"no digit rejected", "ABCDEF", "", true},
		{"only digits rejected by digit rule? no, regex allows", "12345", "12345", false},
		{"empty rejected", "", "", true},
		{"punctuation rejected", "NW5W!", "", true},
		{"space inside rejected", "NW 5W", "", true},
		{"ssid leaves empty rejected", "-9", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeCallsign(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NormalizeCallsign(%q) err=%v wantErr=%v", tt.in, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("NormalizeCallsign(%q) = %q want %q", tt.in, got, tt.want)
			}
		})
	}
}
