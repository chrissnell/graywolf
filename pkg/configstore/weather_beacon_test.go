package configstore

import (
	"context"
	"testing"
)

func TestWeatherBeaconSettingsPersist(t *testing.T) {
	ctx := context.Background()
	s, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, source := range []string{"wxnow_file", "davis_serial", "peet_serial"} {
		b := Beacon{Type: "weather", Callsign: "N0CAL-13", Latitude: 42.96, Longitude: 1.37,
			WeatherSource: source, WeatherPath: "/weather/WxNow.txt", WeatherDevice: "/dev/ttyUSB0",
			WeatherBaud: 2400, WeatherBucket: "0.01in", Symbol: "_", SymbolTable: "/"}
		if err := s.CreateBeacon(ctx, &b); err != nil {
			t.Fatal(err)
		}
		got, err := s.GetBeacon(ctx, b.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Type != b.Type || got.WeatherSource != b.WeatherSource || got.WeatherPath != b.WeatherPath || got.WeatherDevice != b.WeatherDevice || got.WeatherBaud != b.WeatherBaud || got.WeatherBucket != b.WeatherBucket {
			t.Fatalf("weather settings changed during persistence: %+v", got)
		}
	}
}
