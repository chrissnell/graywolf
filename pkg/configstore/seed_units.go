package configstore

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// UnitsConfig (singleton)
// ---------------------------------------------------------------------------

// unitsSystemImperial and unitsSystemMetric are the only two valid
// System values. Anything else read from the row (e.g. a hand-edited
// DB or a forward-incompatible value) falls back to imperial on read.
const (
	unitsSystemImperial = "imperial"
	unitsSystemMetric   = "metric"
)

// GetUnitsConfig returns the singleton measurement-system preference.
// When no row exists (fresh install), returns UnitsConfig{System:
// "imperial"} with no error so the UI has a deterministic default
// without a seed step. An unknown System value in the stored row is
// normalized to imperial so the frontend always sees one of the two
// valid values.
func (s *Store) GetUnitsConfig(ctx context.Context) (UnitsConfig, error) {
	var c UnitsConfig
	err := s.db.WithContext(ctx).Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return UnitsConfig{System: unitsSystemImperial}, nil
	}
	if err != nil {
		return UnitsConfig{}, err
	}
	if c.System != unitsSystemImperial && c.System != unitsSystemMetric {
		c.System = unitsSystemImperial
	}
	return c, nil
}

// UpsertUnitsConfig stores the singleton measurement-system preference.
// Values other than "imperial" or "metric" are rejected so a bad PUT
// can't corrupt the row. When c.ID == 0 and a row already exists, the
// existing ID is adopted so the singleton invariant is preserved.
func (s *Store) UpsertUnitsConfig(ctx context.Context, c UnitsConfig) error {
	if c.System != unitsSystemImperial && c.System != unitsSystemMetric {
		return errors.New("system must be 'imperial' or 'metric'")
	}
	db := s.db.WithContext(ctx)
	if c.ID == 0 {
		var existing UnitsConfig
		err := db.Order("id").First(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil {
			c.ID = existing.ID
		}
	}
	if c.ID == 0 {
		return db.Model(&UnitsConfig{}).Create(map[string]any{
			"system": c.System,
		}).Error
	}
	return db.Model(&UnitsConfig{}).Where("id = ?", c.ID).UpdateColumns(map[string]any{
		"system": c.System,
	}).Error
}
