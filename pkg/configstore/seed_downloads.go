package configstore

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// validDownloadStatuses lists every status the MapsDownload table is
// allowed to hold. Anything else is rejected at upsert time so a
// malformed handler can't corrupt the row.
var validDownloadStatuses = map[string]bool{
	"pending":     true,
	"downloading": true,
	"complete":    true,
	"error":       true,
}

// ListMapsDownloads returns every download row, ordered by slug for
// deterministic UI display. Returns an empty slice (not nil) on a
// fresh install.
func (s *Store) ListMapsDownloads(ctx context.Context) ([]MapsDownload, error) {
	var rows []MapsDownload
	if err := s.db.WithContext(ctx).Order("slug").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// GetMapsDownload returns the row for slug, or a zero-value struct
// (ID==0) if none exists. Callers check ID==0 to detect absence.
func (s *Store) GetMapsDownload(ctx context.Context, slug string) (MapsDownload, error) {
	var d MapsDownload
	err := s.db.WithContext(ctx).Where("slug = ?", slug).First(&d).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return MapsDownload{}, nil
	}
	return d, err
}

// UpsertMapsDownload writes the row. Rows are keyed by slug
// (uniqueIndex on the model); a second call with the same slug
// updates in place rather than inserting a duplicate. Status must be
// one of the four documented values; the slug must be non-empty.
//
// Slug format is namespaced: state/<slug>, country/<iso2>, or
// province/<iso2>/<slug>. Legacy bare-slug rows (e.g. "colorado")
// from pre-namespaced installs are migrated in place at startup by
// MigrateMapsDownloadSlugs. The store layer does not enforce the
// grammar -- the webapi layer validates against the live catalog
// before any write reaches here.
func (s *Store) UpsertMapsDownload(ctx context.Context, d MapsDownload) error {
	if !validDownloadStatuses[d.Status] {
		return fmt.Errorf("invalid status %q", d.Status)
	}
	if d.Slug == "" {
		return errors.New("slug required")
	}
	db := s.db.WithContext(ctx)
	var existing MapsDownload
	err := db.Where("slug = ?", d.Slug).First(&existing).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err == nil {
		d.ID = existing.ID
	}
	cols := map[string]any{
		"slug":             d.Slug,
		"status":           d.Status,
		"bytes_total":      d.BytesTotal,
		"bytes_downloaded": d.BytesDownloaded,
		"downloaded_at":    d.DownloadedAt,
		"error_message":    d.ErrorMessage,
	}
	if d.ID == 0 {
		return db.Model(&MapsDownload{}).Create(cols).Error
	}
	return db.Model(&MapsDownload{}).Where("id = ?", d.ID).UpdateColumns(cols).Error
}

// DeleteMapsDownload removes the row for slug. Idempotent — deleting
// an absent row is not an error.
func (s *Store) DeleteMapsDownload(ctx context.Context, slug string) error {
	return s.db.WithContext(ctx).Where("slug = ?", slug).Delete(&MapsDownload{}).Error
}
