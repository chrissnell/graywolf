package aprs

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Uncompressed position field is exactly 19 bytes:
//   DDMM.mmN[/|\]DDDMM.mmW[/|\]S
// Where the first symbol byte is the symbol table (/ or \) and the last
// is the symbol code. Compressed position field is 13 bytes:
//   sYYYYXXXXcsTT  where s is the symbol table and T is the comp type.
const (
	uncompressedPosLen = 19
	compressedPosLen   = 13
)

// parsePositionNoTS handles '!' and '=' prefix packets (current position,
// no timestamp, with or without messaging capability).
func parsePositionNoTS(pkt *DecodedAPRSPacket, info []byte, _ bool) error {
	pkt.Type = PacketPosition
	return parsePositionBody(pkt, info[1:])
}

// parsePositionWithTS handles '/' and '@' prefix packets (position with
// embedded 7-byte timestamp).
func parsePositionWithTS(pkt *DecodedAPRSPacket, info []byte, _ bool) error {
	pkt.Type = PacketPosition
	if len(info) < 1+7 {
		return errors.New("aprs: position too short for timestamp")
	}
	ts, err := parseAPRSTimestamp(info[1:8])
	if err != nil {
		return err
	}
	local := info[7] == '/'
	body := info[8:]
	if err := parsePositionBody(pkt, body); err != nil {
		return err
	}
	if pkt.Position != nil {
		pkt.Position.Timestamp = ts
		pkt.Position.LocalTime = local
	}
	return nil
}

// parsePositionBody parses either an uncompressed (19-byte) or a
// compressed (13-byte) position block followed by an optional extension
// (7-byte CSE/SPD or PHG) and trailing comment. Weather reports also
// pass through this path because their position prefix is identical.
func parsePositionBody(pkt *DecodedAPRSPacket, body []byte) error {
	if len(body) == 0 {
		return errors.New("aprs: empty position body")
	}
	// Compressed positions always start with a table byte (/, \, or
	// alphanumeric for overlay), followed by base-91 encoded lat/lon. The
	// table byte range disambiguates from uncompressed (which starts with
	// a digit or space).
	if body[0] != ' ' && (body[0] < '0' || body[0] > '9') && len(body) >= compressedPosLen {
		return parseCompressedPosition(pkt, body)
	}
	if len(body) < uncompressedPosLen {
		return errors.New("aprs: uncompressed position too short")
	}
	return parseUncompressedPosition(pkt, body)
}

func parseUncompressedPosition(pkt *DecodedAPRSPacket, body []byte) error {
	// Latitude: 8 chars DDMM.mmN
	latStr := string(body[0:8])
	lat, amb, err := parseLatitude(latStr)
	if err != nil {
		return err
	}
	if body[8] != '/' && body[8] != '\\' && !isAlnum(body[8]) {
		return fmt.Errorf("aprs: bad symbol table byte %q", body[8])
	}
	table := body[8]
	lonStr := string(body[9:18])
	lon, err := parseLongitude(lonStr)
	if err != nil {
		return err
	}
	code := body[18]
	pos := &Position{
		Latitude:  lat,
		Longitude: lon,
		Ambiguity: amb,
		Symbol:    Symbol{Table: table, Code: code},
	}
	pkt.Position = pos
	rest := body[19:]
	// When the symbol code is '_' (weather station), the leading
	// "ddd/sss" is wind direction/speed and must be handed to the
	// weather appendix parser — not consumed as position CSE/SPD.
	if code == '_' {
		if wx, residual := parseWeatherAppendix(string(rest)); wx != nil {
			pkt.Weather = wx
			pkt.Type = PacketWeather
			pkt.Comment = strings.TrimRight(residual, " ")
			parseDirectionFinding(pkt)
			return nil
		}
	}
	// Optional 7-byte CSE/SPD or /A=altitude in the trailing comment.
	rest = parsePositionExtension(pos, rest)
	comment := strings.TrimRight(string(rest), " ")
	comment = extractDAO(pos, comment)
	pkt.Comment = comment
	// DF appendix "/BRG/NRQ" (APRS101 ch 7) may be attached to the
	// comment; attach as pkt.DF without overriding the position type.
	parseDirectionFinding(pkt)
	return nil
}

