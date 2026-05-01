package configstore

import (
	"context"

	"gorm.io/gorm/clause"
)

// GetMessagesConfig returns the singleton row, creating an empty row
// (TxChannel=0, "auto") on first read. Callers handle TxChannel==0 by
// resolving against the live channel inventory in pkg/app.
//
// Uses FirstOrCreate so two concurrent callers on a freshly-opened
// database (e.g. a partial migration) cannot both win the race-to-Create
// and surface a UNIQUE-constraint error. Migration v13 pre-populates
// the row on real systems; this is the belt-and-braces guard.
func (s *Store) GetMessagesConfig(ctx context.Context) (*MessagesConfig, error) {
	var mc MessagesConfig
	err := s.db.WithContext(ctx).
		Where(MessagesConfig{ID: 1}).
		FirstOrCreate(&mc, MessagesConfig{ID: 1}).Error
	if err != nil {
		return nil, err
	}
	return &mc, nil
}

// UpsertMessagesConfig writes the singleton row (id forced to 1).
// TxChannel is validated against ChannelModeLookup at the handler
// layer; the store accepts any uint32 here.
//
// Uses an INSERT ... ON CONFLICT DO UPDATE clause that touches only
// tx_channel + updated_at, so a stale CreatedAt on the caller's struct
// cannot clobber the original row's creation timestamp.
func (s *Store) UpsertMessagesConfig(ctx context.Context, mc *MessagesConfig) error {
	mc.ID = 1
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{"tx_channel", "updated_at"}),
		}).
		Create(mc).Error
}
