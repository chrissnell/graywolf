package aprs

import "testing"

// TestDirectionConstants locks the string representation of the
// Direction enum so downstream consumers (DB rows, JSON DTOs, front-end
// Source badge) can depend on the wire values.
func TestDirectionConstants(t *testing.T) {
	cases := []struct {
		d    Direction
		want string
	}{
		{DirectionUnknown, ""},
		{DirectionRF, "rf"},
		{DirectionIS, "is"},
	}
	for _, tc := range cases {
		if string(tc.d) != tc.want {
			t.Errorf("Direction %q = %q, want %q", tc.d, string(tc.d), tc.want)
		}
	}
}

// TestDirectionZeroValue documents that the zero-value of Direction is
// DirectionUnknown, which is what Parse produces for packets whose
// provenance has not yet been set by the ingress adapter.
func TestDirectionZeroValue(t *testing.T) {
	var pkt DecodedAPRSPacket
	if pkt.Direction != DirectionUnknown {
		t.Errorf("zero-value Direction = %q, want DirectionUnknown", pkt.Direction)
	}
}
