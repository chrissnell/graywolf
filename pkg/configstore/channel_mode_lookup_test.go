package configstore

import (
	"context"
	"testing"
)

func TestStoreSatisfiesChannelModeLookup(t *testing.T) {
	t.Parallel()
	var _ ChannelModeLookup = (*Store)(nil)
}

func TestModeForChannel(t *testing.T) {
	s := newTestStore(t)
	dev := &AudioDevice{Name: "d", Direction: "input", SourceType: "flac",
		SourcePath: "/tmp/x.flac", SampleRate: 44100, Channels: 1, Format: "s16le"}
	if err := s.CreateAudioDevice(context.Background(), dev); err != nil {
		t.Fatalf("seed device: %v", err)
	}
	ch := &Channel{Name: "p", InputDeviceID: U32Ptr(dev.ID),
		ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none", Mode: ChannelModePacket}
	if err := s.CreateChannel(context.Background(), ch); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.ModeForChannel(context.Background(), ch.ID)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got != ChannelModePacket {
		t.Fatalf("mode=%q, want packet", got)
	}
}
