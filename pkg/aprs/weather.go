package aprs

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// parseWeatherPositionless handles the '_' prefix (positionless weather
// report). Layout:
//
//   _MMDDHHMM c...s...g...t...r...p...P...h..b.....<swT>
//
// Where each field is a fixed width and may be filled with '.' or space
// to indicate "no data".
func parseWeatherPositionless(pkt *DecodedAPRSPacket, info []byte) error {
	if len(info) < 1+8 {
		return errors.New("aprs: weather: short")
	}
	tsBytes := info[1:9]
	ts, err := parseWeatherTimestamp(tsBytes)
	if err == nil && ts != nil {
		pkt.Timestamp = *ts
	}
	wx, residual := parseWeatherAppendix(string(info[9:]))
	if wx == nil {
		return nil // nothing to record; fall through
	}
	pkt.Weather = wx
	pkt.Type = PacketWeather
	pkt.Comment = residual
	return nil
}

// parseWeatherTimestamp parses the 8-byte MMDDHHMM timestamp used by
// positionless weather reports.
func parseWeatherTimestamp(b []byte) (*time.Time, error) {
	if len(b) != 8 {
		return nil, errors.New("aprs: weather timestamp len")
	}
	month, err1 := strconv.Atoi(string(b[0:2]))
	day, err2 := strconv.Atoi(string(b[2:4]))
	hour, err3 := strconv.Atoi(string(b[4:6]))
	min, err4 := strconv.Atoi(string(b[6:8]))
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return nil, errors.New("aprs: weather timestamp digits")
	}
	// Sanity-check ranges to avoid time.Date normalizing garbage into
	// plausible-but-wrong dates far in the future.
	if month < 1 || month > 12 || day < 1 || day > 31 || hour > 23 || min > 59 {
		return nil, errors.New("aprs: weather timestamp range")
	}
	now := time.Now().UTC()
	t := time.Date(now.Year(), time.Month(month), day, hour, min, 0, 0, time.UTC)
	return &t, nil
}

// parseWeatherAppendix parses the weather fields that appear either in a
// positionless weather report or in the comment field of a '_' symbol
// position report. Returns the decoded Weather and the residual comment
// with the weather fields stripped.
//
// Each field has a fixed width. Width-3 numerics: c s g t r p P L l.
// Width-2: h (humidity, "00" means 100%). Width-5: b (barometric
// pressure in tenths of millibars). A '.' or space inside the digit
// span means "no data"; we tolerate those without bailing out.
//
// Any trailing bytes that don't match a known key-with-digits form are
// returned as residual (typically the software-type/unit identifier
// like "wRSW" at the end of a real weather packet).
func parseWeatherAppendix(text string) (*Weather, string) {
	wx := &Weather{}
	matched := false
	i := 0
	var leftover strings.Builder
	sawGust := false // APRS101 ch 12: 's' after 'g' is snowfall, otherwise wind speed.

	// Leading "ddd/sss" wind form (direction / speed in mph). Only
	// consumed if the full 7-char shape is present; otherwise fall
	// through and let the key-driven loop pick up 'c'/'s'. The digit
	// spans also accept '.' or space as "no data" placeholders (APRS101
	// ch 12 "no data reported").
	if len(text) >= 7 && isWxNumOrNone3(text[0:3]) && text[3] == '/' && isWxNumOrNone3(text[4:7]) {
		if d, ok := parseWxNum(text[0:3]); ok {
			wx.WindDirection = d
			wx.HasWindDir = true
		}
		if s, ok := parseWxNum(text[4:7]); ok {
			wx.WindSpeed = float64(s)
			wx.HasWindSpeed = true
		}
		i = 7
		matched = true
	}

	// Per-key widths (APRS101 ch 12 Table 12-1).
	width := func(key byte) int {
		switch key {
		case 'c', 's', 'g', 't', 'r', 'p', 'P', 'L', 'l':
			return 3
		case '#': // raw rain counter
			return 4
		case 'h':
			return 2
		case 'b':
			return 5
		}
		return 0
	}

	for i < len(text) {
		key := text[i]
		w := width(key)
		if w == 0 {
			// Unknown key byte — residual starts here (software+unit
			// identifier or free-form comment). Stop weather parsing.
			leftover.WriteString(text[i:])
			break
		}
		if i+1+w > len(text) {
			leftover.WriteString(text[i:])
			break
		}
		digits := text[i+1 : i+1+w]
		if !isDigitsOrSpaceDots(digits) {
			// Key byte present but the following span isn't a valid
			// numeric run — treat as residual.
			leftover.WriteString(text[i:])
			break
		}
		v, ok := parseWxNum(digits)
		switch key {
		case 'c':
			if ok {
				wx.WindDirection = v
				wx.HasWindDir = true
				matched = true
			}
		case 's':
			// Direwolf decode_aprs.c: 's' is wind speed until a gust
			// 'g' has been seen, after which it becomes 24h snowfall
			// (hundredths of an inch).
			if ok {
				if sawGust {
					wx.Snowfall24h = float64(v) / 100.0
					wx.HasSnow = true
				} else {
					wx.WindSpeed = float64(v)
					wx.HasWindSpeed = true
				}
				matched = true
			}
		case 'g':
			if ok {
				wx.WindGust = float64(v)
				wx.HasWindGust = true
				matched = true
			}
			sawGust = true
		case 't':
			if ok {
				wx.Temperature = float64(v)
				wx.HasTemp = true
				matched = true
			}
		case 'r':
			if ok {
				wx.Rain1Hour = float64(v)
				wx.HasRain1h = true
				matched = true
			}
		case 'p':
			if ok {
				wx.Rain24Hour = float64(v)
				wx.HasRain24h = true
				matched = true
			}
		case 'P':
			if ok {
				wx.RainSinceMid = float64(v)
				wx.HasRainMid = true
				matched = true
			}
		case '#':
			if ok {
				wx.RawRainCounter = v
				wx.HasRawRain = true
				matched = true
			}
		case 'h':
			if ok {
				h := v
				if h == 0 {
					h = 100
				}
				wx.Humidity = h
				wx.HasHumidity = true
				matched = true
			}
		case 'b':
			if ok {
				wx.Pressure = float64(v)
				wx.HasPressure = true
				matched = true
			}
		case 'L', 'l':
			if ok {
				lum := v
				if key == 'l' {
					lum += 1000
				}
				wx.Luminosity = lum
				wx.HasLuminosity = true
				matched = true
			}
		}
		i += 1 + w
	}
	residual := leftover.String()
	// Software type / unit tag: the residual typically begins with a
	// single ASCII letter (software code: 'w' = wxnow, 'x' = xastir,
	// 'd' = APRSdos, ...) followed by 2..4 uppercase letters identifying
	// the unit/model (e.g. 'wRSW', 'xOWRD', 'dU2k'). Peel these so the
	// remainder is true free-form comment.
	residual = extractWeatherSoftwareTag(wx, residual)
	if !matched {
		return nil, text
	}
	return wx, residual
}

