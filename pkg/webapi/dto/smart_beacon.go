package dto

import (
	"fmt"
	"math"
	"time"

	"github.com/chrissnell/graywolf/pkg/beacon"
	"github.com/chrissnell/graywolf/pkg/configstore"
)

// SmartBeaconConfigRequest is the body accepted by PUT /api/smart-beacon.
//
// The wire shape is snake_case and matches the UI mock byte-for-byte.
// Speeds are in knots, rates are in seconds, the turn angle is in
// degrees, and the turn slope is in degrees·knots. These field names
// are the source of truth for the on-the-wire contract; renaming any
// of them is a breaking change for every consumer of the generated
// TypeScript client.
type SmartBeaconConfigRequest struct {
	// Enabled is true when SmartBeacon curve computation is active.
	// When false, every beacon with smart_beacon=true falls back to
	// its fixed interval.
	Enabled bool `json:"enabled"`
	// FastSpeedKt is the knots threshold at or above which beacons
	// transmit at FastRateSec. The "moving fast" end of the curve.
	// Must be greater than SlowSpeedKt.
	FastSpeedKt uint32 `json:"fast_speed"`
	// FastRateSec is the beacon interval in seconds at or above
	// FastSpeedKt. Must be shorter than SlowRateSec.
	FastRateSec uint32 `json:"fast_rate"`
	// SlowSpeedKt is the knots threshold at or below which beacons
	// transmit at SlowRateSec. Must be greater than zero to prevent a
	// degenerate middle-branch division by zero inside
	// beacon.SmartBeaconConfig.Interval().
	SlowSpeedKt uint32 `json:"slow_speed"`
	// SlowRateSec is the beacon interval in seconds at or below
	// SlowSpeedKt. Must be longer than FastRateSec.
	SlowRateSec uint32 `json:"slow_rate"`
	// MinTurnDeg is the fixed-component turn angle threshold, in
	// degrees, used in the corner-pegging formula. Must be in
	// [1, 179].
	MinTurnDeg uint32 `json:"min_turn_angle"`
	// TurnSlope is the speed-dependent component (degrees·knots) of
	// the corner-pegging turn threshold. Higher speed → lower
	// effective threshold → corner pegs fire sooner. Must be greater
	// than zero.
	TurnSlope uint32 `json:"turn_slope"`
	// MinTurnSec is the minimum interval in seconds between
	// turn-triggered beacons. Must be greater than zero.
	MinTurnSec uint32 `json:"min_turn_time"`
}

// Validate enforces the SmartBeacon parameter constraints documented in
// the HamHUD/direwolf references. Errors use human-readable field
// names that mirror the wire tags so 400 responses point the UI at the
// offending field.
func (r SmartBeaconConfigRequest) Validate() error {
	if r.SlowSpeedKt == 0 {
		return fmt.Errorf("slow_speed must be greater than 0")
	}
	if r.FastSpeedKt <= r.SlowSpeedKt {
		return fmt.Errorf("fast_speed must be greater than slow_speed")
	}
	if r.FastRateSec == 0 {
		return fmt.Errorf("fast_rate must be greater than 0")
	}
	if r.SlowRateSec == 0 {
		return fmt.Errorf("slow_rate must be greater than 0")
	}
	if r.FastRateSec >= r.SlowRateSec {
		return fmt.Errorf("fast_rate must be shorter than slow_rate")
	}
	if r.MinTurnDeg < 1 || r.MinTurnDeg > 179 {
		return fmt.Errorf("min_turn_angle must be between 1 and 179")
	}
	if r.TurnSlope == 0 {
		return fmt.Errorf("turn_slope must be greater than 0")
	}
	if r.MinTurnSec == 0 {
		return fmt.Errorf("min_turn_time must be greater than 0")
	}
	return nil
}

