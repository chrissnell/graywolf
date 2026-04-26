// Singleton accessors for the log-buffer configuration.
//
// Distinguishing trait: GetLogBufferConfig returns three values
// (config, exists, error). Most other singletons here use either a
// `*T` return that distinguishes "no row" with `nil` (e.g. GetGPSConfig)
// or a `T, error` return that ignores absence. Log-buffer needs to
// disambiguate "operator stored MaxRows == 0 to disable persistence"
// from "no row, fall back to the environment default" — neither nil-
// pointer nor zero-value alone can carry that distinction, so the
// boolean joins the contract.
package configstore

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// GetLogBufferConfig returns the singleton log-buffer configuration
// row plus an exists flag. The flag is required because MaxRows == 0
// is a valid override meaning "operator disabled persistence" — the
// caller can't distinguish that from "no row stored, use environment
// default" by inspecting MaxRows alone. DB errors other than not-found
// are returned verbatim.
func (s *Store) GetLogBufferConfig(ctx context.Context) (LogBufferConfig, bool, error) {
	var c LogBufferConfig
	err := s.db.WithContext(ctx).Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return LogBufferConfig{}, false, nil
	}
	if err != nil {
		return LogBufferConfig{}, false, err
	}
	return c, true, nil
}

// UpsertLogBufferConfig stores the singleton log-buffer config row.
// When c.ID == 0 and a row already exists, the existing ID is adopted
// so Save updates in place. MaxRows is written verbatim — including 0,
// which the consumer treats as "disable persistence".
//
// We use a map-based UpdateColumns path so MaxRows == 0 is never
// silently rewritten by GORM's "default" tag handling (same footgun
// the UpdatesConfig CRUD documents at seed_updates.go:38-45).
//
// Side effect: UpdateColumns suppresses auto-timestamps, so
// UpdatedAt stays stale. No consumer reads it today; same behavior
// as seed_updates.go's UpsertUpdatesConfig.
//
// Defensive: when c.ID != 0 but the row does not exist, the
// UpdateColumns call would silently no-op (RowsAffected=0). We
// require RowsAffected >= 1 on the update path so that footgun
// surfaces as an error rather than vanishing.
func (s *Store) UpsertLogBufferConfig(ctx context.Context, c LogBufferConfig) error {
	db := s.db.WithContext(ctx)
	if c.ID == 0 {
		var existing LogBufferConfig
		err := db.Order("id").First(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil {
			c.ID = existing.ID
		}
	}
	if c.ID == 0 {
		return db.Model(&LogBufferConfig{}).Create(map[string]any{
			"max_rows": c.MaxRows,
		}).Error
	}
	res := db.Model(&LogBufferConfig{}).Where("id = ?", c.ID).UpdateColumns(map[string]any{
		"max_rows": c.MaxRows,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("UpsertLogBufferConfig: no row with id=%d", c.ID)
	}
	return nil
}
