package aprs

// Mic-E decoder — translated from direwolf's decode_mic_e.c and the
// APRS Protocol Reference chapter 10. Mic-E is a bit-packed position
// report that smuggles latitude, message bits, N/S, longitude offset,
// and E/W into the AX.25 destination address. The info field carries
// the longitude, speed/course, and symbol.
//
// Destination callsign byte layout (6 chars):
//   byte 0..3: latitude digits, each coding one of {'0'..'9','A'..'J','K','L','P'..'Y','Z'}
//              'K','L','Z' are the "space" variants used for ambiguity
//              'A'..'J' and 'P'..'Y' also carry the message-bit high ones
//   byte 4   : N/S + longitude offset + message bit
//   byte 5   : E/W indicator
//
// Info-field layout (after the ' or ` type byte):
//   byte 0..2: longitude degrees + minutes + hundredths (offset encoded)
//   byte 3..5: speed/course (base 10 triplet)
//   byte 6   : symbol code
//   byte 7   : symbol table
//   byte 8+  : optional manufacturer byte(s), telemetry, comment, status

import (
	"errors"
	"strings"

	"github.com/chrissnell/graywolf/pkg/ax25"
)

// Mic-E message labels (code 0..7). Code 0 is the "off-duty" default.
var miceMessageLabels = [8]string{
	"Off Duty", "En Route", "In Service", "Returning",
	"Committed", "Special", "Priority", "Emergency",
}

