package configstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// CreateAX25SessionProfile inserts a new profile row. ID is set on
// return.
func (s *Store) CreateAX25SessionProfile(ctx context.Context, p *AX25SessionProfile) error {
	return s.db.WithContext(ctx).Create(p).Error
}

// GetAX25SessionProfile loads a profile by id; ErrRecordNotFound when
// missing so callers can map to 404.
func (s *Store) GetAX25SessionProfile(ctx context.Context, id uint32) (*AX25SessionProfile, error) {
	var p AX25SessionProfile
	if err := s.db.WithContext(ctx).First(&p, id).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// ListAX25SessionProfiles returns every saved profile ordered with
// pinned rows first, then recents by LastUsed desc, then by Name.
// The pre-connect form renders both groups in this order.
func (s *Store) ListAX25SessionProfiles(ctx context.Context) ([]AX25SessionProfile, error) {
	var rows []AX25SessionProfile
	err := s.db.WithContext(ctx).
		Order("pinned DESC, last_used DESC NULLS LAST, name ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// UpdateAX25SessionProfile replaces all editable columns for the row
// identified by p.ID. Pinned + LastUsed are managed by their own
// helpers (PinAX25SessionProfile, TouchAX25SessionProfileLastUsed).
func (s *Store) UpdateAX25SessionProfile(ctx context.Context, p *AX25SessionProfile) error {
	if p.ID == 0 {
		return errors.New("ax25 profile: ID required for update")
	}
	return s.db.WithContext(ctx).
		Model(&AX25SessionProfile{}).
		Where("id = ?", p.ID).
		Updates(map[string]any{
			"name":       p.Name,
			"local_call": p.LocalCall,
			"local_ssid": p.LocalSSID,
			"dest_call":  p.DestCall,
			"dest_ssid":  p.DestSSID,
			"via_path":   p.ViaPath,
			"mod128":     p.Mod128,
			"paclen":     p.Paclen,
			"maxframe":   p.Maxframe,
			"t1_ms":      p.T1MS,
			"t2_ms":      p.T2MS,
			"t3_ms":      p.T3MS,
			"n2":         p.N2,
			"channel_id": p.ChannelID,
		}).Error
}

// DeleteAX25SessionProfile removes the row by id. Idempotent: deleting a
// missing row returns nil so callers can wire it directly to a DELETE
// handler without a 404 race.
func (s *Store) DeleteAX25SessionProfile(ctx context.Context, id uint32) error {
	return s.db.WithContext(ctx).Delete(&AX25SessionProfile{}, id).Error
}

// PinAX25SessionProfile flips Pinned to true on the row, promoting it
// from recents into the permanent list.
func (s *Store) PinAX25SessionProfile(ctx context.Context, id uint32, pinned bool) error {
	res := s.db.WithContext(ctx).
		Model(&AX25SessionProfile{}).
		Where("id = ?", id).
		Update("pinned", pinned)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// TouchAX25SessionProfileLastUsed updates LastUsed on a recent. Used by
// the OnStateChange(CONNECTED) hook in the WebSocket bridge so the
// recents list reflects the most recent successful connection.
func (s *Store) TouchAX25SessionProfileLastUsed(ctx context.Context, id uint32, when time.Time) error {
	return s.db.WithContext(ctx).
		Model(&AX25SessionProfile{}).
		Where("id = ?", id).
		Update("last_used", when).Error
}

// UpsertRecentAX25SessionProfile creates or updates a recent profile
// entry keyed by (LocalCall, LocalSSID, DestCall, DestSSID, ViaPath,
// ChannelID). Used by the bridge so successive connects to the same
// peer/path don't fan out the recents list.
//
// On insert: stamps LastUsed = now, Pinned = false.
// On match: only updates LastUsed (the operator's prior settings stay).
//
// After the upsert, trims unpinned recents back down to the cap (20)
// by deleting the oldest LastUsed rows.
func (s *Store) UpsertRecentAX25SessionProfile(ctx context.Context, p *AX25SessionProfile, capRecents int) error {
	if p.LocalCall == "" || p.DestCall == "" {
		return fmt.Errorf("ax25 recent: LocalCall + DestCall required")
	}
	now := time.Now().UTC()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing AX25SessionProfile
		q := tx.Where(
			"local_call = ? AND local_ssid = ? AND dest_call = ? AND dest_ssid = ? AND via_path = ? AND COALESCE(channel_id, 0) = COALESCE(?, 0)",
			p.LocalCall, p.LocalSSID, p.DestCall, p.DestSSID, p.ViaPath, p.ChannelID,
		)
		err := q.First(&existing).Error
		if err == nil {
			existing.LastUsed = &now
			if err := tx.Save(&existing).Error; err != nil {
				return err
			}
			*p = existing
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			p.Pinned = false
			p.LastUsed = &now
			if err := tx.Create(p).Error; err != nil {
				return err
			}
		} else {
			return err
		}
		// Trim unpinned recents past cap. Skip the cap check when
		// capRecents <= 0 (caller opted out of the trim).
		if capRecents <= 0 {
			return nil
		}
		var unpinned []AX25SessionProfile
		if err := tx.Where("pinned = 0").
			Order("last_used DESC NULLS LAST, id DESC").
			Find(&unpinned).Error; err != nil {
			return err
		}
		if len(unpinned) <= capRecents {
			return nil
		}
		ids := make([]uint32, 0, len(unpinned)-capRecents)
		for _, r := range unpinned[capRecents:] {
			ids = append(ids, r.ID)
		}
		if len(ids) == 0 {
			return nil
		}
		return tx.Where("id IN ?", ids).Delete(&AX25SessionProfile{}).Error
	})
}
