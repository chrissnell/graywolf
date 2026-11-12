package beacon

import (
	"context"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

func TestWeatherBeaconBuildAndDecode(t *testing.T) {
	s, err := New(Options{Sink: newMockSink(1)})
	if err != nil {
		t.Fatal(err)
	}
	b := Config{Type: TypeWeather, Lat: 42.96, Lon: 1.37,
		WeatherSource: "wxnow_file",
		WeatherPath:   writeWxNowTestFile(t, "Feb 01 2009 12:34\n272/010g006t069r010p030P020h61b10150\n"),
		Comment:       "must not appear", CommentCmd: []string{"must-not-run"},
		AltFt: 1000, PHGPower: 10, SymbolCode: '>'}
	info, err := s.buildInfo(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(info, "_272/010g006t069r010p030P020h61b10150") || strings.Contains(info, "PHG") || strings.Contains(info, "/A=") || strings.Contains(info, "must not appear") {
		t.Fatalf("malformed weather information: %q", info)
	}
	pkt, err := aprs.ParseInfo([]byte(info))
	if err != nil || pkt.Weather == nil || !pkt.Weather.HasTemp || pkt.Weather.Temperature != 69 {
		t.Fatalf("weather did not decode: %+v, %v", pkt, err)
	}
	// A missing source is an error rather than a beacon with stale weather.
	b.WeatherPath += ".missing"
	if _, err := s.buildInfo(context.Background(), b); err == nil {
		t.Fatal("missing source accepted")
	}
	b.UseGps = true
	b.WeatherPath = writeWxNowTestFile(t, "Feb 01 2009 12:34\n272/010g006t069\n")
	if _, err := s.buildInfo(context.Background(), b); err == nil {
		t.Fatal("missing GPS accepted")
	}
}