// parseMicE is invoked when the info field starts with '\'' or '`'.
// The frame is required to pull the latitude from the destination
// address; if it's nil (e.g. ParseInfo without a frame) we bail.
func parseMicE(pkt *DecodedAPRSPacket, info []byte, frame *ax25.Frame) error {
	if frame == nil {
		return errors.New("aprs: mic-e requires frame")
	}
	// info[0] is the Mic-E type byte ('`' current, '\'' old). The
	// actual payload — longitude, speed/course, symbol — starts at
	// info[1].
	// Minimum: type byte + 3 lon + 3 spd/crs + 1 sym code + 1 sym table = 9.
	if len(info) < 9 {
		return errors.New("aprs: mic-e info too short")
	}
	body := info[1:]
	dest := frame.Dest.Call
	if len(dest) != 6 {
		return errors.New("aprs: mic-e destination not 6 chars")
	}

	var mic MicE
	lat, msgCode, nsSign, lonOffset, ewSign, err := decodeMicEDest(dest)
	if err != nil {
		return err
	}
	mic.MessageCode = msgCode
	if msgCode >= 0 && msgCode < len(miceMessageLabels) {
		mic.MessageText = miceMessageLabels[msgCode]
	}

	// Longitude bytes 0..2 of the Mic-E body (info field after the type byte).
	lon, err := decodeMicELon(body[0:3], lonOffset, ewSign)
	if err != nil {
		return err
	}
	mic.Position.Latitude = lat * nsSign
	mic.Position.Longitude = lon

	// Speed/course bytes 3..5 of the body.
	spd, crs := decodeMicESpeedCourse(body[3:6])
	if spd >= 0 {
		mic.Position.Speed = float64(spd)
	}
	// APRS101 ch 10: wire course 0 means "unknown"; 1..359 are true
	// bearings; 360 means due north. Mark unknown as HasCourse=false.
	if crs > 0 && crs <= 360 {
		mic.Position.HasCourse = true
		if crs == 360 {
			mic.Position.Course = 360
		} else {
			mic.Position.Course = crs
		}
	}

	// Symbol code (body byte 6) + table (body byte 7). Per Perl FAP
	// and the Mic-E spec, the table must be '/', '\\', A-Z, or 0-9;
	// anything else means we misaligned or the packet is corrupted.
	//
	// accept_broken_mice recovery: some iGates squash the required
	// double-space in the spd/crs field down to a single space. If
	// body[7] isn't a valid table but body[4] is a lone space and
	// body[5..6] look like a symbol code+table pair, re-insert the
	// space so bytes line up again.
	if !isMicESymTable(body[7]) && len(body) >= 8 &&
		body[4] == ' ' && isMicESymTable(body[6]) {
		fixed := make([]byte, 0, len(body)+1)
		fixed = append(fixed, body[:4]...)
		fixed = append(fixed, ' ')
		fixed = append(fixed, body[4:]...)
		body = fixed
		// Re-decode longitude with the corrected alignment.
		lon2, err := decodeMicELon(body[0:3], lonOffset, ewSign)
		if err == nil {
			mic.Position.Longitude = lon2
		}
		spd, crs := decodeMicESpeedCourse(body[3:6])
		if spd >= 0 {
			mic.Position.Speed = float64(spd)
		} else {
			mic.Position.Speed = 0
		}
		mic.Position.HasCourse = false
		if crs > 0 && crs <= 360 {
			mic.Position.HasCourse = true
			mic.Position.Course = crs
		}
	}
	if !isMicESymTable(body[7]) {
		return errors.New("aprs: mic-e invalid symbol table")
	}
	mic.Position.Symbol = Symbol{Table: body[7], Code: body[6]}

	// Remainder is optional altitude + manufacturer/comment.
	if len(body) > 8 {
		rest := body[8:]
		// APRS101 ch 10: optional 4-byte altitude "XXX}" encoding
		// metres+10000 as three base-91 digits followed by '}'. It may
		// appear either directly after the symbol table byte or after
		// a 1-byte manufacturer prefix (']', '>', '`', etc.) per the
		// spec's examples.
		tryAlt := func(off int) bool {
			if len(rest) < off+4 || rest[off+3] != '}' {
				return false
			}
			b0, b1, b2 := int(rest[off])-33, int(rest[off+1])-33, int(rest[off+2])-33
			if b0 < 0 || b0 >= 91 || b1 < 0 || b1 >= 91 || b2 < 0 || b2 >= 91 {
				return false
			}
			n := b0*91*91 + b1*91 + b2
			mic.Position.Altitude = float64(n - 10000)
			mic.Position.HasAlt = true
			// Splice the 4 alt bytes out of rest so the manufacturer
			// decoder still sees its leading prefix byte.
			rest = append(append([]byte{}, rest[:off]...), rest[off+4:]...)
			return true
		}
		if !tryAlt(0) {
			_ = tryAlt(1)
		}
		mic.Manufacturer, mic.Status = decodeMicEManufacturer(rest)
	}
	// APRS101 ch 13: Mic-E comment may embed a base-91 telemetry
	// block wrapped in '|' delimiters. Strip it before DAO scanning
	// so the inner bytes aren't mistaken for "!wXY!".
	mic.Status = stripMicEPipeTelemetry(mic.Status)
	// DAO high-precision extension may appear in the Mic-E comment.
	mic.Status = extractDAO(&mic.Position, mic.Status)

	pkt.MicE = &mic
	// Present MicE as a position packet for downstream consumers.
	pkt.Position = &Position{
		Latitude:  mic.Position.Latitude,
		Longitude: mic.Position.Longitude,
		Speed:     mic.Position.Speed,
		Course:    mic.Position.Course,
		HasCourse: mic.Position.HasCourse,
		Altitude:  mic.Position.Altitude,
		HasAlt:    mic.Position.HasAlt,
		Symbol:    mic.Position.Symbol,
	}
	pkt.Type = PacketMicE
	return nil
}

