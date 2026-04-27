package aprs

// Peet Bros Ultimeter weather formats. Two packet flavors, both carry
// big-endian 16-bit signed hex fields in fixed positions:
//
//   $ULTW…   — realtime Ultimeter-II packet (APRS TM-1 stream)
//   !!…       — Ultimeter logging (data-logger) frame
//
// Reference: perl-aprs-fap _wx_parse_peet_packet / _wx_parse_peet_logging.
// Values "----" denote undefined fields. Fields we don't care about
// (date, time, barometer correction) are still consumed to preserve
// positional alignment.

import (
	"errors"
	"math"
	"strconv"
)

// parseUltwDollar handles the "$ULTW..." realtime packet.
func parseUltwDollar(pkt *DecodedAPRSPacket, info []byte) error {
	vals, err := peetFields(string(info[5:]))
	if err != nil {
		return err
	}
	var wx Weather
	// 1: wind gust peak (0.1 kph)
	if v, ok := peetGet(vals, 0); ok {
		wx.WindGust = kphTenthsToMph(v)
		wx.HasWindGust = true
	}
	// 2: wind direction of peak gust (low byte, 0..255 → 0..360°)
	if v, ok := peetGet(vals, 1); ok {
		wx.WindDirection = peetDir(v)
		wx.HasWindDir = true
	}
	// 3: current outdoor temperature (0.1 °F, signed)
	if v, ok := peetGet(vals, 2); ok {
		wx.Temperature = float64(v) / 10.0
		wx.HasTemp = true
	}
	// 4: "rain since last clear" — Perl fills rain_midnight from this
	// and may overwrite from field 12 below.
	if v, ok := peetGet(vals, 3); ok {
		wx.RainSinceMid = float64(v) // 0.01 inch units match graywolf storage
		wx.HasRainMid = true
	}
	// 5: barometer (0.1 mbar). Graywolf stores tenths of millibar
	// already so the raw value is used directly. Perl rejects values
	// below 10 (sentinel for "not present").
	if v, ok := peetGet(vals, 4); ok && v >= 10 {
		wx.Pressure = float64(v)
		wx.HasPressure = true
	}
	// 6..8: barometer delta + LSW/MSW correction — discarded.
	// 9: outdoor humidity (0.1 %)
	if v, ok := peetGet(vals, 8); ok {
		h := int(math.Round(float64(v) / 10.0))
		if h >= 1 && h <= 100 {
			wx.Humidity = h
			wx.HasHumidity = true
		}
	}
	// 10,11: date, time — discarded.
	// 12: today's rain total (overrides field 4 when present).
	if v, ok := peetGet(vals, 11); ok {
		wx.RainSinceMid = float64(v)
		wx.HasRainMid = true
	}
	// 13: 1-minute average wind speed (0.1 kph)
	if v, ok := peetGet(vals, 12); ok {
		wx.WindSpeed = kphTenthsToMph(v)
		wx.HasWindSpeed = true
	}
	pkt.Weather = &wx
	pkt.Type = PacketWeather
	return nil
}

// parseUltwBang handles the "!!" Ultimeter logging frame.
func parseUltwBang(pkt *DecodedAPRSPacket, info []byte) error {
	vals, err := peetFields(string(info[2:]))
	if err != nil {
		return err
	}
	var wx Weather
	// 1: instantaneous wind speed (0.1 kph)
	if v, ok := peetGet(vals, 0); ok {
		wx.WindSpeed = kphTenthsToMph(v)
		wx.HasWindSpeed = true
	}
	if v, ok := peetGet(vals, 1); ok {
		wx.WindDirection = peetDir(v)
		wx.HasWindDir = true
	}
	if v, ok := peetGet(vals, 2); ok {
		wx.Temperature = float64(v) / 10.0
		wx.HasTemp = true
	}
	if v, ok := peetGet(vals, 3); ok {
		wx.RainSinceMid = float64(v)
		wx.HasRainMid = true
	}
	if v, ok := peetGet(vals, 4); ok && v >= 10 {
		wx.Pressure = float64(v)
		wx.HasPressure = true
	}
	// 6: indoor temp, 7: outdoor humidity, 8: indoor humidity
	if v, ok := peetGet(vals, 6); ok {
		h := int(math.Round(float64(v) / 10.0))
		if h >= 1 && h <= 100 {
			wx.Humidity = h
			wx.HasHumidity = true
		}
	}
	// 9,10: date, time skipped.
	// 11: today's rain.
	if v, ok := peetGet(vals, 10); ok {
		wx.RainSinceMid = float64(v)
		wx.HasRainMid = true
	}
	// 12: avg wind speed — Perl drops.
	pkt.Weather = &wx
	pkt.Type = PacketWeather
	return nil
}

// peetFields splits the Peet Bros hex stream into signed 16-bit values.
// The sentinel "----" yields a missing slot (ok=false). Stops at the
// first non-field character.
func peetFields(s string) ([]peetVal, error) {
	var out []peetVal
	for len(s) >= 4 {
		chunk := s[:4]
		if chunk == "----" {
			out = append(out, peetVal{ok: false})
			s = s[4:]
			continue
		}
		if !isHex4(chunk) {
			break
		}
		u, err := strconv.ParseUint(chunk, 16, 16)
		if err != nil {
			return nil, err
		}
		v := int16(u)
		out = append(out, peetVal{val: int(v), ok: true})
		s = s[4:]
	}
	if len(out) == 0 {
		return nil, errors.New("aprs: peet: no fields")
	}
	return out, nil
}

type peetVal struct {
	val int
	ok  bool
}

func peetGet(vals []peetVal, i int) (int, bool) {
	if i >= len(vals) {
		return 0, false
	}
	return vals[i].val, vals[i].ok
}

// peetDir converts the low byte of the Peet direction field to degrees
// (0..360). Perl: (val & 0xff) * 1.41176, rounded.
func peetDir(v int) int {
	low := v & 0xff
	return int(math.Round(float64(low) * 1.41176))
}

// kphTenthsToMph converts Peet Bros "tenths of km/h" to mph.
func kphTenthsToMph(tenths int) float64 {
	return float64(tenths) * 0.1 * 0.621371
}

func isHex4(s string) bool {
	if len(s) != 4 {
		return false
	}
	for i := 0; i < 4; i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
