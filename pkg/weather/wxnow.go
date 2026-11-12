package weather

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

const (
	maxWxNowBytes        = 64 * 1024
	maxWxNowWeatherBytes = 236
)

// ReadWxNow reads and decodes the conventional two-line WxNow.txt format.
func ReadWxNow(path string) (Observation, error) {
	f, err := os.Open(path)
	if err != nil {
		return Observation{}, fmt.Errorf("open WxNow.txt: %w", err)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxWxNowBytes+1))
	if err != nil {
		return Observation{}, fmt.Errorf("read WxNow.txt: %w", err)
	}
	if len(raw) > maxWxNowBytes {
		return Observation{}, fmt.Errorf("WxNow.txt exceeds %d bytes", maxWxNowBytes)
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	var lines []string
	for _, line := range strings.Split(strings.TrimPrefix(text, "\ufeff"), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) < 2 {
		return Observation{}, fmt.Errorf("WxNow.txt must contain a timestamp line and an APRS weather line")
	}
	if _, err := time.ParseInLocation("Jan 02 2006 15:04", strings.TrimSpace(lines[0]), time.Local); err != nil {
		return Observation{}, fmt.Errorf("invalid WxNow.txt timestamp %q: %w", lines[0], err)
	}
	line := lines[1]
	if len(line) > maxWxNowWeatherBytes {
		return Observation{}, fmt.Errorf("WxNow.txt weather line exceeds %d bytes", maxWxNowWeatherBytes)
	}
	for _, c := range []byte(line) {
		if c < 0x20 || c > 0x7e {
			return Observation{}, fmt.Errorf("WxNow.txt weather line contains a non-printable byte")
		}
	}
	decoded, err := aprs.ParseInfo([]byte("_01010000" + line))
	if err != nil || decoded.Type != aprs.PacketWeather || decoded.Weather == nil {
		return Observation{}, fmt.Errorf("WxNow.txt line 2 is not a complete APRS weather report")
	}
	return fromAPRS(decoded.Weather), nil
}

func fromAPRS(wx *aprs.Weather) Observation {
	var o Observation
	if wx.HasTemp {
		o.TemperatureC = fp((wx.Temperature - 32) * 5 / 9)
	}
	if wx.HasHumidity {
		o.HumidityPct = fp(float64(wx.Humidity))
	}
	if wx.HasPressure {
		o.PressureHPa = fp(wx.Pressure / 10)
	}
	if wx.HasWindSpeed {
		o.WindSpeedKPH = fp(wx.WindSpeed * 1.609344)
	}
	if wx.HasWindDir {
		o.WindDirDeg = ip(wx.WindDirection)
	}
	if wx.HasWindGust {
		o.GustKPH = fp(wx.WindGust * 1.609344)
	}
	if wx.HasRain1h {
		o.RainHourMM = fp(wx.Rain1Hour / 100 * mmPerInch)
	}
	if wx.HasRain24h {
		o.Rain24hMM = fp(wx.Rain24Hour / 100 * mmPerInch)
	}
	if wx.HasRainMid {
		o.RainDayMM = fp(wx.RainSinceMid / 100 * mmPerInch)
	}
	if wx.HasSnow {
		o.Snow24hMM = fp(wx.Snowfall24h * mmPerInch)
	}
	if wx.HasLuminosity {
		o.LuminosityWm2 = ip(wx.Luminosity)
	}
	if wx.HasRawRain {
		o.RawRainCounter = ip(wx.RawRainCounter)
	}
	o.SoftwareType = wx.SoftwareType
	o.WeatherUnitTag = wx.WeatherUnitTag
	return o
}

func fp(v float64) *float64 { return &v }
func ip(v int) *int         { return &v }
