package callsign

import "testing"

// TestAPRSPasscode covers known-good (callsign, passcode) pairs plus the
// invariants the algorithm promises: SSID stripping, case-insensitivity,
// and 10-character truncation.
//
// The "known good" values below were derived by running the canonical
// JavaScript algorithm (ported verbatim in passcode.go) against each
// input. The initial reference table in the Phase 1 brief contained
// incorrect values for W1AW and KE7XYZ; the brief explicitly instructed
// us to trust the implementation over the table when they disagree, so
// the values here are the algorithmically-correct ones. See the phase
// handoff for the derivation trace.
func TestAPRSPasscode_KnownValues(t *testing.T) {
	tests := []struct {
		callsign string
		want     int
	}{
		{"N0CALL", 13023}, // matches the widely-cited reference
		{"W1AW", 25988},   // derived from the canonical algorithm
		{"KE7XYZ", 22181}, // derived from the canonical algorithm
	}
	for _, tc := range tests {
		t.Run(tc.callsign, func(t *testing.T) {
			got := APRSPasscode(tc.callsign)
			if got != tc.want {
				t.Errorf("APRSPasscode(%q) = %d, want %d", tc.callsign, got, tc.want)
			}
		})
	}
}

func TestAPRSPasscode_SSIDStripped(t *testing.T) {
	base := APRSPasscode("KE7XYZ")
	for _, ssid := range []string{"-0", "-1", "-9", "-15", "-16"} {
		in := "KE7XYZ" + ssid
		got := APRSPasscode(in)
		if got != base {
			t.Errorf("APRSPasscode(%q) = %d, want %d (SSID must not affect passcode)", in, got, base)
		}
	}
}

func TestAPRSPasscode_CaseInsensitive(t *testing.T) {
	upper := APRSPasscode("KE7XYZ")
	lower := APRSPasscode("ke7xyz")
	mixed := APRSPasscode("Ke7Xyz")
	if upper != lower || upper != mixed {
		t.Errorf("APRSPasscode case variants differ: upper=%d lower=%d mixed=%d", upper, lower, mixed)
	}
}

func TestAPRSPasscode_TruncatesAt10Chars(t *testing.T) {
	// Anything past the 10th character must be ignored. Build a 10-char
	// prefix and confirm that appending additional characters does not
	// change the result.
	prefix := "ABCDEFGHIJ" // 10 chars
	base := APRSPasscode(prefix)
	for _, extra := range []string{"K", "KLMNOP", "ZZZZZZZZZZZZZ"} {
		long := prefix + extra
		got := APRSPasscode(long)
		if got != base {
			t.Errorf("APRSPasscode(%q)=%d, want %d (must truncate at 10 chars)", long, got, base)
		}
	}
}

func TestAPRSPasscode_EmptyInput(t *testing.T) {
	// Parse returns ok=false, but APRSPasscode still produces a value
	// (the hash of the empty string). Pin the behaviour so callers know
	// what to expect if they hand in garbage.
	got := APRSPasscode("")
	want := 0x73e2 & 0x7fff
	if got != want {
		t.Errorf("APRSPasscode(\"\") = %d, want %d", got, want)
	}
}
