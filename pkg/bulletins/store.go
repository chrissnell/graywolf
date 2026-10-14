package bulletins

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"gorm.io/gorm"
)

// Store is the persistence layer for bulletins.
type Store struct {
	db *gorm.DB
}

// NewStore returns a Store backed by db.
func NewStore(db *gorm.DB) *Store { return &Store{db: db} }

// UpsertInbound inserts or updates an inbound bulletin row keyed on
// (from_call, slot). A re-heard bulletin updates the existing row in-place
// so stale content is not accumulated. The partial unique index
// idx_bulletin_inbound_slot enforces uniqueness at the DB layer;
// this function handles the check-then-write logic in Go because SQLite
// ON CONFLICT syntax does not support partial indexes as conflict targets.
func (s *Store) UpsertInbound(ctx context.Context, b *configstore.Bulletin) error {
	now := time.Now().UTC()
	b.Direction = "in"
	b.Unread = true

	var existing configstore.Bulletin
	err := s.db.WithContext(ctx).
		Where("from_call = ? AND slot = ? AND direction = 'in' AND deleted_at IS NULL", b.FromCall, b.Slot).
		First(&existing).Error

	if err == nil {
		// Update fields on the existing row.
		existing.Text = b.Text
		existing.Source = b.Source
		existing.Channel = b.Channel
		existing.RawTNC2 = b.RawTNC2
		existing.ExpiresAt = b.ExpiresAt
		existing.Unread = true
		existing.UpdatedAt = now
		*b = existing
		return s.db.WithContext(ctx).Save(b).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	b.CreatedAt = now
	b.UpdatedAt = now
	return s.db.WithContext(ctx).Create(b).Error
}

// Insert persists a new outbound bulletin row.
func (s *Store) Insert(ctx context.Context, b *configstore.Bulletin) error {
	now := time.Now().UTC()
	b.Direction = "out"
	b.CreatedAt = now
	b.UpdatedAt = now
	return s.db.WithContext(ctx).Create(b).Error
}

// GetByID returns the bulletin with the given id, including soft-deleted rows.
func (s *Store) GetByID(ctx context.Context, id uint64) (*configstore.Bulletin, error) {
	var b configstore.Bulletin
	err := s.db.WithContext(ctx).
		Unscoped().
		Where("id = ?", id).
		First(&b).Error
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// Filter controls which bulletin rows List returns.
type Filter struct {
	Direction string // "in" | "out" | "" (all)
	UnreadOnly bool
}

// List returns all non-deleted bulletins matching f, newest first.
func (s *Store) List(ctx context.Context, f Filter) ([]configstore.Bulletin, error) {
	q := s.db.WithContext(ctx).Order("updated_at DESC")
	if f.Direction != "" {
		q = q.Where("direction = ?", strings.ToLower(f.Direction))
	}
	if f.UnreadOnly {
		q = q.Where("unread = ?", true)
	}
	var rows []configstore.Bulletin
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListPendingSends returns outbound rows whose next_send_at is at or
// before now and whose send_count has not yet reached max_sends.
func (s *Store) ListPendingSends(ctx context.Context, now time.Time) ([]configstore.Bulletin, error) {
	var rows []configstore.Bulletin
	err := s.db.WithContext(ctx).
		Where("direction = ? AND next_send_at <= ? AND send_count < max_sends", "out", now).
		Find(&rows).Error
	return rows, err
}

// Update saves all fields of b.
func (s *Store) Update(ctx context.Context, b *configstore.Bulletin) error {
	b.UpdatedAt = time.Now().UTC()
	return s.db.WithContext(ctx).Save(b).Error
}

// SoftDelete sets deleted_at on the given row, preventing future scheduler
// sends and hiding it from list queries.
func (s *Store) SoftDelete(ctx context.Context, id uint64) error {
	return s.db.WithContext(ctx).
		Model(&configstore.Bulletin{}).
		Where("id = ?", id).
		Update("deleted_at", time.Now().UTC()).Error
}

// MarkRead clears the unread flag on the given row.
func (s *Store) MarkRead(ctx context.Context, id uint64) error {
	return s.db.WithContext(ctx).
		Model(&configstore.Bulletin{}).
		Where("id = ?", id).
		Update("unread", false).Error
}

// MarkAllRead clears unread on all inbound rows.
func (s *Store) MarkAllRead(ctx context.Context) error {
	return s.db.WithContext(ctx).
		Model(&configstore.Bulletin{}).
		Where("direction = ? AND unread = ?", "in", true).
		Update("unread", false).Error
}
