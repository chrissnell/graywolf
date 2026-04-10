package aprs

import "testing"

func TestParseWeatherPositionless(t *testing.T) {
	// _10090556c220s004g005t077r000p000P000h50b09900
	info := []byte("_10090556c220s004g005t077r000p000P000h50b09900wRSW")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Type != PacketWeather || pkt.Weather == nil {
		t.Fatalf("type %q", pkt.Type)
	}
	if !pkt.Weather.HasTemp || pkt.Weather.Temperature != 77 {
		t.Errorf("temp %+v", pkt.Weather)
	}
	if !pkt.Weather.HasHumidity || pkt.Weather.Humidity != 50 {
		t.Errorf("humidity %+v", pkt.Weather)
	}
	if !pkt.Weather.HasPressure || pkt.Weather.Pressure != 9900 {
		t.Errorf("pressure %+v", pkt.Weather)
	}
}

func TestParseWeatherNegativeTempPositionless(t *testing.T) {
	// Sub-zero temperature must not be dropped.
	info := []byte("_10090556c220s004g005t-14h50b09900")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Weather == nil {
		t.Fatal("no weather")
	}
	if !pkt.Weather.HasTemp || pkt.Weather.Temperature != -14 {
		t.Errorf("temp %+v", pkt.Weather)
	}
}

func TestParseWeatherNegativeTempInPositionComment(t *testing.T) {
	info := []byte("!4903.50N/07201.75W_220/004g005t-14h50b09900")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Weather == nil {
		t.Fatal("no weather")
	}
	if !pkt.Weather.HasTemp || pkt.Weather.Temperature != -14 {
		t.Errorf("temp %+v", pkt.Weather)
	}
}

func TestParseWeatherSnowAfterGust(t *testing.T) {
	// 's' before 'g' is wind speed; 's' after 'g' is 24h snowfall
	// (hundredths of an inch, per direwolf decode_aprs.c).
	info := []byte("_10090556c220s004g010s012t077h50b09900")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Weather == nil {
		t.Fatal("no weather")
	}
	if !pkt.Weather.HasWindSpeed || pkt.Weather.WindSpeed != 4 {
		t.Errorf("wind speed %+v", pkt.Weather)
	}
	if !pkt.Weather.HasSnow || pkt.Weather.Snowfall24h != 0.12 {
		t.Errorf("snow %+v", pkt.Weather)
	}
}

func TestParseWeatherRawRain(t *testing.T) {
	info := []byte("_10090556c220s004g005t077#0123h50b09900")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Weather == nil || !pkt.Weather.HasRawRain || pkt.Weather.RawRainCounter != 123 {
		t.Errorf("raw rain %+v", pkt.Weather)
	}
}

func TestParseWeatherSoftwareTag(t *testing.T) {
	info := []byte("_10090556c220s004g005t077h50b09900xOWRD")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Weather == nil {
		t.Fatal("no weather")
	}
	if pkt.Weather.SoftwareType != "x" || pkt.Weather.WeatherUnitTag != "OWRD" {
		t.Errorf("sw %q unit %q", pkt.Weather.SoftwareType, pkt.Weather.WeatherUnitTag)
	}
}

func TestIsDigitsOrSpaceDotsDashOnlyFirst(t *testing.T) {
	if isDigitsOrSpaceDots("1-2") {
		t.Errorf("'1-2' should be rejected (dash not at position 0)")
	}
	if !isDigitsOrSpaceDots("-14") {
		t.Errorf("'-14' should be accepted")
	}
}

func TestParseWeatherTimestampRange(t *testing.T) {
	if _, err := parseWeatherTimestamp([]byte("99999999")); err == nil {
		t.Errorf("expected range error for 99999999")
	}
}

func TestParseWeatherInPositionComment(t *testing.T) {
	// Position report with weather symbol and weather appendix
	info := []byte("!4903.50N/07201.75W_220/004g005t077r000p000P000h50b09900")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Weather == nil {
		t.Fatal("expected weather from position+_ symbol")
	}
	if !pkt.Weather.HasWindDir || pkt.Weather.WindDirection != 220 {
		t.Errorf("wind dir %+v", pkt.Weather)
	}
}
