package beacon

import (
	"math"
	"time"
)

// SmartBeaconConfig mirrors direwolf's SMARTBEACON / HamHUD parameters.
// All speeds are in knots (beacon code operates in the APRS canonical
// unit; callers convert from GPS m/s if needed).
type SmartBeaconConfig struct {
	Enabled   bool
	FastSpeed float64       // knots; at and above → FastRate
	FastRate  time.Duration // beacon interval at high speed
	SlowSpeed float64       // knots; at and below → SlowRate
	SlowRate  time.Duration // beacon interval at low speed
	TurnTime  time.Duration // minimum interval between turn-triggered beacons
	TurnAngle float64       // degrees; fixed component of turn threshold
	TurnSlope float64       // degrees · knots; divided by speed for speed-dep component
}

// DefaultSmartBeacon matches direwolf's defaults.
func DefaultSmartBeacon() SmartBeaconConfig {
	return SmartBeaconConfig{
		Enabled:   false,
		FastSpeed: 60,
		FastRate:  180 * time.Second,
		SlowSpeed: 5,
		SlowRate:  1800 * time.Second,
		TurnTime:  15 * time.Second,
		TurnAngle: 30,
		TurnSlope: 255,
	}
}

// Interval returns the current smart beacon interval for a given speed.
// HamHUD canonical formula:
//
//	speed <= slow_speed  → slow_rate
//	speed >= fast_speed  → fast_rate
//	otherwise            → fast_rate * fast_speed / speed
//
// This is NOT linear interpolation — it is inverse-proportional to speed
// so a vehicle at half fast_speed beacons at twice fast_rate. That's the
// algorithm as documented on hamhud.net.
func (s SmartBeaconConfig) Interval(speedKt float64) time.Duration {
	if speedKt <= s.SlowSpeed {
		return s.SlowRate
	}
	if speedKt >= s.FastSpeed {
		return s.FastRate
	}
	secs := float64(s.FastRate/time.Second) * s.FastSpeed / speedKt
	return time.Duration(secs) * time.Second
}

// TurnThreshold returns the heading-change angle (degrees) that triggers
// a corner-pegging beacon at the given speed. Per HamHUD:
//
//	threshold = turn_angle + turn_slope / speed
//
// Higher speed → smaller threshold (corner pegs fire sooner). At speed
// == 0 the threshold is infinite (no turn-triggered beacons while
// stopped).
func (s SmartBeaconConfig) TurnThreshold(speedKt float64) float64 {
	if speedKt <= 0 {
		return math.Inf(1)
	}
	return s.TurnAngle + s.TurnSlope/speedKt
}

// HeadingDelta returns the absolute angular difference between two
// headings in degrees, wrapped to [0, 180].
func HeadingDelta(a, b float64) float64 {
	d := math.Mod(math.Abs(a-b), 360)
	if d > 180 {
		d = 360 - d
	}
	return d
}
