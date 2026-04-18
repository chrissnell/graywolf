package configstore

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// Historical gorm-tag defaults for configstore.Beacon.Sb* columns. These
// are the values the legacy per-beacon schema wrote when a user never
// touched the knobs. Used only by seedSmartBeaconFromLegacyBeacons to
// decide whether any beacon carries meaningful tunings worth migrating
// into the new singleton. Keep in sync with the `gorm:"default:..."`
// tags on Beacon (see models.go) — they remain on those columns until
// the Phase 5 follow-up drops them.
const (
	legacySbFastSpeed   uint32 = 60
	legacySbSlowSpeed   uint32 = 5
	legacySbFastRate    uint32 = 60
	legacySbSlowRate    uint32 = 1800
	legacySbTurnAngle   uint32 = 30
	legacySbTurnSlope   uint32 = 255
	legacySbMinTurnTime uint32 = 5
)

// ---------------------------------------------------------------------------
// SmartBeaconConfig (singleton)
// ---------------------------------------------------------------------------

// GetSmartBeaconConfig returns the singleton SmartBeacon configuration
// row. When no row exists, returns (nil, nil) — the caller interprets
// that as "apply defaults from beacon.DefaultSmartBeacon()". DB errors
// are returned as non-nil errors. Matches the established singleton
// contract used by GetDigipeaterConfig, GetIGateConfig, GetGPSConfig.
func (s *Store) GetSmartBeaconConfig(ctx context.Context) (*SmartBeaconConfig, error) {
	var c SmartBeaconConfig
	err := s.db.WithContext(ctx).Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// UpsertSmartBeaconConfig stores the singleton row. Either inserts or
// updates: if the caller passes cfg.ID == 0 and a row already exists,
// the existing ID is adopted so Save updates in place rather than
// creating a second row. Matches UpsertDigipeaterConfig et al.
func (s *Store) UpsertSmartBeaconConfig(ctx context.Context, cfg *SmartBeaconConfig) error {
	if cfg.ID == 0 {
		existing, err := s.GetSmartBeaconConfig(ctx)
		if err != nil {
			return err
		}
		if existing != nil {
			cfg.ID = existing.ID
		}
	}
	return s.db.WithContext(ctx).Save(cfg).Error
}

// seedSmartBeaconFromLegacyBeacons migrates any per-beacon Sb* tunings
// into the new SmartBeacon singleton on first run. It is a no-op when:
//   - the singleton row already exists (migration already ran, or a
//     user has saved a global config via the API), OR
//   - no beacon row carries Sb* values that differ from the legacy
//     gorm-tag defaults (nothing to preserve — let DefaultSmartBeacon
//     serve as the answer).
//
// When exactly one beacon has non-default Sb* values, its values become
// the singleton. When multiple beacons have non-default values, the
// first one (lowest ID) wins — under the old schema there was no UI to
// set these, and the plan documents the behavior as "preserve any
// tunings a user had set"; picking the first keeps the seed
// deterministic without inventing merge semantics. Enabled is forced to
// false: per-beacon Sb* data was never active (no UI wired it up), so
// the user must opt in via the new global toggle.
//
// Runs on every startup after AutoMigrate — must stay cheap and
// idempotent.
func (s *Store) seedSmartBeaconFromLegacyBeacons(ctx context.Context) error {
	existing, err := s.GetSmartBeaconConfig(ctx)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	var matches []Beacon
	if err := s.db.WithContext(ctx).
		Where("sb_fast_speed != ? OR sb_slow_speed != ? OR sb_fast_rate != ? OR sb_slow_rate != ? OR sb_turn_angle != ? OR sb_turn_slope != ? OR sb_min_turn_time != ?",
			legacySbFastSpeed, legacySbSlowSpeed, legacySbFastRate, legacySbSlowRate,
			legacySbTurnAngle, legacySbTurnSlope, legacySbMinTurnTime).
		Order("id").
		Limit(1).
		Find(&matches).Error; err != nil {
		return err
	}
	if len(matches) == 0 {
		return nil
	}
	candidate := matches[0]
	seed := &SmartBeaconConfig{
		Enabled:     false,
		FastSpeedKt: candidate.SbFastSpeed,
		FastRateSec: candidate.SbFastRate,
		SlowSpeedKt: candidate.SbSlowSpeed,
		SlowRateSec: candidate.SbSlowRate,
		MinTurnDeg:  candidate.SbTurnAngle,
		TurnSlope:   candidate.SbTurnSlope,
		MinTurnSec:  candidate.SbMinTurnTime,
	}
	return s.UpsertSmartBeaconConfig(ctx, seed)
}
