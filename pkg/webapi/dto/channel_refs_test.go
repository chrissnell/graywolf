package dto

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stubLookup is a tiny ChannelLookup fake. existing is the set of known
// channel IDs; err, when non-nil, is returned unchanged for any lookup.
type stubLookup struct {
	existing map[uint32]bool
	err      error
}

func (s stubLookup) ChannelExists(_ context.Context, id uint32) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	return s.existing[id], nil
}

func TestValidateChannelRef_Matrix(t *testing.T) {
	ctx := context.Background()
	lookup := stubLookup{existing: map[uint32]bool{1: true, 7: true}}

	cases := []struct {
		name      string
		lookup    ChannelLookup
		fieldName string
		channelID uint32
		wantErr   string // empty = no error; otherwise substring match
	}{
		{"zero passes (unset sentinel)", lookup, "channel", 0, ""},
		{"known channel passes", lookup, "channel", 1, ""},
		{"unknown channel rejected", lookup, "channel", 99, "channel 99 does not exist"},
		{"field name surfaces in error", lookup, "to_channel", 99, "to_channel"},
		{"nil lookup defensive reject", nil, "channel", 1, "channel lookup unavailable"},
		{"lookup error propagates", stubLookup{err: errors.New("boom")}, "channel", 1, "boom"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateChannelRef(ctx, c.lookup, c.fieldName, c.channelID)
			if c.wantErr == "" {
				if err != nil {
					t.Errorf("want nil, got %v", err)
				}
				return
			}
			if err == nil {
				t.Errorf("want error containing %q, got nil", c.wantErr)
				return
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("want error containing %q, got %q", c.wantErr, err.Error())
			}
		})
	}
}
