package remoteactions

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// MacroStore is the persistence layer for RemoteActionMacro rows.
// One instance per Service; safe for concurrent use.
type MacroStore struct {
	db *gorm.DB
}

func NewMacroStore(db *gorm.DB) *MacroStore { return &MacroStore{db: db} }

// Create inserts a new macro, stamping CreatedAt and UpdatedAt.
// TargetCall must already be uppercased — validation lives in the
// caller (validate.go).
func (s *MacroStore) Create(ctx context.Context, m *RemoteActionMacro) error {
	if m == nil {
		return errors.New("remoteactions: nil macro")
	}
	now := time.Now().UTC()
	m.CreatedAt = now
	m.UpdatedAt = now
	return s.db.WithContext(ctx).Create(m).Error
}

// Get fetches by primary key. Returns gorm.ErrRecordNotFound when missing.
func (s *MacroStore) Get(ctx context.Context, id uint) (*RemoteActionMacro, error) {
	var m RemoteActionMacro
	if err := s.db.WithContext(ctx).First(&m, id).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// ListByTarget returns macros for one peer, ordered by Position
// ascending then ID ascending (tie-break for ties; defensive against
// reorder races).
func (s *MacroStore) ListByTarget(ctx context.Context, targetCall string) ([]RemoteActionMacro, error) {
	var out []RemoteActionMacro
	if err := s.db.WithContext(ctx).
		Where("target_call = ?", targetCall).
		Order("position ASC, id ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// Update writes Label, ActionName, ArgsString, RemoteOTPCredentialID,
// Position, and bumps UpdatedAt. TargetCall is intentionally NOT
// updatable — the drawer is per-thread; moving a macro between targets
// is a delete+create. Returns gorm.ErrRecordNotFound when no row
// matches m.ID so callers can map to HTTP 404.
func (s *MacroStore) Update(ctx context.Context, m *RemoteActionMacro) error {
	if m == nil || m.ID == 0 {
		return errors.New("remoteactions: nil macro or zero id")
	}
	m.UpdatedAt = time.Now().UTC()
	res := s.db.WithContext(ctx).Model(&RemoteActionMacro{}).
		Where("id = ?", m.ID).
		Updates(map[string]any{
			"label":                    m.Label,
			"action_name":              m.ActionName,
			"args_string":              m.ArgsString,
			"remote_otp_credential_id": m.RemoteOTPCredentialID,
			"position":                 m.Position,
			"updated_at":               m.UpdatedAt,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// Delete removes one macro by id.
func (s *MacroStore) Delete(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&RemoteActionMacro{}, id).Error
}

// ErrReorderUnknownID is returned when Reorder receives an id that
// does not name a macro for the supplied targetCall (deleted, wrong
// target, or never existed). Mapped to HTTP 400 by the handler.
var ErrReorderUnknownID = errors.New("remoteactions: reorder list contains unknown id")

// Reorder rewrites the Position column of every macro for one
// targetCall to match the supplied id order (index 0 -> position 0,
// index 1 -> position 1, ...). Runs in a single transaction so any
// failure rolls the change back atomically. Every id in ids must
// resolve to a row for targetCall: an unknown id returns
// ErrReorderUnknownID and rolls back. Macros for the target that are
// NOT in ids keep their prior position — caller is responsible for
// passing the full live id set if it wants total reordering.
func (s *MacroStore) Reorder(ctx context.Context, targetCall string, ids []uint) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		for i, id := range ids {
			res := tx.Model(&RemoteActionMacro{}).
				Where("id = ? AND target_call = ?", id, targetCall).
				Updates(map[string]any{
					"position":   i,
					"updated_at": now,
				})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return ErrReorderUnknownID
			}
		}
		return nil
	})
}
