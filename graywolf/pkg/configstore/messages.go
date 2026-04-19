package configstore

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// MessagePreferences (singleton)
// ---------------------------------------------------------------------------

// GetMessagePreferences returns the singleton preferences row. The row
// is seeded with defaults by seedMessagePreferences on first migrate,
// so a nil return indicates a DB error path only (preserved for
// consistency with the other singleton getters).
func (s *Store) GetMessagePreferences(ctx context.Context) (*MessagePreferences, error) {
	var c MessagePreferences
	err := s.db.WithContext(ctx).Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// UpsertMessagePreferences stores the singleton row. When cfg.ID == 0
// and a row already exists, the existing ID is adopted so Save updates
// in place. Matches UpsertDigipeaterConfig et al.
func (s *Store) UpsertMessagePreferences(ctx context.Context, cfg *MessagePreferences) error {
	if cfg.ID == 0 {
		existing, err := s.GetMessagePreferences(ctx)
		if err != nil {
			return err
		}
		if existing != nil {
			cfg.ID = existing.ID
		}
	}
	return s.db.WithContext(ctx).Save(cfg).Error
}

// seedMessagePreferences inserts the default preferences row on first
// run. Relies on the gorm-tag defaults declared on the struct — passing
// a zero MessagePreferences to Save causes SQLite to apply the column
// defaults, giving callers the canonical "fresh install" state without
// duplicating literals here.
func (s *Store) seedMessagePreferences(ctx context.Context) error {
	existing, err := s.GetMessagePreferences(ctx)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	seed := &MessagePreferences{
		FallbackPolicy:   "is_fallback",
		DefaultPath:      "WIDE1-1,WIDE2-1",
		RetryMaxAttempts: 5,
		RetentionDays:    0,
	}
	return s.db.WithContext(ctx).Create(seed).Error
}

// ---------------------------------------------------------------------------
// TacticalCallsign CRUD
// ---------------------------------------------------------------------------

// CreateTacticalCallsign inserts a new tactical entry. Callsign is
// normalized to uppercase by the TacticalCallsign.BeforeSave hook.
func (s *Store) CreateTacticalCallsign(ctx context.Context, t *TacticalCallsign) error {
	return s.db.WithContext(ctx).Create(t).Error
}

// UpdateTacticalCallsign saves changes to an existing row. Callsign
// re-normalization happens via BeforeSave.
func (s *Store) UpdateTacticalCallsign(ctx context.Context, t *TacticalCallsign) error {
	return s.db.WithContext(ctx).Save(t).Error
}

// DeleteTacticalCallsign removes a tactical entry by id. Historical
// message rows keyed by the tactical label persist so the thread stays
// a read-only archive — only the monitor entry is deleted.
func (s *Store) DeleteTacticalCallsign(ctx context.Context, id uint32) error {
	return s.db.WithContext(ctx).Delete(&TacticalCallsign{}, id).Error
}

// GetTacticalCallsign returns a single tactical entry by id. Returns
// (nil, nil) on not-found to match the other singleton helpers.
func (s *Store) GetTacticalCallsign(ctx context.Context, id uint32) (*TacticalCallsign, error) {
	var t TacticalCallsign
	err := s.db.WithContext(ctx).First(&t, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ListTacticalCallsigns returns every tactical entry (enabled or not),
// ordered by callsign for stable UI display.
func (s *Store) ListTacticalCallsigns(ctx context.Context) ([]TacticalCallsign, error) {
	var out []TacticalCallsign
	return out, s.db.WithContext(ctx).Order("callsign").Find(&out).Error
}

// ListEnabledTacticalCallsigns returns only the entries with
// Enabled=true. The router uses this at startup and on preferences
// reload to rebuild its in-memory matching set.
func (s *Store) ListEnabledTacticalCallsigns(ctx context.Context) ([]TacticalCallsign, error) {
	var out []TacticalCallsign
	return out, s.db.WithContext(ctx).Where("enabled = ?", true).Order("callsign").Find(&out).Error
}
