// Package weather defines hardware-independent weather observations and APRS
// encoding. Sources such as Davis serial consoles and WxNow.txt adapters fill
// Observation; they do not construct APRS packets themselves.
package weather

// Observation is one weather snapshot in SI units. A nil pointer means the
// source did not report a usable value; zero remains a valid measurement.
type Observation struct {
	TemperatureC       *float64
	TemperatureIndoorC *float64
	HumidityPct        *float64
	PressureHPa        *float64

	WindSpeedKPH *float64
	WindDirDeg   *int
	WindAvg2KPH  *float64
	WindAvg10KPH *float64
	GustKPH      *float64
	GustDirDeg   *int

	DewPointC *float64

	RainHourMM  *float64
	Rain24hMM   *float64
	RainDayMM   *float64
	RainMonthMM *float64
	Snow24hMM   *float64

	LuminosityWm2  *int
	RawRainCounter *int
	Raining        *bool

	SoftwareType   string
	WeatherUnitTag string
}
