package aprs

import (
	"errors"
	"fmt"
	"math"
)

// PHG is the decoded APRS "PHGphgd" radio-capability extension (APRS101
// chapter 7). It is carried in the position extension slot of fixed-
// station position, object, and item reports and advertises the
// transmitter's power, antenna height above average terrain, antenna
// gain, and antenna directivity so other stations can estimate coverage.
//
// On the wire the extension is exactly seven ASCII bytes: the literal
// "PHG" followed by four digits "phgd". The digits are not the raw
// values; they are coarsely quantised exponents (see the PHG* helpers
// below for the exact formulas).
type PHG struct {
	Raw         string // four-digit "phgd" body (e.g. "7700"); never includes the "PHG" prefix
	PowerWatts  int    // p² watts
	HeightFt    int    // 10·2^h feet above average terrain
	GainDB      int    // g dB (0..9)
	Directivity int    // 0=omni, 1..8 = 45°·d compass direction (N, NE, E, …)
}

// String returns the on-air encoding of the PHG extension including the
// "PHG" prefix (seven bytes). If the receiver is nil it returns "".
func (p *PHG) String() string {
	if p == nil {
		return ""
	}
	if len(p.Raw) == 4 {
		return "PHG" + p.Raw
	}
	// Reconstruct from decoded values if Raw was lost (e.g. manual
	// construction in tests).
	s, err := EncodePHG(p.PowerWatts, p.HeightFt, p.GainDB, p.Directivity)
	if err != nil {
		return ""
	}
	return s
}

// ParsePHG decodes a four-character "phgd" body (no "PHG" prefix) into
// a *PHG. The input must be exactly four ASCII digits.
func ParsePHG(body string) (*PHG, error) {
	if len(body) != 4 {
		return nil, errors.New("aprs: PHG body must be 4 bytes")
	}
	for i := 0; i < 4; i++ {
		if body[i] < '0' || body[i] > '9' {
			return nil, fmt.Errorf("aprs: PHG non-digit at %d: %q", i, body[i])
		}
	}
	p := int(body[0] - '0')
	h := int(body[1] - '0')
	g := int(body[2] - '0')
	d := int(body[3] - '0')
	return &PHG{
		Raw:         body,
		PowerWatts:  p * p,          // APRS101: watts = p²
		HeightFt:    10 << uint(h),  // APRS101: feet = 10 · 2^h
		GainDB:      g,              // APRS101: dB literal
		Directivity: d,              // 0 omni, 1..8 = 45°·d
	}, nil
}

// EncodePHG builds the on-air "PHGphgd" string (7 bytes) from decoded
// watts / feet HAAT / dB gain / directivity values. Input ranges:
//
//	watts        0..8281   (encoded as round(√watts), clamped 0..9)
//	heightFt     10..5120  (encoded as round(log2(h/10)), clamped 0..9)
//	gainDB       0..9      (clamped)
//	directivity  0..8      (0 = omni, 1..8 = 45° × d)
//
// Values outside the representable range are clamped with no error;
// the only error returned is for directivity > 8 or < 0 (structurally
// invalid, not merely coarse).
func EncodePHG(watts, heightFt, gainDB, directivity int) (string, error) {
	if directivity < 0 || directivity > 8 {
		return "", fmt.Errorf("aprs: PHG directivity %d out of range 0..8", directivity)
	}

	// Power: p = round(√watts), clamped 0..9.
	p := 0
	if watts > 0 {
		p = int(math.Round(math.Sqrt(float64(watts))))
	}
	if p < 0 {
		p = 0
	}
	if p > 9 {
		p = 9
	}

	// Height: h = round(log2(heightFt/10)), clamped 0..9. heightFt < 10
	// encodes as 0 (10 ft); heightFt <= 0 is also 0.
	h := 0
	if heightFt > 10 {
		h = int(math.Round(math.Log2(float64(heightFt) / 10.0)))
	}
	if h < 0 {
		h = 0
	}
	if h > 9 {
		h = 9
	}

	// Gain: clamp to 0..9.
	g := gainDB
	if g < 0 {
		g = 0
	}
	if g > 9 {
		g = 9
	}

	return fmt.Sprintf("PHG%d%d%d%d", p, h, g, directivity), nil
}
