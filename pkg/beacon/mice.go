package beacon

import (
	"math"
	"strings"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

// MicEMessageOffDuty is the Mic-E message code emitted for every
// graywolf-originated Mic-E beacon. APRS101 ch 10 table 8 numbers the
// 3-bit codes by their decimal bit value: ABC = 111 (decimal 7) is
// M0 = Off Duty (the innocuous "nothing special" code), ABC = 000
// (decimal 0) is M7 = Emergency. We want Off Duty by default --
// operator-selectable codes are deferred to a future plan.
const MicEMessageOffDuty = 7

// MicEDestination returns the 6-character AX.25 destination callsign
// for a Mic-E transmission. Ambiguity blanks trailing latitude digits
// per APRS101 ch 10 table 9 (K/L/Z variants); it is applied identically
// to the uncompressed path so a Mic-E beacon and an uncompressed beacon
// at the same ambiguity level publish the same effective precision.
//
// The callsign is built by aprs.EncodeMicEDest; this wrapper hides the
// per-beacon "did the longitude need the +100 offset?" computation
// from the scheduler so it doesn't have to duplicate the lat/lon
// preprocessing the info-field encoder already does.
func MicEDestination(lat, lon float64, ambiguity int) string {
	_, offset100, west := micELonFields(lon)
	return aprs.EncodeMicEDest(lat, MicEMessageOffDuty, offset100, west, ambiguity)
}

// MicEPositionInfo builds an APRS101 ch 10 Mic-E info field for the
// given fix. The caller is responsible for swapping the AX.25
// destination with MicEDestination at frame-build time; the bytes
// returned here cover only the info portion (data-type indicator
// through to the comment).
//
// course is degrees (1..360, 0 means "no course"); speedKt is knots;
// altM is metres (emitted as a 4-byte "XYZ}" base-91 extension when
// non-zero per APRS101 ch 10). ambiguity (0..4) blanks the trailing
// longitude minutes / hundredths bytes with ASCII space; the
// destination's latitude blanking is performed by MicEDestination.
//
// Mic-E preempts PHG: there is no PHG slot in the wire format. The
// builder drops PHG silently before invoking this encoder.
func MicEPositionInfo(lat, lon float64, course int, speedKt float64, altM float64, symbolTable, symbolCode byte, messaging bool, ambiguity int, comment string) string {
	if symbolTable == 0 {
		symbolTable = '/'
	}
	if symbolCode == 0 {
		symbolCode = '-'
	}
	// Data-type indicator: backtick = messaging-capable / current GPS;
	// apostrophe = non-messaging / old style. APRS101 ch 10.
	dti := byte('\'')
	if messaging {
		dti = '`'
	}

	lonBytes := micELonBytes(lon, ambiguity)
	csBytes := micESpeedCourse(speedKt, course)

	var sb strings.Builder
	sb.WriteByte(dti)
	sb.Write(lonBytes[:])
	sb.Write(csBytes[:])
	sb.WriteByte(symbolCode)
	sb.WriteByte(symbolTable)
	if altM != 0 {
		sb.WriteString(micEAltitudeExt(altM))
	}
	if comment != "" {
		sb.WriteString(comment)
	}
	return sb.String()
}

// micELonFields returns the longitude degree value (with the +100
// adjustment applied when the source longitude falls in the
// +/-100..180 band), whether that adjustment was needed, and whether
// the longitude is west. Mirrors the parser-side logic in
// pkg/aprs/mice.go so a round-trip is byte-clean.
func micELonFields(lon float64) (degAdjusted int, offset100 bool, west bool) {
	west = lon < 0
	absLon := lon
	if absLon < 0 {
		absLon = -absLon
	}
	d := int(absLon)
	if d >= 100 {
		return d - 100, true, west
	}
	return d, false, west
}

// micELonBytes returns the 3-byte longitude info-field bytes per
// APRS101 ch 10. Ambiguity in Mic-E is signaled only by the
// destination callsign's K/L/Z space variants; the info-field
// longitude bytes always carry value bytes (none of them are ASCII
// space) and the receiver discards the trailing minute digits at
// decode time based on the level it read from the destination.
//
// We truncate the longitude minutes / hundredths here so that the
// position we put on the wire matches the precision the receiver will
// recompute -- there's no point sending full-precision GPS bytes that
// the receiver is going to throw away, and truncating ensures the
// destination's K/L/Z signal lines up with the emitted longitude.
//
// Precision per ambiguity level:
//
//	0: full (1/100 minute)
//	1: 1/10 minute
//	2: 1 minute
//	3: 10 minutes
//	4: 1 degree
func micELonBytes(lon float64, ambiguity int) [3]byte {
	degAdjusted, _, _ := micELonFields(lon)

	absLon := lon
	if absLon < 0 {
		absLon = -absLon
	}
	// Recover the minutes portion from the fractional degrees.
	frac := absLon - math.Trunc(absLon)
	minF := frac * 60.0
	minWhole := int(minF)
	minFrac := int(math.Round((minF - float64(minWhole)) * 100.0))
	// Float carry guard: rounding may push minFrac to 100.
	if minFrac >= 100 {
		minFrac = 0
		minWhole++
	}
	if minWhole >= 60 {
		minWhole = 0 // wrap; the degree byte already covers this case
	}

	// Coarsen by truncation to match the destination's K/L/Z blanking.
	// Truncation (rather than round-to-nearest) keeps the encoded value
	// at or below the operator's true position so the wire matches the
	// "digit blanked" semantics the receiver sees in the destination.
	switch {
	case ambiguity >= 4:
		minWhole = 0
		minFrac = 0
	case ambiguity >= 3:
		minWhole = (minWhole / 10) * 10
		minFrac = 0
	case ambiguity >= 2:
		minFrac = 0
	case ambiguity >= 1:
		minFrac = (minFrac / 10) * 10
	}

	// Degrees byte: just value + 28. For 0-9 the resulting raw byte is
	// in 28-37 (control chars) but the APRS101 parser accepts them; we
	// do not use the +80 alternate range because the in-tree decoder
	// has no branch for that variant. For values >= 100 the caller has
	// already subtracted 100 (offset100 is set in the destination).
	degByte := byte(degAdjusted + 28)

	// Minutes byte: value + 28; if value < 10, also +60 so the byte
	// stays printable and outside the control range (APRS101 ch 10
	// minutes encoding).
	m := minWhole
	if m < 10 {
		m += 60
	}
	minByte := byte(m + 28)
	hundByte := byte(minFrac + 28)

	return [3]byte{degByte, minByte, hundByte}
}

// micESpeedCourse encodes speed (knots) and course (degrees) into the
// three-byte triplet per APRS101 ch 10:
//
//	chr1 = SP/10 + 28
//	chr2 = (SP%10)*10 + DC/100 + 28
//	chr3 = DC%100 + 28
//
// where SP is speed in knots (0..799) and DC is course in degrees
// (1..360). When the caller passes course=0 ("unknown"), the encoded
// course is 0 too (parser flags HasCourse=false).
func micESpeedCourse(speedKt float64, course int) [3]byte {
	sp := int(math.Round(speedKt))
	if sp < 0 {
		sp = 0
	}
	if sp > 799 {
		sp = 799
	}
	if course < 0 {
		course = 0
	}
	if course > 360 {
		course = course % 360
	}
	chr1 := byte(sp/10) + 28
	chr2 := byte((sp%10)*10+course/100) + 28
	chr3 := byte(course%100) + 28
	return [3]byte{chr1, chr2, chr3}
}

// micEAltitudeExt returns the 4-byte altitude extension "XYZ}" where
// XYZ are 3 base-91 digits (each +33 offset) encoding metres + 10000
// (APRS101 ch 10). The trailing "}" is the marker the parser side
// scans for to detect the extension.
func micEAltitudeExt(altM float64) string {
	v := int(math.Round(altM)) + 10000
	if v < 0 {
		v = 0
	}
	if v > 91*91*91-1 {
		v = 91*91*91 - 1
	}
	out := make([]byte, 4)
	out[0] = byte(v/(91*91)) + 33
	out[1] = byte((v/91)%91) + 33
	out[2] = byte(v%91) + 33
	out[3] = '}'
	return string(out)
}