// parseCompressedPosition implements APRS Protocol Reference section
// "Compressed Lat/Long Position Report Format". The 13-byte form is:
//
//   byte 0  : symbol table (/ or \ or overlay alphanumeric)
//   bytes 1..4: YYYY base-91 latitude
//   bytes 5..8: XXXX base-91 longitude
//   byte 9  : symbol code
//   bytes 10..11: cs (course/speed or altitude or range)
//   byte 12 : compression type byte
func parseCompressedPosition(pkt *DecodedAPRSPacket, body []byte) error {
	if len(body) < compressedPosLen {
		return errors.New("aprs: compressed position too short")
	}
	table := body[0]
	lat := 90.0 - float64(base91Decode4(body[1:5]))/380926.0
	lon := -180.0 + float64(base91Decode4(body[5:9]))/190463.0
	code := body[9]
	pos := &Position{
		Latitude:   lat,
		Longitude:  lon,
		Symbol:     Symbol{Table: table, Code: code},
		Compressed: true,
	}
	// cs decoding: if c == ' ' no cs info; else if c >= '!' and T byte
	// bit 0x18 == 0x10 => altitude; else => course/speed.
	c := body[10]
	s := body[11]
	t := body[12]
	if c != ' ' {
		if (t-33)&0x18 == 0x10 {
			// Altitude: 2 base-91 digits → feet = 1.002^((c-33)*91+(s-33))
			n := int(c-33)*91 + int(s-33)
			alt := math.Pow(1.002, float64(n))
			pos.Altitude = alt * 0.3048 // feet → meters
			pos.HasAlt = true
		} else {
			course := int(c-33) * 4
			if course == 0 {
				course = 360
			}
			speed := (math.Pow(1.08, float64(s-33)) - 1.0)
			// Preserve 360 as a distinct value (Perl FAP convention).
			pos.Course = course
			pos.HasCourse = true
			pos.Speed = speed
		}
	}
	pkt.Position = pos
	rest := body[compressedPosLen:]
	// APRS101 ch 12 / FAP: compressed positions with the '_' weather
	// symbol carry a weather appendix in the trailing bytes, identical
	// to the uncompressed path.
	if code == '_' {
		if wx, residual := parseWeatherAppendix(string(rest)); wx != nil {
			pkt.Weather = wx
			pkt.Type = PacketWeather
			pkt.Comment = strings.TrimRight(residual, " ")
			parseDirectionFinding(pkt)
			return nil
		}
	}
	rest = parsePositionExtension(pos, rest)
	comment := strings.TrimSpace(string(rest))
	comment = extractDAO(pos, comment)
	pkt.Comment = strings.TrimSpace(comment)
	parseDirectionFinding(pkt)
	return nil
}

// parsePositionExtension handles the 7-byte CSE/SPD, PHGphgd radio
// extension, and /A=altitude appendix that may follow a position
// block. Returns the remaining unparsed bytes (which become the
// comment / weather / DF appendix).
func parsePositionExtension(pos *Position, rest []byte) []byte {
	if len(rest) >= 7 && isDigit(rest[0]) && isDigit(rest[1]) && isDigit(rest[2]) && rest[3] == '/' {
		// CSE/SPD
		if cse, err := strconv.Atoi(string(rest[0:3])); err == nil {
			if spd, err := strconv.Atoi(string(rest[4:7])); err == nil {
				pos.Course = cse
				pos.HasCourse = true
				pos.Speed = float64(spd) // knots
				rest = rest[7:]
			}
		}
	}
	// PHGphgd (APRS101 ch 7). "PHG" followed by 4 digits.
	if len(rest) >= 7 && rest[0] == 'P' && rest[1] == 'H' && rest[2] == 'G' &&
		isDigit(rest[3]) && isDigit(rest[4]) && isDigit(rest[5]) && isDigit(rest[6]) {
		pos.PHG = string(rest[3:7])
		rest = rest[7:]
	}
	// /A=aaaaaa altitude in feet (any position within the comment).
	// Accepts optional leading '-' in place of the first digit.
	if idx := bytes.Index(rest, []byte("/A=")); idx >= 0 && len(rest) >= idx+9 {
		digits := rest[idx+3 : idx+9]
		if allDigitsOrSign(digits) {
			if alt, err := strconv.Atoi(string(digits)); err == nil {
				pos.Altitude = float64(alt) * 0.3048
				pos.HasAlt = true
				// Splice out the /A= block.
				out := make([]byte, 0, len(rest)-9)
				out = append(out, rest[:idx]...)
				out = append(out, rest[idx+9:]...)
				rest = out
			}
		}
	}
	// /BRG/NRQ direction-finding appendix (7 bytes "/BBB/NRQ").
	// TODO: handled in df.go via parseDirectionFinding on the comment.
	return rest
}

