package configstore

import "context"

// GetMessagesConfig returns the singleton row, creating an empty row
// (TxChannel=0, "auto") on first read. Callers handle TxChannel==0 by
// running ResolveTxChannel against the channel inventory.
func (s *Store) GetMessagesConfig(ctx context.Context) (*MessagesConfig, error) {
	var mc MessagesConfig
	err := s.db.WithContext(ctx).First(&mc, 1).Error
	if err == nil {
		return &mc, nil
	}
	mc = MessagesConfig{ID: 1}
	if err := s.db.WithContext(ctx).Create(&mc).Error; err != nil {
		return nil, err
	}
	return &mc, nil
}

// UpsertMessagesConfig writes the singleton row (id forced to 1).
// TxChannel is validated against ChannelModeLookup at the handler
// layer; the store accepts any uint32 here.
func (s *Store) UpsertMessagesConfig(ctx context.Context, mc *MessagesConfig) error {
	mc.ID = 1
	return s.db.WithContext(ctx).Save(mc).Error
}
