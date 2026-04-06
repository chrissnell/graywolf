package beacon

import (
	"fmt"
	"math"
	"strings"
)

// APRS101 position encoding — uncompressed format only. Compressed
// encoding is a TODO(phase-4) if demand arises; most trackers accept
// uncompressed.

// PositionInfo builds an uncompressed APRS position info-field.
//
//	!DDMM.hhN/DDDMM.hhW>comment     — no timestamp, no messaging
//	=DDMM.hhN/DDDMM.hhW>comment     — no timestamp, messaging capable
//
// symbolTable/symbolCode default to '/' and '-' if zero. course is
// degrees (1..360, 0 means "not set"); speed is knots; altitude is
// metres and is appended as "/A=NNNNNN" (in feet per APRS101) when
// non-zero.
func PositionInfo(lat, lon float64, course int, speedKt float64, altM float64, symbolTable, symbolCode byte, messaging bool, comment string) string {
	if symbolTable == 0 {
		symbolTable = '/'
	}
	if symbolCode == 0 {
		symbolCode = '-'
	}
	latS := encodeLat(lat)
	lonS := encodeLon(lon)
	prefix := byte('!')
	if messaging {
		prefix = '='
	}
	var sb strings.Builder
	sb.WriteByte(prefix)
	sb.WriteString(latS)
	sb.WriteByte(symbolTable)
	sb.WriteString(lonS)
	sb.WriteByte(symbolCode)
	if course > 0 || speedKt > 0 {
		// CSE/SPD extension: "CCC/SSS" (course/speed) — 7 chars.
		c := course
		if c <= 0 {
			c = 0
		}
		if c > 360 {
			c = c % 360
		}
		fmt.Fprintf(&sb, "%03d/%03d", c, int(math.Round(speedKt)))
	}
	if altM != 0 {
		ft := altM * 3.28084
		fmt.Fprintf(&sb, "/A=%06d", int(math.Round(ft)))
	}
	if comment != "" {
		sb.WriteString(comment)
	}
	return sb.String()
}

// ObjectInfo builds an APRS object report info-field.
//
//	;NAME     *DDHHMMzDDMM.hhN/DDDMM.hhW>comment
//
// objectName is padded/truncated to 9 characters. live=true sets '*'
// (live) rather than '_' (killed). timestampDHM is a 6-char "DDHHMMz"
// string; if empty, "111111z" is used (APRS wildcard).
func ObjectInfo(objectName string, live bool, timestampDHM string, lat, lon float64, symbolTable, symbolCode byte, comment string) string {
	if symbolTable == 0 {
		symbolTable = '/'
	}
	if symbolCode == 0 {
		symbolCode = '-'
	}
	name := objectName
	if len(name) > 9 {
		name = name[:9]
	}
	for len(name) < 9 {
		name += " "
	}
	alive := byte('*')
	if !live {
		alive = '_'
	}
	ts := timestampDHM
	if ts == "" {
		ts = "111111z"
	}
	var sb strings.Builder
	sb.WriteByte(';')
	sb.WriteString(name)
	sb.WriteByte(alive)
	sb.WriteString(ts)
	sb.WriteString(encodeLat(lat))
	sb.WriteByte(symbolTable)
	sb.WriteString(encodeLon(lon))
	sb.WriteByte(symbolCode)
	sb.WriteString(comment)
	return sb.String()
}

// StatusInfo builds an APRS status report: ">comment".
func StatusInfo(comment string) string { return ">" + comment }

// encodeLat converts a signed decimal latitude to the 8-char APRS form
// "DDMM.hhH" (H = N/S).
func encodeLat(lat float64) string {
	h := byte('N')
	if lat < 0 {
		h = 'S'
		lat = -lat
	}
	deg := int(lat)
	min := (lat - float64(deg)) * 60.0
	return fmt.Sprintf("%02d%05.2f%c", deg, min, h)
}

// encodeLon converts a signed decimal longitude to the 9-char APRS form
// "DDDMM.hhH" (H = E/W).
func encodeLon(lon float64) string {
	h := byte('E')
	if lon < 0 {
		h = 'W'
		lon = -lon
	}
	deg := int(lon)
	min := (lon - float64(deg)) * 60.0
	return fmt.Sprintf("%03d%05.2f%c", deg, min, h)
}
