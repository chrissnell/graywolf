package configstore

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GetWeatherConfig returns the singleton row, creating it with defaults on
// first read (enabled=false, tx_channel_id=0, max_distance_miles=50,
// min_interval_seconds=300, alert_clear_minutes=10).
func (s *Store) GetWeatherConfig(ctx context.Context) (*WeatherConfig, error) {
	var cfg WeatherConfig
	err := s.db.WithContext(ctx).Where("id = 1").First(&cfg).Error
	if err == nil {
		return &cfg, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	// No row yet — seed with defaults.
	cfg = WeatherConfig{
		ID:                 1,
		Enabled:            false,
		TxChannelID:        0,
		MaxDistanceMiles:   50,
		MinIntervalSeconds: 300,
		AlertClearMinutes:  10,
	}
	if err := s.db.WithContext(ctx).Create(&cfg).Error; err != nil {
		return nil, err
	}
	return &cfg, nil
}

// UpdateWeatherConfig writes the singleton row (id forced to 1).
func (s *Store) UpdateWeatherConfig(ctx context.Context, cfg *WeatherConfig) error {
	cfg.ID = 1
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{"enabled", "tx_channel_id", "max_distance_miles", "min_interval_seconds", "alert_clear_minutes", "updated_at"}),
		}).
		Create(cfg).Error
}

// ListWeatherCountyPrefs returns a map of FIPS → AllowTX for all rows in
// the weather_county_prefs table. Counties not present in the table default
// to AllowTX=false.
func (s *Store) ListWeatherCountyPrefs(ctx context.Context) (map[string]bool, error) {
	var rows []WeatherCountyPref
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(rows))
	for _, r := range rows {
		out[r.FIPS] = r.AllowTX
	}
	return out, nil
}

// UpsertWeatherCountyPref sets AllowTX for the given FIPS code. When
// allowTX is false the row is deleted to keep the table sparse; when true
// the row is inserted or updated.
func (s *Store) UpsertWeatherCountyPref(ctx context.Context, fips string, allowTX bool) error {
	if !allowTX {
		return s.db.WithContext(ctx).
			Delete(&WeatherCountyPref{}, "fips = ?", fips).Error
	}
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "fips"}},
			DoUpdates: clause.AssignmentColumns([]string{"allow_tx", "updated_at"}),
		}).
		Create(&WeatherCountyPref{FIPS: fips, AllowTX: true}).Error
}
