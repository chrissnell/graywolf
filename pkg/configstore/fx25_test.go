package configstore

import (
	"context"
	"testing"
)

func TestFX25Scope(t *testing.T) {
	good := Channel{InputDeviceID: U32Ptr(1), ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200, FX25Encode: true}
	if err := good.ValidateFX25(); err != nil {
		t.Fatal(err)
	}
	cases := []Channel{good, good, good, good, good, good}
	cases[0].BitRate = 300
	cases[1].ModemType = "9600"
	cases[2].MarkFreq = 1600
	cases[3].SpaceFreq = 1800
	cases[4].IL2PEncode = true
	cases[5].InputDeviceID = nil
	for _, ch := range cases {
		if ch.ValidateFX25() == nil {
			t.Fatalf("accepted %+v", ch)
		}
		ch.FX25Encode = false
		if err := ch.ValidateFX25(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFX25ModeDefaultsAndValidation(t *testing.T) {
	for _, mode := range []string{"", "auto", "16", "32", "64"} {
		ch := Channel{FX25Mode: mode}
		if err := ch.ValidateFX25(); err != nil {
			t.Fatal(err)
		}
		if mode == "auto" && ch.FX25ParityBytes() != 0 {
			t.Fatal("auto mapping")
		}
	}
	for _, mode := range []string{"0", "off", "8", "128"} {
		if (Channel{FX25Mode: mode}).ValidateFX25() == nil {
			t.Fatalf("accepted %s", mode)
		}
	}
}

func TestFX25ModeAddedToExistingDatabase(t *testing.T) {
	path := t.TempDir() + "/config.db"
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	dev := &AudioDevice{Name: "input", Direction: "input", SourceType: "soundcard", SampleRate: 48000, Channels: 1}
	if err := store.CreateAudioDevice(ctx, dev); err != nil {
		t.Fatal(err)
	}
	ch := &Channel{Name: "VHF", InputDeviceID: U32Ptr(dev.ID), ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200, FX25Encode: true}
	if err := store.CreateChannel(ctx, ch); err != nil {
		t.Fatal(err)
	}
	if err := store.db.Migrator().DropColumn(&Channel{}, "fx25_mode"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	saved, err := store.GetChannel(ctx, ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.FX25Encode || saved.FX25Mode != "auto" {
		t.Fatalf("old settings/default not preserved: %+v", saved)
	}
	saved.FX25Mode = "64"
	saved.FX25Encode = false
	if err := store.UpdateChannel(ctx, saved); err != nil {
		t.Fatal(err)
	}
	again, err := store.GetChannel(ctx, ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.FX25Encode || again.FX25Mode != "64" {
		t.Fatal("mode did not persist while disabled")
	}
}
