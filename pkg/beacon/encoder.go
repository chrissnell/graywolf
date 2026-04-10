package beacon

import (
	"fmt"
	"math"
	"strings"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

// APRS101 position encoding. Both the 19-byte uncompressed form and the
// 13-byte base-91 compressed form are supported. Compressed is the
// default for new beacons (see pkg/configstore): it's shorter on the air
// and has far finer position resolution (~5 cm lat vs ~18 m for the
// uncompressed "DDMM.hh" format).

// PositionInfo builds an uncompressed APRS position info-field.
//
//	!DDMM.hhN/DDDMM.hhW>comment     — no timestamp, no messaging
//	=DDMM.hhN/DDDMM.hhW>comment     — no timestamp, messaging capable
//
// symbolTable/symbolCode default to '/' and '-' if zero. course is
// degrees (1..360, 0 means "not set"); speed is knots; altitude is
// metres and is appended as "/A=NNNNNN" (in feet per APRS101) when
// non-zero.
//
// phg is the already-encoded "PHGphgd" 7-byte string (or "" for no
// PHG extension). PHG occupies the same slot as CSE/SPD and is
// meaningless for moving stations, so it is only emitted when both
// course and speed are zero.
func PositionInfo(lat, lon float64, course int, speedKt float64, altM float64, symbolTable, symbolCode byte, messaging bool, phg string, comment string) string {
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
	} else if phg != "" {
		// PHGphgd radio-capability extension — 7 chars, fixed-station only.
		sb.WriteString(phg)
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

// CompressedPositionInfo builds a 13-byte base-91 compressed APRS
// position info-field per APRS101 ch 9:
//
//	!<sym_table>YYYYXXXX<sym_code><cs><T>[/A=NNNNNN][comment]
//
// cs holds course/speed when either is set, otherwise two spaces
// ("no data"). Altitude is emitted via the "/A=" extension rather
// than the cs-altitude form so the uncompressed and compressed paths
// produce equivalent altitude precision and the caller can always
// supply both course/speed and altitude.
//
// The compression type byte T advertises: current GPS fix, NMEA
// source "other", origin "software" — matching the value most
// APRS software trackers emit.
func CompressedPositionInfo(lat, lon float64, course int, speedKt float64, altM float64, symbolTable, symbolCode byte, messaging bool, comment string) string {
	if symbolTable == 0 {
		symbolTable = '/'
	}
	if symbolCode == 0 {
		symbolCode = '-'
	}
	yx := aprs.EncodeCompressedLatLon(lat, lon)

	prefix := byte('!')
	if messaging {
		prefix = '='
	}

	// cs field: course/speed if either is known, otherwise "  ".
	var cByte, sByte byte
	if course > 0 || speedKt > 0 {
		cc := course
		if cc < 0 {
			cc = 0
		}
		if cc > 360 {
			cc = 360
		}
		// c = 33 + round(course/4). course=360 → 33+90 = 123 = '{'.
		cVal := int(math.Round(float64(cc) / 4.0))
		if cVal < 0 {
			cVal = 0
		}
		if cVal > 90 {
			cVal = 90
		}
		cByte = byte(33 + cVal)
		// s = 33 + round(log(speed+1)/log(1.08)). Clamp to 0..89
		// (s=122='z' caps at ~1062 kt, well beyond any APRS use).
		sVal := 0
		if speedKt > 0 {
			sVal = int(math.Round(math.Log(speedKt+1) / math.Log(1.08)))
		}
		if sVal < 0 {
			sVal = 0
		}
		if sVal > 89 {
			sVal = 89
		}
		sByte = byte(33 + sVal)
	} else {
		cByte = ' '
		sByte = ' '
	}
	// T byte: bit5 = current fix, bits 3-4 = NMEA source "other" (00),
	// bits 0-2 = origin "software" (010). Raw value 0b00100010 = 0x22;
	// transmitted as 0x22 + 33 = 67 = 'C'.
	tByte := byte(33 + 0x22)

	var sb strings.Builder
	sb.WriteByte(prefix)
	sb.WriteByte(symbolTable)
	sb.Write(yx)
	sb.WriteByte(symbolCode)
	sb.WriteByte(cByte)
	sb.WriteByte(sByte)
	sb.WriteByte(tByte)
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
