package configstore

import (
	"context"
	"errors"
	"regexp"

	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// ThemeConfig (singleton)
// ---------------------------------------------------------------------------

// defaultThemeID is the fallback when no row exists. Keep in sync with
// the "default" field in graywolf/web/themes/themes.json.
const defaultThemeID = "graywolf"

// validThemeIDPattern accepts kebab-case lowercase alphanumeric, up to
// 64 characters. This is deliberately loose so new themes can be added
// by dropping a CSS file + a themes.json entry in the frontend without
// touching the backend. The frontend holds the authoritative set of
// themes; the server only guards against obviously malformed input
// (injection, path traversal, pathological lengths).
var validThemeIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// IsValidTheme reports whether id is a well-formed theme identifier.
// It does NOT verify the id corresponds to a shipped theme — that's
// the frontend's job.
func IsValidTheme(id string) bool {
	return validThemeIDPattern.MatchString(id)
}

// GetThemeConfig returns the singleton theme preference. Fresh install
// returns ThemeConfig{ThemeID: "graywolf"} with no error. A row with a
// malformed id (e.g. hand-edited DB) is normalized to the default on
// read so the frontend never sees garbage.
func (s *Store) GetThemeConfig(ctx context.Context) (ThemeConfig, error) {
	var c ThemeConfig
	err := s.db.WithContext(ctx).Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ThemeConfig{ThemeID: defaultThemeID}, nil
	}
	if err != nil {
		return ThemeConfig{}, err
	}
	if !IsValidTheme(c.ThemeID) {
		c.ThemeID = defaultThemeID
	}
	return c, nil
}

// UpsertThemeConfig stores the singleton theme preference. Rejects
// malformed ids so a bad PUT can't corrupt the row. Preserves the
// singleton ID across upserts.
func (s *Store) UpsertThemeConfig(ctx context.Context, c ThemeConfig) error {
	if !IsValidTheme(c.ThemeID) {
		return errors.New("invalid theme id")
	}
	db := s.db.WithContext(ctx)
	if c.ID == 0 {
		var existing ThemeConfig
		err := db.Order("id").First(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil {
			c.ID = existing.ID
		}
	}
	if c.ID == 0 {
		return db.Model(&ThemeConfig{}).Create(map[string]any{
			"theme_id": c.ThemeID,
		}).Error
	}
	return db.Model(&ThemeConfig{}).Where("id = ?", c.ID).UpdateColumns(map[string]any{
		"theme_id": c.ThemeID,
	}).Error
}
