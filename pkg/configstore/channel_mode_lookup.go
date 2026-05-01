package configstore

import (
	"context"
	"log/slog"
)

// ChannelModeLookup is the small read-only surface the TX-gating
// subsystems (beacon, digipeater, igate, messages, ax25conn) consume
// to decide whether to permit a transmit on a given channel. The
// concrete *Store implements it; tests can substitute a fake.
type ChannelModeLookup interface {
	ModeForChannel(ctx context.Context, channelID uint32) (string, error)
}

// ModeForChannel returns the Mode column for the given channel id.
// Returns ChannelModeAPRS and a nil error when the channelID is 0
// (no channel selected) or when the row does not exist -- TX subsystems
// treat both cases as the conservative APRS-only choice. Existing rows
// always carry a non-empty Mode (validateChannel normalizes empty to
// ChannelModeAPRS), so the empty-string branch is solely a missing-row
// guard.
//
// Missing-row hits emit a debug log so operators investigating why a
// downstream subsystem (e.g. ax25conn refusing to bind) believes a
// channel is APRS-only can correlate against an actually-deleted
// channel ID without re-deriving the lookup path.
func (s *Store) ModeForChannel(ctx context.Context, channelID uint32) (string, error) {
	if channelID == 0 {
		return ChannelModeAPRS, nil
	}
	// Narrow read: avoid full Channel struct alloc; only the mode column matters.
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
		slog.Default().Debug("ModeForChannel: missing row, defaulting to APRS",
			"channel_id", channelID)
		return ChannelModeAPRS, nil
	}
	return mode, nil
}
