package configstore

import (
	"context"
	"errors"
	"time"
)

// ActionInvocationFilter narrows a ListActionInvocations call. All
// fields are optional; the zero filter returns the most recent rows up
// to the default limit. Search is a case-insensitive substring match
// applied to action_name_at, sender_call, and status_detail in a single
// OR predicate so the UI's free-text box can probe multiple columns
// without the caller having to choose.
type ActionInvocationFilter struct {
	ActionID   *uint
	SenderCall string
	Status     string
	Source     string // 'rf' | 'is'
	Search     string
	Limit      int
	Offset     int
}

func (s *Store) InsertActionInvocation(ctx context.Context, row *ActionInvocation) error {
	if row == nil {
		return errors.New("configstore: nil invocation")
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}
	return s.db.WithContext(ctx).Create(row).Error
}

func (s *Store) ListActionInvocations(ctx context.Context, f ActionInvocationFilter) ([]ActionInvocation, error) {
	q := s.db.WithContext(ctx).Order("created_at DESC")
	if f.ActionID != nil {
		q = q.Where("action_id = ?", *f.ActionID)
	}
	if f.SenderCall != "" {
		q = q.Where("sender_call = ?", f.SenderCall)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Source != "" {
		q = q.Where("source = ?", f.Source)
	}
	if f.Search != "" {
		like := "%" + f.Search + "%"
		q = q.Where("action_name_at LIKE ? OR sender_call LIKE ? OR status_detail LIKE ?", like, like, like)
	}
	if f.Limit <= 0 {
		f.Limit = 100
	}
	q = q.Limit(f.Limit).Offset(f.Offset)
	var out []ActionInvocation
	if err := q.Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// PruneActionInvocations enforces the audit-log retention contract:
// rows older than maxAge are deleted unconditionally; if the post-age
// row count still exceeds maxRows, the oldest excess rows are deleted
// too. Either bound on its own keeps the table bounded; running both
// captures the more aggressive of the two so a quiet operator who
// hasn't crossed the time bound but somehow accumulated a million rows
// (e.g. a runaway test fixture) still stays under the count cap.
// Returns the total number of rows deleted across both passes.
func (s *Store) PruneActionInvocations(ctx context.Context, maxRows int, maxAge time.Duration) (int, error) {
	cutoff := time.Now().UTC().Add(-maxAge)
	res := s.db.WithContext(ctx).
		Where("created_at < ?", cutoff).
		Delete(&ActionInvocation{})
	if res.Error != nil {
		return 0, res.Error
	}
	deleted := int(res.RowsAffected)

	var total int64
	if err := s.db.WithContext(ctx).Model(&ActionInvocation{}).Count(&total).Error; err != nil {
		return deleted, err
	}
	if int(total) <= maxRows {
		return deleted, nil
	}
	excess := int(total) - maxRows
	subq := s.db.WithContext(ctx).Model(&ActionInvocation{}).
		Order("created_at ASC").Limit(excess).Select("id")
	res2 := s.db.WithContext(ctx).
		Where("id IN (?)", subq).
		Delete(&ActionInvocation{})
	if res2.Error != nil {
		return deleted, res2.Error
	}
	return deleted + int(res2.RowsAffected), nil
}
