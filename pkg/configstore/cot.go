package configstore

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// CotSettings (singleton)
// ---------------------------------------------------------------------------

// GetCotSettings returns the singleton CoT settings row, or (nil, nil)
// when none has been saved yet -- callers apply cot.DefaultSettings()
// in that case, mirroring GetSmartBeaconConfig's "no row = defaults"
// contract.
func (s *Store) GetCotSettings(ctx context.Context) (*CotSettings, error) {
	var c CotSettings
	err := s.db.WithContext(ctx).Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// UpsertCotSettings stores the singleton row, adopting the existing id
// on first save so a repeated PUT updates in place rather than
// inserting a second row. Matches UpsertSmartBeaconConfig.
func (s *Store) UpsertCotSettings(ctx context.Context, cfg *CotSettings) error {
	if cfg.ID == 0 {
		existing, err := s.GetCotSettings(ctx)
		if err != nil {
			return err
		}
		if existing != nil {
			cfg.ID = existing.ID
		}
	}
	return s.db.WithContext(ctx).Save(cfg).Error
}

// ---------------------------------------------------------------------------
// CotTarget
// ---------------------------------------------------------------------------

// ListCotTargets returns every CoT target, newest first (both active
// and exhausted -- the webapi/frontend split active vs. inactive by
// comparing TxCount to NumTransmits).
func (s *Store) ListCotTargets(ctx context.Context) ([]CotTarget, error) {
	var out []CotTarget
	return out, s.db.WithContext(ctx).Order("id desc").Find(&out).Error
}

func (s *Store) GetCotTarget(ctx context.Context, id uint32) (*CotTarget, error) {
	var t CotTarget
	if err := s.db.WithContext(ctx).First(&t, id).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) CreateCotTarget(ctx context.Context, t *CotTarget) error {
	return s.db.WithContext(ctx).Create(t).Error
}

func (s *Store) DeleteCotTarget(ctx context.Context, id uint32) error {
	return s.db.WithContext(ctx).Delete(&CotTarget{}, id).Error
}

// DeleteInactiveCotTargets bulk-deletes every exhausted (TxCount >=
// NumTransmits) target and reports how many rows were removed, for the
// Inactive-tab "Delete all" button.
func (s *Store) DeleteInactiveCotTargets(ctx context.Context) (int64, error) {
	res := s.db.WithContext(ctx).Where("tx_count >= num_transmits").Delete(&CotTarget{})
	return res.RowsAffected, res.Error
}

// DueCotTargets returns every target whose NextSendAt is set and has
// arrived. Backs pkg/cot.Scheduler.Run's poll loop -- the scheduler is
// stateless, so this query is the single source of truth for "what's
// due right now" rather than an in-memory schedule that could drift
// out of sync with deletes.
func (s *Store) DueCotTargets(ctx context.Context, now time.Time) ([]CotTarget, error) {
	var out []CotTarget
	err := s.db.WithContext(ctx).
		Where("next_send_at IS NOT NULL AND next_send_at <= ?", now).
		Find(&out).Error
	return out, err
}

// RecordCotSend increments TxCount, stamps FirstSentAt the first time
// it is called for a target, stamps LastSentAt, and sets NextSendAt to
// the caller-computed value (nil once the target is exhausted). The
// caller (pkg/cot.Scheduler) owns the decaying-schedule math; this
// method only persists the result -- kept here rather than in pkg/cot
// to avoid an import cycle (pkg/cot needs configstore's model types).
func (s *Store) RecordCotSend(ctx context.Context, id uint32, sentAt time.Time, nextSendAt *time.Time) error {
	t, err := s.GetCotTarget(ctx, id)
	if err != nil {
		return err
	}
	t.TxCount++
	if t.FirstSentAt == nil {
		fs := sentAt
		t.FirstSentAt = &fs
	}
	ls := sentAt
	t.LastSentAt = &ls
	t.NextSendAt = nextSendAt
	return s.db.WithContext(ctx).Save(t).Error
}