// decodeMicEDest parses the 6-character destination callsign into
// latitude (degrees, minutes/100), message bits, N/S sign, longitude
// offset, and E/W sign.
func decodeMicEDest(dest string) (lat float64, msgCode int, nsSign float64, lonOffset int, ewSign float64, err error) {
	if len(dest) != 6 {
		err = errors.New("mic-e: dest length")
		return
	}
	digits := make([]byte, 6)
	// Message bits: bytes 0, 1, 2 contribute bit2, bit1, bit0.
	var mb [3]int
	for i := 0; i < 6; i++ {
		c := dest[i]
		var d byte
		var bit int
		switch {
		case c >= '0' && c <= '9':
			d = c - '0'
			bit = 0
		case c >= 'A' && c <= 'J':
			d = c - 'A'
			bit = 1 // custom message bit
		case c == 'K':
			d = ' '
			bit = 1 // ambiguity
		case c == 'L':
			d = ' '
			bit = 0
		case c >= 'P' && c <= 'Y':
			d = c - 'P'
			bit = 1 // standard message bit
		case c == 'Z':
			d = ' '
			bit = 1
		default:
			err = errors.New("mic-e: bad dest char")
			return
		}
		digits[i] = d
		if i < 3 {
			mb[i] = bit
		}
	}
	// Latitude: DD MM.MM from digits[0..5]. Space digits mean ambiguity.
	latDeg := 0
	if digits[0] != ' ' {
		latDeg += int(digits[0]) * 10
	}
	if digits[1] != ' ' {
		latDeg += int(digits[1])
	}
	latMinWhole := 0
	if digits[2] != ' ' {
		latMinWhole += int(digits[2]) * 10
	}
	if digits[3] != ' ' {
		latMinWhole += int(digits[3])
	}
	latMinFrac := 0
	if digits[4] != ' ' {
		latMinFrac += int(digits[4]) * 10
	}
	if digits[5] != ' ' {
		latMinFrac += int(digits[5])
	}
	lat = float64(latDeg) + (float64(latMinWhole)+float64(latMinFrac)/100.0)/60.0

	// Mic-E hemisphere/offset decoding (APRS spec table):
	//   byte 3: '1' bits → North, '0' bits → South
	//   byte 4: '1' bits → +100° longitude offset, '0' → +0°
	//   byte 5: '1' bits → West, '0' → East
	// where '1' bits are produced by letters A..J, P..Y, K, L, Z and
	// '0' bits by digits.
	nsSign = -1
	if isMicEHighBit(dest[3]) {
		nsSign = 1
	}
	lonOffset = 0
	if isMicEHighBit(dest[4]) {
		lonOffset = 100
	}
	// Position uses positive east, so west → negative sign.
	ewSign = 1
	if isMicEHighBit(dest[5]) {
		ewSign = -1
	}

	// Assemble message code from the 3 message bits.
	msgCode = (mb[0] << 2) | (mb[1] << 1) | mb[2]
	return
}

// stripMicEPipeTelemetry removes an APRS base-91 telemetry block
// ("|...|", APRS101 ch 13) from the middle or tail of a Mic-E comment
// and returns the comment with any adjacent whitespace tidied up. We
// don't currently decode the telemetry values themselves.
func stripMicEPipeTelemetry(comment string) string {
	first := strings.IndexByte(comment, '|')
	if first < 0 {
		return comment
	}
	last := strings.LastIndexByte(comment, '|')
	if last <= first {
		return comment
	}
	out := comment[:first] + comment[last+1:]
	return strings.TrimSpace(out)
}

// isMicESymTable reports whether c is a valid APRS symbol table
// identifier: '/' (primary), '\\' (alternate), a letter A-Z, or a
// digit 0-9 (numeric overlay).
func isMicESymTable(c byte) bool {
	switch {
	case c == '/' || c == '\\':
		return true
	case c >= 'A' && c <= 'Z':
		return true
	case c >= '0' && c <= '9':
		return true
	}
	return false
}

// isMicEHighBit reports whether a Mic-E destination character encodes
// the "1" variant of its hemisphere / offset field. Per the APRS
// Protocol Reference: digits '0'..'9' and the letter 'L' carry the "0"
// bit; letters 'A'..'J', 'P'..'Y', 'K', and 'Z' carry the "1" bit.
func isMicEHighBit(c byte) bool {
	switch {
	case c >= '0' && c <= '9':
		return false
	case c == 'L':
		return false
	case c >= 'A' && c <= 'J':
		return true
	case c == 'K':
		return true
	case c >= 'P' && c <= 'Y':
		return true
	case c == 'Z':
		return true
	}
	return false
}

// ErrMicELonAmbiguous reports that one of the three longitude bytes is a
// SPACE (0x20), which APRS101 ch 10 reserves as the "no/ambiguous data"
// marker for the Mic-E info-field longitude field. Some encoders (Yaesu
// FT-2D/FT-3D, Kenwood TH-D72) emit this state before GPS lock; the
// receiver MUST NOT combine the SPACE byte with the destination's
// longitude offset bit and pretend the result is a position. parseMicE
// surfaces this as a warn-and-drop rather than plotting the station
// 8000+ km from its actual location.
var ErrMicELonAmbiguous = errors.New("mic-e: longitude ambiguous (SPACE in info field)")

