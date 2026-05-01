package configstore

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
)

// MigrateMapsDownloadSlugs prepends "state/" to any legacy bare-slug
// row in maps_downloads. Idempotent: rows already containing "/" are
// left alone. Run once at startup after AutoMigrate.
//
// Collision policy: if a row already exists at the namespaced target
// (e.g. both "colorado" and "state/colorado" coexist after some prior
// partial migration or hand edit), the legacy bare row is DELETED and
// the namespaced row is kept. The unique-index on slug means a naive
// UPDATE would error and abort startup, so this collision case is
// handled explicitly. The whole pass runs in a single transaction so a
// crash mid-migration leaves the table either fully migrated or fully
// untouched.
func (s *Store) MigrateMapsDownloadSlugs(ctx context.Context) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []MapsDownload
		if err := tx.Find(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			if strings.Contains(r.Slug, "/") {
				continue
			}
			target := "state/" + r.Slug

			var existing MapsDownload
			err := tx.Where("slug = ?", target).First(&existing).Error
			if err == nil {
				// Namespaced row already exists; drop the legacy
				// duplicate to clear the way without clobbering the
				// (presumably newer) namespaced row.
				if err := tx.Delete(&MapsDownload{}, r.ID).Error; err != nil {
					return err
				}
				continue
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err := tx.Model(&MapsDownload{}).
				Where("id = ?", r.ID).
				UpdateColumn("slug", target).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