// extractWeatherSoftwareTag pulls the leading "<letter><2..4 upper>"
// software+unit identifier off a weather residual, populating
// wx.SoftwareType and wx.WeatherUnitTag. Returns the remaining residual.
func extractWeatherSoftwareTag(wx *Weather, residual string) string {
	if len(residual) < 1 {
		return residual
	}
	c0 := residual[0]
	if !((c0 >= 'a' && c0 <= 'z') || (c0 >= 'A' && c0 <= 'Z')) {
		return residual
	}
	// Count consecutive uppercase letters after the type byte.
	n := 0
	for n < 4 && 1+n < len(residual) {
		c := residual[1+n]
		if c < 'A' || c > 'Z' {
			break
		}
		n++
	}
	if n < 2 {
		return residual
	}
	wx.SoftwareType = string(c0)
	wx.WeatherUnitTag = residual[1 : 1+n]
	return residual[1+n:]
}

// isWxNumOrNone3 reports whether s is a 3-byte span that could be a
// weather numeric ("123") or a "no data" placeholder ("..." / "   ").
// Mixed numerics/placeholder chars are accepted because some stations
// emit spaces within a field (e.g. "g  1").
func isWxNumOrNone3(s string) bool {
	if len(s) != 3 {
		return false
	}
	for i := 0; i < 3; i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || c == '.' || c == ' ') {
			return false
		}
	}
	return true
}

func isDigit3(s string) bool {
	if len(s) != 3 {
		return false
	}
	for i := 0; i < 3; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isDigitsOrSpaceDots(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if i == 0 && c == '-' {
			continue
		}
		if !((c >= '0' && c <= '9') || c == '.' || c == ' ') {
			return false
		}
	}
	return true
}

func parseWxNum(s string) (int, bool) {
	cleaned := strings.ReplaceAll(strings.ReplaceAll(s, " ", ""), ".", "")
	if cleaned == "" || cleaned == "-" {
		return 0, false
	}
	n, err := strconv.Atoi(cleaned)
	if err != nil {
		return 0, false
	}
	return n, true
}
