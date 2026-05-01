package configstore

import "context"

// ChannelModeLookup is the small read-only surface the TX-gating
// subsystems (beacon, digipeater, igate, messages, ax25conn) consume
// to decide whether to permit a transmit on a given channel. The
// concrete *Store implements it; tests can substitute a fake.
type ChannelModeLookup interface {
	ModeForChannel(ctx context.Context, channelID uint32) (string, error)
}

// ModeForChannel returns the Mode column for the given channel id.
// Returns ChannelModeAPRS and a nil error when the row is missing,
// matching the migration default — TX subsystems treat "row not
// found" as the conservative APRS-only choice.
func (s *Store) ModeForChannel(ctx context.Context, channelID uint32) (string, error) {
	if channelID == 0 {
		return ChannelModeAPRS, nil
	}
	var mode string
	err := s.db.WithContext(ctx).
		Table("channels").
		Where("id = ?", channelID).
		Select("mode").
		Scan(&mode).Error
	if err != nil {
		return "", err
	}
	if mode == "" {
		return ChannelModeAPRS, nil
	}
	return mode, nil
}
