package configstore

import "context"

// MigrateMapsDownloadBBox is a no-op shim. GORM's AutoMigrate (called
// from Store.bootstrap) sees the new BBox field on MapsDownload and
// adds the bbox TEXT column automatically; this helper exists so the
// migration step is observable (callable from wiring.go) and so we
// have a hook if we ever need to perform repair beyond AutoMigrate.
// Idempotent: AutoMigrate is itself idempotent for column adds.
func (s *Store) MigrateMapsDownloadBBox(ctx context.Context) error {
	_ = ctx
	return s.db.AutoMigrate(&MapsDownload{})
}
