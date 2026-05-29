package blocklist

import "testing"

func TestValidatePattern(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantCan string // empty when wantErr is true
		wantErr bool
	}{
		// Valid forms — canonicalized to uppercase, trimmed.
		{"bare call", "N1ROG", "N1ROG", false},
		{"lowercase canonicalized", "n1rog", "N1ROG", false},
		{"surrounding whitespace trimmed", "  KK6XYZ-9  ", "KK6XYZ-9", false},
		{"call-0 preserved distinct from bare", "N1ROG-0", "N1ROG-0", false},
		{"call-15 boundary", "N1ROG-15", "N1ROG-15", false},
		{"wildcard form", "N1ROG-*", "N1ROG-*", false},
		{"wildcard mixed case", "n1rog-*", "N1ROG-*", false},
		{"6-char callsign", "WB6ABC", "WB6ABC", false},
		{"6-char with ssid", "WB6ABC-9", "WB6ABC-9", false},

		// Invalid forms — flooding / nonsense guards.
		{"empty", "", "", true},
		{"whitespace only", "   ", "", true},
		{"lone star", "*", "", true},
		{"lone dash", "-", "", true},
		{"lone dash-star", "-*", "", true},
		{"missing call before dash", "-9", "", true},
		{"missing call before wildcard", "-*", "", true},
		{"call too long", "TOOLONGCALL", "", true},
		{"7-char call", "ABCDEFG", "", true},
		{"wildcard inside call", "N1*ROG", "", true},
		{"wildcard inside call with ssid", "N1*-9", "", true},
		{"ssid > 15", "N1ROG-16", "", true},
		{"negative ssid", "N1ROG--1", "", true},
		{"non-numeric ssid", "N1ROG-X", "", true},
		{"ssid with trailing junk", "N1ROG-9X", "", true},
		{"invalid char in call", "N1R!G", "", true},
		{"space inside call", "N1 ROG", "", true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidatePattern(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ValidatePattern(%q) = %q, nil; want error", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidatePattern(%q) error: %v", tc.input, err)
			}
			if got != tc.wantCan {
				t.Fatalf("ValidatePattern(%q) = %q, want %q", tc.input, got, tc.wantCan)
			}
		})
	}
}
