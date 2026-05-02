package configstore

import (
	"context"

	"gorm.io/gorm/clause"
)

// GetAX25TerminalConfig returns the singleton AX25TerminalConfig row,
// creating one with sane defaults on first read. Migration v14 seeds
// the row at startup; this FirstOrCreate is the belt-and-braces guard
// for any code path that opens a fresh database without going through
// Migrate() (e.g. in-process integration tests).
func (s *Store) GetAX25TerminalConfig(ctx context.Context) (*AX25TerminalConfig, error) {
	var cfg AX25TerminalConfig
	err := s.db.WithContext(ctx).
		Where(AX25TerminalConfig{ID: 1}).
		Attrs(AX25TerminalConfig{
			ID:             1,
			ScrollbackRows: 1000,
			DefaultModulo:  8,
			DefaultPaclen:  256,
			MacrosJSON:     "[]",
		}).
		FirstOrCreate(&cfg).Error
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// UpsertAX25TerminalConfig writes the singleton (id forced to 1). The
// REST handler converts the macros DTO array into MacrosJSON before
// calling.
func (s *Store) UpsertAX25TerminalConfig(ctx context.Context, cfg *AX25TerminalConfig) error {
	cfg.ID = 1
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"scrollback_rows", "cursor_blink", "default_modulo",
				"default_paclen", "macros_json", "raw_tail_filter",
				"updated_at",
			}),
		}).
		Create(cfg).Error
}
