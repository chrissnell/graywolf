package configstore

import (
	"context"
	"errors"

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
	return db.Model(&LogBufferConfig{}).Where("id = ?", c.ID).UpdateColumns(map[string]any{
		"max_rows": c.MaxRows,
	}).Error
}
