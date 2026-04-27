package callsign

import (
	"errors"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantBase string
		wantSSID string
		wantOK   bool
	}{
		// valid without SSID
		{"bare call", "KE7XYZ", "KE7XYZ", "", true},
		{"bare N0CALL", "N0CALL", "N0CALL", "", true},
		{"bare lowercase", "w1aw", "w1aw", "", true},

		// valid with SSID
		{"ssid 0", "KE7XYZ-0", "KE7XYZ", "0", true},
		{"ssid 9", "KE7XYZ-9", "KE7XYZ", "9", true},
		{"ssid 15", "KE7XYZ-15", "KE7XYZ", "15", true},
		{"N0CALL ssid", "N0CALL-7", "N0CALL", "7", true},
		{"lowercase with ssid", "w1aw-1", "w1aw", "1", true},

		// trimming
		{"leading+trailing whitespace", "  KE7XYZ-9  ", "KE7XYZ", "9", true},
		{"tabs", "\tKE7XYZ\t", "KE7XYZ", "", true},

		// invalid
		{"empty", "", "", "", false},
		{"whitespace only", "   ", "", "", false},
		{"leading dash", "-9", "", "", false},
		{"trailing dash", "KE7XYZ-", "", "", false},
		{"dash only", "-", "", "", false},
		{"multiple dashes", "K-E-7", "", "", false},
		{"three dashes", "CALL-A-B", "", "", false},
		{"internal space", "KE7 XYZ", "", "", false},
		{"internal tab", "KE7\tXYZ", "", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base, ssid, ok := Parse(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("Parse(%q) ok = %v, want %v (base=%q ssid=%q)", tc.in, ok, tc.wantOK, base, ssid)
			}
			if base != tc.wantBase {
				t.Errorf("Parse(%q) base = %q, want %q", tc.in, base, tc.wantBase)
			}
			if ssid != tc.wantSSID {
				t.Errorf("Parse(%q) ssid = %q, want %q", tc.in, ssid, tc.wantSSID)
			}
		})
	}
}

func TestIsN0Call(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		// true cases
		{"uppercase", "N0CALL", true},
		{"lowercase", "n0call", true},
		{"mixed case", "N0Call", true},
		{"with ssid", "N0CALL-7", true},
		{"with zero ssid", "N0CALL-0", true},
		{"padded whitespace", "  n0call  ", true},
		{"lowercase with ssid", "n0call-7", true},

		// false cases
		{"empty", "", false},
		{"different call", "KE7XYZ", false},
		{"superstring", "N0CALLX", false},
		{"malformed trailing dash", "N0CALL-", false},
		{"prefix only", "N0", false},
		{"whitespace only", "   ", false},
		{"multiple dashes", "N0CALL-1-2", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsN0Call(tc.in)
			if got != tc.want {
				t.Errorf("IsN0Call(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name     string
		override string
		station  string
		want     string
		wantErr  error // nil or one of the sentinel errors
	}{
		{"override wins", "K1ABC", "W1AW", "K1ABC", nil},
		{"inherits station", "", "W1AW", "W1AW", nil},
		{"uppercases override", " k1abc ", "", "K1ABC", nil},
		{"uppercases station", "", " w1aw ", "W1AW", nil},
		{"override preserves ssid", "k1abc-9", "", "K1ABC-9", nil},

		{"empty both", "", "", "", ErrCallsignEmpty},
		{"whitespace both", "   ", "\t", "", ErrCallsignEmpty},

		{"station N0CALL", "", "N0CALL", "", ErrCallsignN0Call},
		{"station N0CALL lowercase ssid", "", "n0call-7", "", ErrCallsignN0Call},
		{"override N0CALL beats station", "N0CALL", "W1AW", "", ErrCallsignN0Call},
		{"override N0CALL with ssid", "n0call-0", "W1AW", "", ErrCallsignN0Call},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Resolve(tc.override, tc.station)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Resolve(%q, %q) err = %v, want %v", tc.override, tc.station, err, tc.wantErr)
				}
				if got != "" {
					t.Errorf("Resolve(%q, %q) returned value %q on error; want empty", tc.override, tc.station, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve(%q, %q) unexpected err: %v", tc.override, tc.station, err)
			}
			if got != tc.want {
				t.Errorf("Resolve(%q, %q) = %q, want %q", tc.override, tc.station, got, tc.want)
			}
		})
	}
}
