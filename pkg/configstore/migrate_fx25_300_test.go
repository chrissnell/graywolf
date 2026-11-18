package configstore

import (
	"context"
	"path/filepath"
	"testing"
)

func TestFX25300MigrationRequiresNewOptInAndPreservesItAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	dev := AudioDevice{Name: "input", Direction: "input", SourceType: "soundcard", SampleRate: 48000, Channels: 1}
	if err = s.CreateAudioDevice(ctx, &dev); err != nil {
		t.Fatal(err)
	}
	hf := Channel{Name: "experimental HF", InputDeviceID: U32Ptr(dev.ID), ModemType: "afsk", BitRate: 300, MarkFreq: 1600, SpaceFreq: 1800, FX25Encode: true, FX25Mode: "32"}
	vhf := Channel{Name: "VHF", InputDeviceID: U32Ptr(dev.ID), ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200, FX25Encode: true}
	for _, c := range []*Channel{&hf, &vhf} {
		if err = s.CreateChannel(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	// Simulate a database saved before this experimental implementation.
	if err = s.db.Exec("PRAGMA user_version = 30").Error; err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.GetChannel(ctx, hf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.FX25Encode || saved.FX25Mode != "32" {
		t.Fatal("legacy HF flag activated or mode lost")
	}
	v, err := s.GetChannel(ctx, vhf.ID)
	if err != nil || !v.FX25Encode {
		t.Fatal("1200-baud setting changed")
	}
	saved.FX25Encode = true
	if err = s.UpdateChannel(ctx, saved); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	saved, err = s.GetChannel(ctx, hf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.FX25Encode || saved.BitRate != 300 || saved.FX25Mode != "32" {
		t.Fatal("new explicit opt-in did not survive restart")
	}
}
