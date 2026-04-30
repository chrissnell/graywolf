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
// exists (fresh install), returns MapsConfig{Source: "graywolf"} with
// no error so the UI has a deterministic default without a seed step.
// Graywolf is the default basemap; the maplibre frontend falls back to
// OSM rendering automatically when the device hasn't registered yet,
// so this is safe even before the operator obtains a token. An unknown
// Source value in the stored row is normalized to graywolf.
func (s *Store) GetMapsConfig(ctx context.Context) (MapsConfig, error) {
	var c MapsConfig
	err := s.db.WithContext(ctx).Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return MapsConfig{Source: mapsSourceGraywolf}, nil
	}
	if err != nil {
		return MapsConfig{}, err
	}
	if c.Source != mapsSourceOSM && c.Source != mapsSourceGraywolf {
		c.Source = mapsSourceGraywolf
	}
	return c, nil
}

// UpsertMapsConfig persists the singleton maps preference. Source must
// be one of the two recognized values; anything else is rejected so a
// bad PUT can't corrupt the row. ID is adopted from any existing row
// to preserve the singleton invariant.
//
// This is a full-replace operation: every mutable column (source,
// callsign, token, registered_at) is overwritten with the value on c.
// Callers that intend to update only one field (e.g. just Source)
// MUST GetMapsConfig first, mutate the returned struct, then pass it
// here — otherwise empty fields silently un-register the device.
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
