package configstore

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// migrateBulletins creates the bulletin_groups and bulletin_items tables
// (AutoMigrate handles the schema) and seeds the undeleteable Global group
// (ID=1, Name="") plus its 10 empty slots if they are absent.
func migrateBulletins(tx *gorm.DB) error {
	// Seed the Global group if it doesn't exist yet.
	// We insert with an explicit id=1 so the Global identity is stable
	// across databases; a fresh DB may have auto-increment starting at 1
	// anyway, but being explicit is safer for tests and edge cases.
	var count int64
	if err := tx.Model(&BulletinGroup{}).Where("id = 1").Count(&count).Error; err != nil {
		return fmt.Errorf("probe bulletin_groups for global seed: %w", err)
	}
	if count == 0 {
		global := &BulletinGroup{
			ID:          1,
			Name:        "",
			SendPath:    "rf",
			DigiPath:    "",
			InitialRate: 60,
			DecayFactor: 1.5,
			StableRate:  600,
			Active:      false,
		}
		if err := tx.Create(global).Error; err != nil {
			return fmt.Errorf("seed global bulletin group: %w", err)
		}
	}

	// Seed the 10 item slots for the Global group if absent.
	for slot := 0; slot <= 9; slot++ {
		var itemCount int64
		if err := tx.Model(&BulletinItem{}).
			Where("group_id = 1 AND slot = ?", slot).
			Count(&itemCount).Error; err != nil {
			return fmt.Errorf("probe bulletin_items slot %d: %w", slot, err)
		}
		if itemCount == 0 {
			item := &BulletinItem{GroupID: 1, Slot: slot, Text: "", Active: false, SendCount: 0}
			if err := tx.Create(item).Error; err != nil {
				return fmt.Errorf("seed bulletin item slot %d: %w", slot, err)
			}
		}
	}
	return nil
}

// seedBulletinGlobalGroup ensures the undeleteable Global bulletin group
// (ID=1, Name="") and its 10 item slots exist. Idempotent — no-op once all
// rows are present. Called from Migrate() on every startup so any database
// where migration 29 was skipped (e.g. a device whose user_version was already
// ≥ 29 from a prior dev build) recovers automatically.
func (s *Store) seedBulletinGlobalGroup(ctx context.Context) error {
	var count int64
	if err := s.db.WithContext(ctx).Model(&BulletinGroup{}).Where("id = 1").Count(&count).Error; err != nil {
		return fmt.Errorf("probe bulletin_groups for global seed: %w", err)
	}
	if count == 0 {
		global := &BulletinGroup{
			ID:          1,
			Name:        "",
			SendPath:    "rf",
			DigiPath:    "",
			InitialRate: 60,
			DecayFactor: 1.5,
			StableRate:  600,
			Active:      false,
		}
		if err := s.db.WithContext(ctx).Create(global).Error; err != nil {
			return fmt.Errorf("seed global bulletin group: %w", err)
		}
	}
	for slot := 0; slot <= 9; slot++ {
		var itemCount int64
		if err := s.db.WithContext(ctx).Model(&BulletinItem{}).
			Where("group_id = 1 AND slot = ?", slot).
			Count(&itemCount).Error; err != nil {
			return fmt.Errorf("probe bulletin_items slot %d: %w", slot, err)
		}
		if itemCount == 0 {
			item := &BulletinItem{GroupID: 1, Slot: slot, Text: "", Active: false, SendCount: 0}
			if err := s.db.WithContext(ctx).Create(item).Error; err != nil {
				return fmt.Errorf("seed bulletin item slot %d: %w", slot, err)
			}
		}
	}
	return nil
}
