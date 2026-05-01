package configstore

import (
	"context"
	"testing"
)

func TestChannelDefaultsToAPRSMode(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	dev := &AudioDevice{Name: "d", Direction: "input", SourceType: "flac",
		SourcePath: "/tmp/x.flac", SampleRate: 44100, Channels: 1, Format: "s16le"}
	if err := store.CreateAudioDevice(context.Background(), dev); err != nil {
		t.Fatalf("seed device: %v", err)
	}
	ch := &Channel{
		Name: "vhf", InputDeviceID: U32Ptr(dev.ID),
		ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	if err := store.CreateChannel(context.Background(), ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	got, err := store.GetChannel(context.Background(), ch.ID)
	if err != nil {
		t.Fatalf("get channel: %v", err)
	}
	if got.Mode != ChannelModeAPRS {
		t.Fatalf("Mode default = %q, want %q", got.Mode, ChannelModeAPRS)
	}
}

// ValidKissMode is the only gatekeeper between a user-supplied string
// and a stored row, so its rejection behavior — in particular on case
// variants and surrounding whitespace — is part of the API contract.
func TestValidKissMode(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"modem", true},
		{"tnc", true},
		{"", false},
		{"Modem", false},
		{"MODEM", false},
		{"Tnc", false},
		{"TNC", false},
		{"tnc ", false},
		{" tnc", false},
		{"foo", false},
		{"modem\n", false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := ValidKissMode(c.in); got != c.want {
				t.Fatalf("ValidKissMode(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// The exported constants are referenced by the DTO and kiss packages;
// pin their string forms so a rename here fails loudly at the test
// boundary rather than silently breaking downstream consumers.
func TestKissModeConstants(t *testing.T) {
	if KissModeModem != "modem" {
		t.Errorf("KissModeModem = %q, want %q", KissModeModem, "modem")
	}
	if KissModeTnc != "tnc" {
		t.Errorf("KissModeTnc = %q, want %q", KissModeTnc, "tnc")
	}
}

// ValidChannelMode is the only gatekeeper between a user-supplied string
// and a stored row, so its rejection behavior — including case variants
// and surrounding whitespace — is part of the API contract.
func TestValidChannelMode(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"aprs", true},
		{"packet", true},
		{"aprs+packet", true},
		{"", false},
		{"APRS", false},
		{"Packet", false},
		{"aprs ", false},
		{" aprs", false},
		{"garbage", false},
		{"aprs+packet ", false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := ValidChannelMode(c.in); got != c.want {
				t.Fatalf("ValidChannelMode(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// TestChannelModeConstants pins the string forms of Channel.Mode constants
// so a rename fails at the test boundary rather than silently breaking
// downstream consumers.
func TestChannelModeConstants(t *testing.T) {
	if ChannelModeAPRS != "aprs" {
		t.Errorf("ChannelModeAPRS = %q, want %q", ChannelModeAPRS, "aprs")
	}
	if ChannelModePacket != "packet" {
		t.Errorf("ChannelModePacket = %q, want %q", ChannelModePacket, "packet")
	}
	if ChannelModeAPRSPacket != "aprs+packet" {
		t.Errorf("ChannelModeAPRSPacket = %q, want %q", ChannelModeAPRSPacket, "aprs+packet")
	}
}
