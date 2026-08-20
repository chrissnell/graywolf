package configstore

import (
	"context"

	"gorm.io/gorm/clause"
)

// GetBulletinsConfig returns the singleton row, creating it with defaults
// (TxChannel=0 auto, SendPath="rf") on first read.
func (s *Store) GetBulletinsConfig(ctx context.Context) (*BulletinsConfig, error) {
	var bc BulletinsConfig
	err := s.db.WithContext(ctx).
		Where(BulletinsConfig{ID: 1}).
		FirstOrCreate(&bc, BulletinsConfig{ID: 1, SendPath: "rf"}).Error
	if err != nil {
		return nil, err
	}
	return &bc, nil
}

// UpsertBulletinsConfig writes the singleton row (id forced to 1).
func (s *Store) UpsertBulletinsConfig(ctx context.Context, bc *BulletinsConfig) error {
	bc.ID = 1
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{"tx_channel", "send_path", "updated_at"}),
		}).
		Create(bc).Error
}