// SmartBeaconConfigResponse is the body returned by GET/PUT
// /api/smart-beacon. Shape matches SmartBeaconConfigRequest — the
// singleton has no id or timestamps exposed on the wire.
type SmartBeaconConfigResponse struct {
	// Enabled is true when SmartBeacon curve computation is active.
	Enabled bool `json:"enabled"`
	// FastSpeedKt is the knots threshold at or above which beacons
	// transmit at FastRateSec.
	FastSpeedKt uint32 `json:"fast_speed"`
	// FastRateSec is the beacon interval in seconds at or above
	// FastSpeedKt.
	FastRateSec uint32 `json:"fast_rate"`
	// SlowSpeedKt is the knots threshold at or below which beacons
	// transmit at SlowRateSec.
	SlowSpeedKt uint32 `json:"slow_speed"`
	// SlowRateSec is the beacon interval in seconds at or below
	// SlowSpeedKt.
	SlowRateSec uint32 `json:"slow_rate"`
	// MinTurnDeg is the fixed-component turn angle threshold in
	// degrees.
	MinTurnDeg uint32 `json:"min_turn_angle"`
	// TurnSlope is the speed-dependent component (degrees·knots) of
	// the corner-pegging turn threshold.
	TurnSlope uint32 `json:"turn_slope"`
	// MinTurnSec is the minimum interval in seconds between
	// turn-triggered beacons.
	MinTurnSec uint32 `json:"min_turn_time"`
}

// SmartBeaconConfigFromModel converts the persisted singleton into the
// response DTO. Straight field copy — model and DTO share the same
// units.
func SmartBeaconConfigFromModel(m configstore.SmartBeaconConfig) SmartBeaconConfigResponse {
	return SmartBeaconConfigResponse{
		Enabled:     m.Enabled,
		FastSpeedKt: m.FastSpeedKt,
		FastRateSec: m.FastRateSec,
		SlowSpeedKt: m.SlowSpeedKt,
		SlowRateSec: m.SlowRateSec,
		MinTurnDeg:  m.MinTurnDeg,
		TurnSlope:   m.TurnSlope,
		MinTurnSec:  m.MinTurnSec,
	}
}

// SmartBeaconConfigDefaults returns the response DTO populated from
// beacon.DefaultSmartBeacon(), which is the single source of truth
// for SmartBeacon parameter defaults. GET /api/smart-beacon uses this
// on a fresh install where no singleton row has been written yet.
//
// Unit conversions applied to cross the package boundary:
//   - FastSpeed / SlowSpeed (float64 knots) → uint32 knots (rounded).
//   - FastRate / SlowRate / TurnTime (time.Duration) → uint32 seconds.
//   - TurnAngle / TurnSlope (float64) → uint32 (rounded).
func SmartBeaconConfigDefaults() SmartBeaconConfigResponse {
	d := beacon.DefaultSmartBeacon()
	return SmartBeaconConfigResponse{
		Enabled:     d.Enabled,
		FastSpeedKt: uint32(math.Round(d.FastSpeed)),
		FastRateSec: uint32(d.FastRate / time.Second),
		SlowSpeedKt: uint32(math.Round(d.SlowSpeed)),
		SlowRateSec: uint32(d.SlowRate / time.Second),
		MinTurnDeg:  uint32(math.Round(d.TurnAngle)),
		TurnSlope:   uint32(math.Round(d.TurnSlope)),
		MinTurnSec:  uint32(d.TurnTime / time.Second),
	}
}

// SmartBeaconConfigToModel maps a validated request into a storage
// model. Caller is responsible for stamping ID on update via the
// upsert path (configstore adopts the existing singleton id when
// cfg.ID == 0).
func SmartBeaconConfigToModel(r SmartBeaconConfigRequest) configstore.SmartBeaconConfig {
	return configstore.SmartBeaconConfig{
		Enabled:     r.Enabled,
		FastSpeedKt: r.FastSpeedKt,
		FastRateSec: r.FastRateSec,
		SlowSpeedKt: r.SlowSpeedKt,
		SlowRateSec: r.SlowRateSec,
		MinTurnDeg:  r.MinTurnDeg,
		TurnSlope:   r.TurnSlope,
		MinTurnSec:  r.MinTurnSec,
	}
}
