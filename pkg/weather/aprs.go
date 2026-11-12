package weather

import (
	"fmt"
	"math"
	"strings"
)

const mmPerInch = 25.4

// EncodeAPRS converts an observation to an APRS101 complete-weather appendix.
// The returned string begins with the optional ddd/sss wind group and is meant
// to follow the '_' symbol in a positioned weather report.
func EncodeAPRS(o Observation) (string, error) {
	var b strings.Builder
	reported := false

	if o.WindDirDeg != nil || o.WindSpeedKPH != nil {
		dir := "..."
		if o.WindDirDeg != nil {
			d := *o.WindDirDeg
			if d < 0 || d > 360 {
				return "", fmt.Errorf("weather: wind direction %d outside 0..360", d)
			}
			if d == 0 {
				d = 360
			}
			dir = fmt.Sprintf("%03d", d)
		}
		speed := "..."
		if o.WindSpeedKPH != nil {
			v, err := field3(kphToMPH(*o.WindSpeedKPH), "wind speed")
			if err != nil {
				return "", err
			}
			speed = v
		}
		fmt.Fprintf(&b, "%s/%s", dir, speed)
		reported = true
	}
	if o.GustKPH != nil {
		v, err := field3(kphToMPH(*o.GustKPH), "wind gust")
		if err != nil {
			return "", err
		}
		b.WriteByte('g')
		b.WriteString(v)
		reported = true
	}
	if o.TemperatureC != nil {
		f := int(math.Round(*o.TemperatureC*9/5 + 32))
		if f < -99 || f > 999 {
			return "", fmt.Errorf("weather: temperature %dF outside APRS range", f)
		}
		if f < 0 {
			fmt.Fprintf(&b, "t-%02d", -f)
		} else {
			fmt.Fprintf(&b, "t%03d", f)
		}
		reported = true
	}
	for _, rain := range []struct {
		key  byte
		mm   *float64
		name string
	}{
		{'r', o.RainHourMM, "one-hour rain"},
		{'p', o.Rain24hMM, "24-hour rain"},
		{'P', o.RainDayMM, "rain since midnight"},
	} {
		if rain.mm == nil {
			continue
		}
		v, err := field3(*rain.mm/mmPerInch*100, rain.name)
		if err != nil {
			return "", err
		}
		b.WriteByte(rain.key)
		b.WriteString(v)
		reported = true
	}
	if o.RawRainCounter != nil {
		if *o.RawRainCounter < 0 || *o.RawRainCounter > 9999 {
			return "", fmt.Errorf("weather: raw rain counter outside 0..9999")
		}
		fmt.Fprintf(&b, "#%04d", *o.RawRainCounter)
		reported = true
	}
	if o.HumidityPct != nil {
		h := int(math.Round(*o.HumidityPct))
		if h < 0 || h > 100 {
			return "", fmt.Errorf("weather: humidity %d outside 0..100", h)
		}
		if h == 100 {
			h = 0
		}
		fmt.Fprintf(&b, "h%02d", h)
		reported = true
	}
	if o.PressureHPa != nil {
		p := int(math.Round(*o.PressureHPa * 10))
		if p < 0 || p > 99999 {
			return "", fmt.Errorf("weather: pressure outside APRS range")
		}
		fmt.Fprintf(&b, "b%05d", p)
		reported = true
	}
	if o.LuminosityWm2 != nil {
		l := *o.LuminosityWm2
		if l < 0 || l > 1999 {
			return "", fmt.Errorf("weather: luminosity outside 0..1999")
		}
		if l >= 1000 {
			fmt.Fprintf(&b, "l%03d", l-1000)
		} else {
			fmt.Fprintf(&b, "L%03d", l)
		}
		reported = true
	}
	if o.Snow24hMM != nil {
		if o.GustKPH == nil {
			b.WriteString("g...") // makes the following 's' unambiguously snowfall
		}
		v, err := field3(*o.Snow24hMM/mmPerInch*100, "24-hour snowfall")
		if err != nil {
			return "", err
		}
		b.WriteByte('s') // after gust: APRS snowfall, not wind speed
		b.WriteString(v)
		reported = true
	}
	if o.SoftwareType != "" || o.WeatherUnitTag != "" {
		if len(o.SoftwareType) != 1 || len(o.WeatherUnitTag) < 2 || len(o.WeatherUnitTag) > 4 {
			return "", fmt.Errorf("weather: invalid software/unit tag")
		}
		b.WriteString(o.SoftwareType)
		b.WriteString(o.WeatherUnitTag)
	}
	if !reported {
		return "", fmt.Errorf("weather: observation contains no APRS weather fields")
	}
	return b.String(), nil
}

func field3(value float64, name string) (string, error) {
	v := int(math.Round(value))
	if v < 0 || v > 999 {
		return "", fmt.Errorf("weather: %s outside APRS range", name)
	}
	return fmt.Sprintf("%03d", v), nil
}

func kphToMPH(kph float64) float64 { return kph / 1.609344 }
