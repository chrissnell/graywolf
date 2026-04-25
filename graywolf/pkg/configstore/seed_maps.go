package configstore

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// MapsConfig (singleton)
// ---------------------------------------------------------------------------

// mapsSourceOSM and mapsSourceGraywolf are the only two valid Source
// values. Anything else read from the row falls back to osm on read so
// the frontend always sees one of the two valid values.
const (
	mapsSourceOSM      = "osm"
	mapsSourceGraywolf = "graywolf"
)

// GetMapsConfig returns the singleton maps preference. When no row
// exists (fresh install), returns MapsConfig{Source: "osm"} with no
// error so the UI has a deterministic default without a seed step. An
// unknown Source value in the stored row is normalized to osm.
func (s *Store) GetMapsConfig(ctx context.Context) (MapsConfig, error) {
	var c MapsConfig
	err := s.db.WithContext(ctx).Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return MapsConfig{Source: mapsSourceOSM}, nil
	}
	if err != nil {
		return MapsConfig{}, err
	}
	if c.Source != mapsSourceOSM && c.Source != mapsSourceGraywolf {
		c.Source = mapsSourceOSM
	}
	return c, nil
}

// UpsertMapsConfig stores the singleton maps preference. Values other
// than "osm" or "graywolf" are rejected so a bad PUT can't corrupt the
// row. When c.ID == 0 and a row already exists, the existing ID is
// adopted so the singleton invariant is preserved.
func (s *Store) UpsertMapsConfig(ctx context.Context, c MapsConfig) error {
	if c.Source != mapsSourceOSM && c.Source != mapsSourceGraywolf {
		return errors.New("source must be 'osm' or 'graywolf'")
	}
	db := s.db.WithContext(ctx)
	if c.ID == 0 {
		var existing MapsConfig
		err := db.Order("id").First(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil {
			c.ID = existing.ID
		}
	}
	cols := map[string]any{
		"source":        c.Source,
		"callsign":      c.Callsign,
		"token":         c.Token,
		"registered_at": c.RegisteredAt,
	}
	if c.ID == 0 {
		return db.Model(&MapsConfig{}).Create(cols).Error
	}
	return db.Model(&MapsConfig{}).Where("id = ?", c.ID).UpdateColumns(cols).Error
}
