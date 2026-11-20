package configstore

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// TelemetryConfig is one station-wide reporter. Channel selects RF delivery only.
// Defaults are applied by the reporter/API; zero booleans must stay false.
type TelemetryConfig struct {
	ID                uint32 `gorm:"primaryKey" json:"-"`
	Enabled           bool   `json:"enabled"`
	Callsign          string `json:"callsign"`
	Channel           uint32 `json:"channel"`
	Delivery          string `json:"delivery"`
	RFPath            string `json:"rf_path"`
	AllowAbove255     bool   `json:"allow_above_255"`
	MetadataISSeconds uint32 `json:"metadata_is_seconds"`
	MetadataRFSeconds uint32 `json:"metadata_rf_seconds"`
}

// TelemetryState holds only bounded sequence/definition state, not sample history.
type TelemetryState struct {
	ID        uint32 `gorm:"primaryKey"`
	StateJSON string `gorm:"not null"`
}

func (s *Store) GetTelemetryConfig(ctx context.Context) (*TelemetryConfig, error) {
	var c TelemetryConfig
	err := s.db.WithContext(ctx).First(&c, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &c, err
}
func (s *Store) UpsertTelemetryConfig(ctx context.Context, c *TelemetryConfig) error {
	c.ID = 1
	return s.db.WithContext(ctx).Save(c).Error
}
func (s *Store) GetTelemetryState(ctx context.Context) (string, error) {
	var c TelemetryState
	err := s.db.WithContext(ctx).First(&c, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	return c.StateJSON, err
}
func (s *Store) SaveTelemetryState(ctx context.Context, state string) error {
	return s.db.WithContext(ctx).Save(&TelemetryState{ID: 1, StateJSON: state}).Error
}
