package configstore

import (
	"context"
	"strings"
)

// MigrateMapsDownloadSlugs prepends "state/" to any legacy bare-slug
// row in maps_downloads. Idempotent: rows already containing "/" are
// left alone. Run once at startup after AutoMigrate.
func (s *Store) MigrateMapsDownloadSlugs(ctx context.Context) error {
	var rows []MapsDownload
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		if strings.Contains(r.Slug, "/") {
			continue
		}
		newSlug := "state/" + r.Slug
		if err := s.db.WithContext(ctx).
			Model(&MapsDownload{}).
			Where("id = ?", r.ID).
			UpdateColumn("slug", newSlug).Error; err != nil {
			return err
		}
	}
	return nil
}