func parseLatitude(s string) (float64, int, error) {
	if len(s) != 8 {
		return 0, 0, errors.New("aprs: latitude length")
	}
	// Ambiguity: trailing spaces in minutes/hundredths reduce precision.
	// Per APRS101 ch 6 and the Ham::APRS::FAP reference implementation,
	// the reported position is the MIDPOINT of the ambiguous range, not
	// the low end. Positions 6,5,3,2 correspond to hundredths, tenths,
	// ones, tens of minutes respectively.
	b := []byte(s)
	amb := applyLatLonAmbiguity(b, []int{6, 5, 3, 2})
	if b[4] != '.' {
		return 0, 0, errors.New("aprs: latitude missing '.'")
	}
	ns := b[7]
	if ns != 'N' && ns != 'S' {
		return 0, 0, fmt.Errorf("aprs: latitude hemisphere %q", ns)
	}
	deg, err := strconv.Atoi(string(b[0:2]))
	if err != nil {
		return 0, 0, err
	}
	minWhole, err := strconv.Atoi(string(b[2:4]))
	if err != nil {
		return 0, 0, err
	}
	minFrac, err := strconv.Atoi(string(b[5:7]))
	if err != nil {
		return 0, 0, err
	}
	lat := float64(deg) + (float64(minWhole)+float64(minFrac)/100.0)/60.0
	if ns == 'S' {
		lat = -lat
	}
	return lat, amb, nil
}

// applyLatLonAmbiguity replaces spaces in the minute/hundredths positions
// with digits that place the reported value at the MIDPOINT of the
// ambiguous range (matching Ham::APRS::FAP and APRS101 ch 6). positions
// is ordered least-significant first: [hundredths, tenths, ones, tens].
// amb 1→hundredths get '5'; amb 2→tenths get '5'; amb 3→ones get '5';
// amb 4→tens get '3' (midpoint of 0..59 minutes). All less-significant
// obscured positions are filled with '0'. Returns the ambiguity count.
func applyLatLonAmbiguity(b []byte, positions []int) int {
	amb := 0
	for _, i := range positions {
		if b[i] == ' ' {
			b[i] = '0'
			amb++
		}
	}
	switch amb {
	case 1:
		b[positions[0]] = '5'
	case 2:
		b[positions[1]] = '5'
	case 3:
		b[positions[2]] = '5'
	case 4:
		// Tens-of-minutes midpoint of 0..59 is 30.
		b[positions[3]] = '3'
	}
	return amb
}

func parseLongitude(s string) (float64, error) {
	if len(s) != 9 {
		return 0, errors.New("aprs: longitude length")
	}
	b := []byte(s)
	applyLatLonAmbiguity(b, []int{7, 6, 4, 3})
	if b[5] != '.' {
		return 0, errors.New("aprs: longitude missing '.'")
	}
	ew := b[8]
	if ew != 'E' && ew != 'W' {
		return 0, fmt.Errorf("aprs: longitude hemisphere %q", ew)
	}
	deg, err := strconv.Atoi(string(b[0:3]))
	if err != nil {
		return 0, err
	}
	minWhole, err := strconv.Atoi(string(b[3:5]))
	if err != nil {
		return 0, err
	}
	minFrac, err := strconv.Atoi(string(b[6:8]))
	if err != nil {
		return 0, err
	}
	lon := float64(deg) + (float64(minWhole)+float64(minFrac)/100.0)/60.0
	if ew == 'W' {
		lon = -lon
	}
	return lon, nil
}

