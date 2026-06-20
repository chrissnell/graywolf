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
// The global "world" archive is a legitimate bare slug, not a legacy
// state slug. This pass predates the world archive, so earlier releases
// wrongly rewrote "world" to "state/world" on every startup, which made
// the region picker (which looks up the fixed "world" slug) show a
// Download button for an already-downloaded world map. A correct "world"
// row is now left untouched and a mangled "state/world" row is repaired
// back to "world".
//
// Collision policy: if a row already exists at the rename target (e.g.
// both "colorado" and "state/colorado" coexist after some prior partial
// migration or hand edit), the source row is DELETED and the target row
// is kept. The unique-index on slug means a naive UPDATE would error and
// abort startup, so this collision case is handled explicitly. The whole
// pass runs in a single transaction so a crash mid-migration leaves the
// table either fully migrated or fully untouched.
func (s *Store) MigrateMapsDownloadSlugs(ctx context.Context) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []MapsDownload
		if err := tx.Find(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			switch {
			case r.Slug == "world":
				// Legitimate global archive slug; never namespace it.
				continue
			case r.Slug == "state/world":
				// Undo the earlier mis-migration of the world archive.
				if err := renameDownloadSlug(tx, r, "world"); err != nil {
					return err
				}
			case strings.Contains(r.Slug, "/"):
				continue
			default:
				if err := renameDownloadSlug(tx, r, "state/"+r.Slug); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// renameDownloadSlug changes r's slug to target within tx, honouring the
// unique index on slug: if a row already holds target, the source row r
// is dropped rather than clobbering the (presumably current) target row.
func renameDownloadSlug(tx *gorm.DB, r MapsDownload, target string) error {
	var existing MapsDownload
	err := tx.Where("slug = ?", target).First(&existing).Error
	if err == nil {
		return tx.Delete(&MapsDownload{}, r.ID).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return tx.Model(&MapsDownload{}).
		Where("id = ?", r.ID).
		UpdateColumn("slug", target).Error
}
