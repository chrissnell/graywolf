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
		RetryMaxAttempts: 4,
		RetentionDays:    0,
	}
	return s.db.WithContext(ctx).Create(seed).Error
}

// ---------------------------------------------------------------------------
// ConversationPrefs (per-thread overrides)
// ---------------------------------------------------------------------------

// GetConversationPrefs returns the override row for one thread, or
// (nil, nil) when none exists (the common case — most conversations
// inherit the global defaults, so no row is written). Callers treat a
// nil result as "SendPath inherit, WaitForAck true". Kind/key are
// matched exactly; callers normalize key to uppercase before calling.
func (s *Store) GetConversationPrefs(ctx context.Context, kind, key string) (*ConversationPrefs, error) {
	var c ConversationPrefs
	err := s.db.WithContext(ctx).
		Where("thread_kind = ? AND thread_key = ?", kind, key).
		First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// UpsertConversationPrefs stores the override row for (kind, key),
// adopting the existing row's ID when one is present so Save updates in
// place rather than inserting a duplicate. When the incoming prefs carry
// only default values (inherit SendPath + WaitForAck true), any existing
// row is deleted instead so the table stays sparse.
func (s *Store) UpsertConversationPrefs(ctx context.Context, cfg *ConversationPrefs) error {
	existing, err := s.GetConversationPrefs(ctx, cfg.ThreadKind, cfg.ThreadKey)
	if err != nil {
		return err
	}
	if cfg.SendPath == "" && cfg.WaitForAck {
		if existing != nil {
			return s.db.WithContext(ctx).Delete(&ConversationPrefs{}, existing.ID).Error
		}
		return nil
	}
	if existing != nil {
		cfg.ID = existing.ID
	}
	return s.db.WithContext(ctx).Save(cfg).Error
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

// GetTacticalCallsignByCallsign returns the entry whose Callsign
// equals the uppercase-normalized argument. Returns (nil, nil) on
// not-found to match the other singleton getters. Used by the invite
// accept handler so it can upsert without racing the autoincrement
// ID.
func (s *Store) GetTacticalCallsignByCallsign(ctx context.Context, callsign string) (*TacticalCallsign, error) {
	var t TacticalCallsign
	err := s.db.WithContext(ctx).Where("callsign = ?", callsign).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ---------------------------------------------------------------------------
// BlockedCallsign CRUD
// ---------------------------------------------------------------------------

// CreateBlockedCallsign inserts a new blocklist entry. Callsign is
// normalized to uppercase by the BlockedCallsign.BeforeSave hook.
func (s *Store) CreateBlockedCallsign(ctx context.Context, b *BlockedCallsign) error {
	return s.db.WithContext(ctx).Create(b).Error
}

// UpdateBlockedCallsign saves changes to an existing row. Callsign
// re-normalization happens via BeforeSave.
func (s *Store) UpdateBlockedCallsign(ctx context.Context, b *BlockedCallsign) error {
	return s.db.WithContext(ctx).Save(b).Error
}

// DeleteBlockedCallsign removes a blocklist entry by id. Historical
// message rows already persisted are unaffected — the block only
// applies to inbound traffic received after the entry is enabled.
func (s *Store) DeleteBlockedCallsign(ctx context.Context, id uint32) error {
	return s.db.WithContext(ctx).Delete(&BlockedCallsign{}, id).Error
}

// GetBlockedCallsign returns a single blocklist entry by id. Returns
// (nil, nil) on not-found to match the other getters.
func (s *Store) GetBlockedCallsign(ctx context.Context, id uint32) (*BlockedCallsign, error) {
	var b BlockedCallsign
	err := s.db.WithContext(ctx).First(&b, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// ListBlockedCallsigns returns every blocklist entry (enabled or not),
// ordered by callsign for stable UI display.
func (s *Store) ListBlockedCallsigns(ctx context.Context) ([]BlockedCallsign, error) {
	var out []BlockedCallsign
	return out, s.db.WithContext(ctx).Order("callsign").Find(&out).Error
}

// ListEnabledBlockedCallsigns returns only the entries with
// Enabled=true. The router uses this at startup and on reload to
// rebuild its in-memory blocklist set.
func (s *Store) ListEnabledBlockedCallsigns(ctx context.Context) ([]BlockedCallsign, error) {
	var out []BlockedCallsign
	return out, s.db.WithContext(ctx).Where("enabled = ?", true).Order("callsign").Find(&out).Error
}