// decodeMicELon decodes the 3-byte info-field longitude into decimal
// degrees and applies the offset / hemisphere.
func decodeMicELon(b []byte, offset int, sign float64) (float64, error) {
	if len(b) < 3 {
		return 0, errors.New("mic-e: lon short")
	}
	// APRS101 ch 10: a SPACE (0x20) in any of the three longitude
	// bytes flags that field as unknown — the convention used when GPS
	// has not locked or the encoder is otherwise unwilling to assert a
	// value. Combining it with the dest-byte-4 +100° offset would
	// otherwise yield lon = (b[0]-28)+100 = 104° from a 0x20 byte and
	// drop a German station onto Mongolia.
	if b[0] == ' ' || b[1] == ' ' || b[2] == ' ' {
		return 0, ErrMicELonAmbiguous
	}
	// Degrees byte (0x1C..0x7F after +28 offset).
	d := int(b[0]) - 28
	if d >= 180 && d <= 189 {
		d -= 80
	} else if d >= 190 && d <= 199 {
		d -= 190
	}
	if d < 0 || d > 179 {
		return 0, errors.New("mic-e: lon degrees range")
	}
	d += offset

	m := int(b[1]) - 28
	if m >= 60 {
		m -= 60
	}
	if m < 0 || m > 59 {
		return 0, errors.New("mic-e: lon minutes range")
	}

	h := int(b[2]) - 28
	if h < 0 || h > 99 {
		return 0, errors.New("mic-e: lon hundredths range")
	}
	lon := float64(d) + (float64(m)+float64(h)/100.0)/60.0
	return lon * sign, nil
}

// decodeMicESpeedCourse decodes the 3-byte speed/course field. Returns
// speed in knots and course in degrees true, or (-1,-1) if unavailable.
func decodeMicESpeedCourse(b []byte) (int, int) {
	if len(b) < 3 {
		return -1, -1
	}
	sp := int(b[0]) - 28
	dc := int(b[1]) - 28
	se := int(b[2]) - 28
	if sp < 0 || dc < 0 || se < 0 {
		return -1, -1
	}
	speed := sp*10 + dc/10
	if speed >= 800 {
		speed -= 800
	}
	course := (dc%10)*100 + se
	if course >= 400 {
		course -= 400
	}
	return speed, course
}

// decodeMicEManufacturer inspects the trailing bytes after the symbol
// table byte and matches a short list of known manufacturer prefixes
// documented in the APRS Mic-E application note. Returns the model
// string and any residual status text.
func decodeMicEManufacturer(rest []byte) (string, string) {
	s := string(rest)
	switch {
	case strings.HasPrefix(s, ">") && strings.HasSuffix(s, "="):
		return "Kenwood TH-D74", strings.TrimSuffix(strings.TrimPrefix(s, ">"), "=")
	case strings.HasPrefix(s, ">"):
		return "Kenwood TH-D72", strings.TrimPrefix(s, ">")
	case strings.HasPrefix(s, "]") && strings.HasSuffix(s, "="):
		return "Kenwood TM-D710", strings.TrimSuffix(strings.TrimPrefix(s, "]"), "=")
	case strings.HasPrefix(s, "]"):
		return "Kenwood TM-D700", strings.TrimPrefix(s, "]")
	case strings.HasPrefix(s, "`"):
		return "Yaesu/Other", strings.TrimPrefix(s, "`")
	case strings.HasPrefix(s, "'"):
		return "McTracker", strings.TrimPrefix(s, "'")
	}
	return "", s
}

// EncodeMicEDest builds the 6-character destination callsign for a
// Mic-E transmission from a latitude and the message bits / hemisphere
// selectors. Exposed for the beacon encoder and unit tests.
func EncodeMicEDest(lat float64, msgCode int, lonOffset100 bool, westLong bool) string {
	north := lat >= 0
	if lat < 0 {
		lat = -lat
	}
	deg := int(lat)
	minF := (lat - float64(deg)) * 60.0
	minWhole := int(minF)
	minFrac := int((minF - float64(minWhole)) * 100.0)
	digits := [6]int{deg / 10, deg % 10, minWhole / 10, minWhole % 10, minFrac / 10, minFrac % 10}
	// Message bits: bit2, bit1, bit0 go to bytes 0, 1, 2 respectively.
	bits := [3]bool{
		msgCode&0x4 != 0,
		msgCode&0x2 != 0,
		msgCode&0x1 != 0,
	}
	out := make([]byte, 6)
	for i := 0; i < 6; i++ {
		d := byte(digits[i])
		var c byte
		highBit := false
		switch i {
		case 0, 1, 2:
			highBit = bits[i]
		case 3:
			highBit = north
		case 4:
			highBit = lonOffset100
		case 5:
			highBit = westLong
		}
		if highBit {
			c = 'P' + d
		} else {
			c = '0' + d
		}
		out[i] = c
	}
	return string(out)
}
