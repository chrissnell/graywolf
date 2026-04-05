package aprs

// DAO high-precision position extension (APRS101 "!DAO!" appendix, see
// http://www.aprs.org/aprs12/datum.txt). Three forms:
//
//   !DAO!  where D = datum letter, AO = two digits (human-readable)
//          lowercase 'w' means WGS84, uppercase 'W' is also WGS84 but
//          encodes the extra precision in the human-readable minutes
//          form.
//   !wxy!  where w is lowercase (base-91 form), x/y in printable ASCII
//          33..126. Adds ~0.0055 minute (0.1852m) of extra precision.
//
// The extension appears anywhere inside the comment; the parser splices
// it out and applies the latitude/longitude correction.

import (
	"strings"
)

// extractDAO finds a DAO extension "!XYZ!" inside comment, applies the
// precision correction to pos, and returns the comment with the token
// stripped. Leaves pos and comment unchanged if no valid DAO present.
func extractDAO(pos *Position, comment string) string {
	if pos == nil || len(comment) < 5 {
		return comment
	}
	for i := 0; i+5 <= len(comment); i++ {
		if comment[i] != '!' || comment[i+4] != '!' {
			continue
		}
		d := comment[i+1]
		a := comment[i+2]
		o := comment[i+3]
		latAdd, lonAdd, ok := decodeDAO(d, a, o)
		if !ok {
			continue
		}
		if pos.Latitude >= 0 {
			pos.Latitude += latAdd
		} else {
			pos.Latitude -= latAdd
		}
		if pos.Longitude >= 0 {
			pos.Longitude += lonAdd
		} else {
			pos.Longitude -= lonAdd
		}
		pos.DAODatum = normalizeDAODatum(d)
		// Splice the 5-byte "!DAO!" out of the comment. Also swallow
		// a single adjacent space on either side so we don't leave
		// dangling whitespace.
		start, end := i, i+5
		if start > 0 && comment[start-1] == ' ' {
			start--
		} else if end < len(comment) && comment[end] == ' ' {
			end++
		}
		return strings.TrimRight(comment[:start]+comment[end:], " ")
	}
	return comment
}

// decodeDAO translates the 3 DAO bytes into latitude/longitude additive
// corrections (decimal degrees, always positive — caller applies sign
// based on hemisphere). Returns ok=false if the datum byte isn't one of
// the recognized forms.
func decodeDAO(datum, a, o byte) (float64, float64, bool) {
	switch {
	case datum >= 'A' && datum <= 'Z':
		// Human-readable: a and o are ASCII digits ('0'..'9') or space
		// for "no data". Units are hundredths of a minute.
		da, okA := daoHumanDigit(a)
		db, okB := daoHumanDigit(o)
		if !okA || !okB {
			return 0, 0, false
		}
		latMin := float64(da) / 1000.0
		lonMin := float64(db) / 1000.0
		return latMin / 60.0, lonMin / 60.0, true
	case datum >= 'a' && datum <= 'z':
		// Base-91 form: a, o are printable ASCII 33..126. Each encodes
		// 0..90 → 91-step precision. Scale = 0.01 minute per unit / 91.
		if a < '!' || a > '{' || o < '!' || o > '{' {
			return 0, 0, false
		}
		latUnits := float64(a-'!') / 91.0 // 0..1 fractional minute hundredths
		lonUnits := float64(o-'!') / 91.0
		latMin := latUnits / 100.0
		lonMin := lonUnits / 100.0
		return latMin / 60.0, lonMin / 60.0, true
	}
	return 0, 0, false
}

// daoHumanDigit accepts '0'..'9' or space (no data). Returns the digit
// value and ok=true; space yields 0 with ok=true.
func daoHumanDigit(c byte) (int, bool) {
	if c == ' ' {
		return 0, true
	}
	if c >= '0' && c <= '9' {
		return int(c - '0'), true
	}
	return 0, false
}

// normalizeDAODatum returns the upper-case datum letter used by Perl
// FAP's daodatumbyte key. Both 'w' and 'W' indicate WGS84.
func normalizeDAODatum(d byte) byte {
	if d >= 'a' && d <= 'z' {
		return d - 32
	}
	return d
}