// parseAPRSTimestamp parses a 7-byte APRS timestamp. Supported forms:
//
//   DDHHMMz — day of month, hours, minutes UTC
//   DDHHMM/ — day of month, hours, minutes local
//   HHMMSSh — hours, minutes, seconds UTC ("hms" form)
func parseAPRSTimestamp(b []byte) (*time.Time, error) {
	if len(b) != 7 {
		return nil, errors.New("aprs: timestamp length")
	}
	suffix := b[6]
	now := time.Now().UTC()
	switch suffix {
	case 'z', '/':
		dd, err1 := strconv.Atoi(string(b[0:2]))
		hh, err2 := strconv.Atoi(string(b[2:4]))
		mm, err3 := strconv.Atoi(string(b[4:6]))
		if err1 != nil || err2 != nil || err3 != nil {
			return nil, errors.New("aprs: timestamp digits")
		}
		if dd < 1 || dd > 31 || hh > 23 || mm > 59 {
			return nil, errors.New("aprs: timestamp range")
		}
		year, month := now.Year(), now.Month()
		t := time.Date(year, month, dd, hh, mm, 0, 0, time.UTC)
		// If the calculated time is more than a day in the future, it's
		// probably last month.
		if t.After(now.Add(24 * time.Hour)) {
			if month == 1 {
				year--
				month = 12
			} else {
				month--
			}
			t = time.Date(year, month, dd, hh, mm, 0, 0, time.UTC)
		}
		return &t, nil
	case 'h':
		hh, err1 := strconv.Atoi(string(b[0:2]))
		mm, err2 := strconv.Atoi(string(b[2:4]))
		ss, err3 := strconv.Atoi(string(b[4:6]))
		if err1 != nil || err2 != nil || err3 != nil {
			return nil, errors.New("aprs: timestamp digits")
		}
		t := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, ss, 0, time.UTC)
		return &t, nil
	}
	return nil, fmt.Errorf("aprs: timestamp suffix %q", suffix)
}

// EncodePosition builds the 19-byte uncompressed position field for a
// '!' or '=' packet. ambiguity is 0..4; non-zero replaces trailing
// minute/hundredth digits with spaces.
func EncodePosition(p Position, messaging bool) ([]byte, error) {
	lat, err := formatLatitude(p.Latitude, p.Ambiguity)
	if err != nil {
		return nil, err
	}
	lon, err := formatLongitude(p.Longitude, p.Ambiguity)
	if err != nil {
		return nil, err
	}
	prefix := byte('!')
	if messaging {
		prefix = '='
	}
	out := make([]byte, 0, 1+uncompressedPosLen)
	out = append(out, prefix)
	out = append(out, lat...)
	out = append(out, p.Symbol.Table)
	out = append(out, lon...)
	out = append(out, p.Symbol.Code)
	return out, nil
}

func formatLatitude(lat float64, ambiguity int) ([]byte, error) {
	if lat < -90 || lat > 90 {
		return nil, errors.New("aprs: latitude out of range")
	}
	ns := byte('N')
	if lat < 0 {
		ns = 'S'
		lat = -lat
	}
	deg := int(lat)
	minF := (lat - float64(deg)) * 60.0
	minWhole := int(minF)
	minFrac := int((minF - float64(minWhole)) * 100.0)
	b := []byte(fmt.Sprintf("%02d%02d.%02d%c", deg, minWhole, minFrac, ns))
	applyAmbiguity(b, ambiguity, []int{6, 5, 3, 2})
	return b, nil
}

func formatLongitude(lon float64, ambiguity int) ([]byte, error) {
	if lon < -180 || lon > 180 {
		return nil, errors.New("aprs: longitude out of range")
	}
	ew := byte('E')
	if lon < 0 {
		ew = 'W'
		lon = -lon
	}
	deg := int(lon)
	minF := (lon - float64(deg)) * 60.0
	minWhole := int(minF)
	minFrac := int((minF - float64(minWhole)) * 100.0)
	b := []byte(fmt.Sprintf("%03d%02d.%02d%c", deg, minWhole, minFrac, ew))
	applyAmbiguity(b, ambiguity, []int{7, 6, 4, 3})
	return b, nil
}

func applyAmbiguity(b []byte, amb int, positions []int) {
	if amb < 0 {
		amb = 0
	}
	if amb > len(positions) {
		amb = len(positions)
	}
	for i := 0; i < amb; i++ {
		b[positions[i]] = ' '
	}
}

// Helpers --------------------------------------------------------------

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isAlnum(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func allDigitsOrSign(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for i, c := range b {
		if i == 0 && (c == '-' || c == '+') {
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

