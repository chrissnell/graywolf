package configstore

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// UpdatesConfig (singleton)
// ---------------------------------------------------------------------------

// GetUpdatesConfig returns the singleton updates-check configuration
// row. When no row exists (fresh install), returns
// UpdatesConfig{Enabled: true} with no error — the feature is on by
// default and callers don't need a separate seed step. DB errors other
// than not-found are returned verbatim. Mirrors the shape of
// GetStationConfig but with a different zero-value-on-missing contract:
// StationConfig's zero value ("unconfigured") is also the safe default,
// whereas UpdatesConfig's safe default is Enabled=true, which differs
// from the Go zero value.
func (s *Store) GetUpdatesConfig(ctx context.Context) (UpdatesConfig, error) {
	var c UpdatesConfig
	err := s.db.WithContext(ctx).Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return UpdatesConfig{Enabled: true}, nil
	}
	if err != nil {
		return UpdatesConfig{}, err
	}
	return c, nil
}

// UpsertUpdatesConfig stores the singleton updates-check config row.
// When c.ID == 0 and a row already exists, the existing ID is adopted
// so Save updates in place. Unlike StationConfig there is no value to
// normalize (Enabled is a bool).
//
// GORM footgun: the column carries `default:true`, so a plain
// Create with Enabled=false would be silently rewritten to true on
// insert (GORM treats bool zero-values with a default tag as "unset,
// use default"). To defeat that we build the insert via a map, which
// sends every column value verbatim. For updates we do the same with
// UpdateColumns so Enabled=false is always honored.
func (s *Store) UpsertUpdatesConfig(ctx context.Context, c UpdatesConfig) error {
	db := s.db.WithContext(ctx)
	if c.ID == 0 {
		var existing UpdatesConfig
		err := db.Order("id").First(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil {
			c.ID = existing.ID
		}
	}
	if c.ID == 0 {
		// Fresh insert: use a map so Enabled=false is not rewritten to
		// the column default.
		return db.Model(&UpdatesConfig{}).Create(map[string]any{
			"enabled": c.Enabled,
		}).Error
	}
	// Existing row: UpdateColumns with a map forces GORM to write the
	// literal bool regardless of zero-value handling.
	return db.Model(&UpdatesConfig{}).Where("id = ?", c.ID).UpdateColumns(map[string]any{
		"enabled": c.Enabled,
	}).Error
}
