package weather

import "testing"

func TestEncodeAPRSObservation(t *testing.T) {
	o := Observation{
		TemperatureC: fp(20.5), HumidityPct: fp(61), PressureHPa: fp(1015),
		WindDirDeg: ip(272), WindSpeedKPH: fp(16.09344), GustKPH: fp(9.656064),
		RainHourMM: fp(2.54), Rain24hMM: fp(7.62), RainDayMM: fp(5.08),
	}
	got, err := EncodeAPRS(o)
	if err != nil {
		t.Fatal(err)
	}
	if want := "272/010g006t069r010p030P020h61b10150"; got != want {
		t.Fatalf("EncodeAPRS() = %q, want %q", got, want)
	}
}

func TestEncodeAPRSNorthAndNegativeTemperature(t *testing.T) {
	o := Observation{WindDirDeg: ip(0), WindSpeedKPH: fp(0), TemperatureC: fp(-25.5)}
	got, err := EncodeAPRS(o)
	if err != nil {
		t.Fatal(err)
	}
	if got != "360/000t-14" {
		t.Fatalf("EncodeAPRS() = %q", got)
	}
}
