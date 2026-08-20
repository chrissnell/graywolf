package configstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrBulletinGlobalProtected is returned when code attempts to delete the
// undeleteable Global group (ID=1).
var ErrBulletinGlobalProtected = errors.New("configstore: the Global bulletin group cannot be deleted")

// ErrBulletinNameReserved is returned when code tries to create a group
// whose name is "" (reserved for Global).
var ErrBulletinNameReserved = errors.New("configstore: name \"\" is reserved for the Global group")

// ListBulletinGroups returns all groups preloaded with their Items.
// The Global group (ID=1) is always returned first.
func (s *Store) ListBulletinGroups(ctx context.Context) ([]BulletinGroup, error) {
	var groups []BulletinGroup
	if err := s.db.WithContext(ctx).
		Preload("Items", func(db *gorm.DB) *gorm.DB {
			return db.Order("slot ASC")
		}).
		Order("CASE WHEN id = 1 THEN 0 ELSE 1 END, name ASC").
		Find(&groups).Error; err != nil {
		return nil, fmt.Errorf("list bulletin groups: %w", err)
	}
	return groups, nil
}

// GetBulletinGroup returns a single group by ID with its Items preloaded.
func (s *Store) GetBulletinGroup(ctx context.Context, id uint32) (*BulletinGroup, error) {
	var g BulletinGroup
	err := s.db.WithContext(ctx).
		Preload("Items", func(db *gorm.DB) *gorm.DB {
			return db.Order("slot ASC")
		}).
		First(&g, "id = ?", id).Error
	if err != nil {
		return nil, fmt.Errorf("get bulletin group %d: %w", id, err)
	}
	return &g, nil
}

// CreateBulletinGroup inserts a new group and seeds its 10 item slots.
// Name="" is reserved for Global and returns ErrBulletinNameReserved.
func (s *Store) CreateBulletinGroup(ctx context.Context, g *BulletinGroup) error {
	if g.Name == "" {
		return ErrBulletinNameReserved
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(g).Error; err != nil {
			return fmt.Errorf("create bulletin group: %w", err)
		}
		for slot := 0; slot <= 9; slot++ {
			item := &BulletinItem{GroupID: g.ID, Slot: slot, Text: "", Active: false, SendCount: 0}
			if err := tx.Create(item).Error; err != nil {
				return fmt.Errorf("seed bulletin item slot %d: %w", slot, err)
			}
		}
		return nil
	})
}

// UpdateBulletinGroup saves group-level settings. ID=1 (Global) may be
// updated for settings changes but its Name cannot be changed from "".
func (s *Store) UpdateBulletinGroup(ctx context.Context, g *BulletinGroup) error {
	update := map[string]any{
		"send_path":    g.SendPath,
		"digi_path":    g.DigiPath,
		"initial_rate": g.InitialRate,
		"decay_factor": g.DecayFactor,
		"stable_rate":  g.StableRate,
		"active":       g.Active,
	}
	// Preserve name="" for Global; non-Global groups can rename freely.
	if g.ID != 1 {
		update["name"] = g.Name
	}
	res := s.db.WithContext(ctx).Model(&BulletinGroup{}).Where("id = ?", g.ID).Updates(update)
	if res.Error != nil {
		return fmt.Errorf("update bulletin group %d: %w", g.ID, res.Error)
	}
	return nil
}

// DeleteBulletinGroup removes a group and its items. ID=1 (Global) is
// protected and returns ErrBulletinGlobalProtected.
func (s *Store) DeleteBulletinGroup(ctx context.Context, id uint32) error {
	if id == 1 {
		return ErrBulletinGlobalProtected
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", id).Delete(&BulletinItem{}).Error; err != nil {
			return fmt.Errorf("delete bulletin items for group %d: %w", id, err)
		}
		if err := tx.Delete(&BulletinGroup{}, "id = ?", id).Error; err != nil {
			return fmt.Errorf("delete bulletin group %d: %w", id, err)
		}
		return nil
	})
}

// UpsertBulletinItem sets the text and active flag for a single slot.
// Uses ON CONFLICT DO UPDATE so callers never need to distinguish insert
// from update.
func (s *Store) UpsertBulletinItem(ctx context.Context, groupID uint32, slot int, text string, active bool) error {
	item := BulletinItem{
		GroupID: groupID,
		Slot:    slot,
		Text:    text,
		Active:  active,
	}
	res := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "group_id"}, {Name: "slot"}},
			DoUpdates: clause.AssignmentColumns([]string{"text", "active"}),
		}).
		Create(&item)
	if res.Error != nil {
		return fmt.Errorf("upsert bulletin item (group=%d slot=%d): %w", groupID, slot, res.Error)
	}
	return nil
}

// IncrBulletinSendCount atomically increments send_count and records the send timestamp.
func (s *Store) IncrBulletinSendCount(ctx context.Context, groupID uint32, slot int, sentAt time.Time) error {
	res := s.db.WithContext(ctx).
		Model(&BulletinItem{}).
		Where("group_id = ? AND slot = ?", groupID, slot).
		Updates(map[string]any{
			"send_count":   gorm.Expr("send_count + 1"),
			"last_sent_at": sentAt,
		})
	if res.Error != nil {
		return fmt.Errorf("incr bulletin send_count (group=%d slot=%d): %w", groupID, slot, res.Error)
	}
	return nil
}

// ResetBulletinGroupSchedule clears send_count and last_sent_at for every item
// in a group so the decay schedule restarts from scratch on next activation.
func (s *Store) ResetBulletinGroupSchedule(ctx context.Context, groupID uint32) error {
	res := s.db.WithContext(ctx).
		Model(&BulletinItem{}).
		Where("group_id = ?", groupID).
		Updates(map[string]any{
			"send_count":   0,
			"last_sent_at": nil,
		})
	if res.Error != nil {
		return fmt.Errorf("reset bulletin group schedule (group=%d): %w", groupID, res.Error)
	}
	return nil
}

// ClearBulletinItem blanks text, sets active=false, and resets send_count=0.
func (s *Store) ClearBulletinItem(ctx context.Context, groupID uint32, slot int) error {
	res := s.db.WithContext(ctx).
		Model(&BulletinItem{}).
		Where("group_id = ? AND slot = ?", groupID, slot).
		Updates(map[string]any{
			"text":         "",
			"active":       false,
			"send_count":   0,
			"last_sent_at": nil,
		})
	if res.Error != nil {
		return fmt.Errorf("clear bulletin item (group=%d slot=%d): %w", groupID, slot, res.Error)
	}
	return nil
}

// ListActiveBulletinGroups returns only active groups whose at least one item
// is also active. Used by the bulletin scheduler to build its run list.
func (s *Store) ListActiveBulletinGroups(ctx context.Context) ([]BulletinGroup, error) {
	var groups []BulletinGroup
	if err := s.db.WithContext(ctx).
		Preload("Items", func(db *gorm.DB) *gorm.DB {
			return db.Where("active = ?", true).Order("slot ASC")
		}).
		Where("active = ?", true).
		Find(&groups).Error; err != nil {
		return nil, fmt.Errorf("list active bulletin groups: %w", err)
	}
	return groups, nil
}
