package configstore

import (
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestMigrateIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
}

func TestAudioDeviceCRUD(t *testing.T) {
	s := newTestStore(t)
	d := &AudioDevice{
		Name:       "default",
		SourceType: "soundcard",
		SourcePath: "default",
		SampleRate: 48000,
		Channels:   1,
		Format:     "s16le",
	}
	if err := s.CreateAudioDevice(d); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if d.ID == 0 {
		t.Fatalf("expected autoincrement id")
	}

	got, err := s.GetAudioDevice(d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "default" || got.SourceType != "soundcard" {
		t.Fatalf("unexpected row: %+v", got)
	}

	got.Name = "renamed"
	if err := s.UpdateAudioDevice(got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	list, err := s.ListAudioDevices()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Name != "renamed" {
		t.Fatalf("unexpected list: %+v", list)
	}

	if err := s.DeleteAudioDevice(got.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.GetAudioDevice(got.ID); err == nil {
		t.Fatalf("expected error for missing row")
	}
}

func TestChannelAndPtt(t *testing.T) {
	s := newTestStore(t)

	dev := &AudioDevice{Name: "a", SourceType: "flac", SourcePath: "x.flac", SampleRate: 44100, Channels: 1, Format: "s16le"}
	if err := s.CreateAudioDevice(dev); err != nil {
		t.Fatal(err)
	}
	ch := &Channel{
		Name:          "rx1",
		AudioDeviceID: dev.ID,
		ModemType:     "afsk",
		BitRate:       1200,
		MarkFreq:      1200,
		SpaceFreq:     2200,
		Profile:       "A",
		NumSlicers:    1,
		FixBits:       "none",
	}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatal(err)
	}
	if ch.ID == 0 {
		t.Fatalf("expected channel id")
	}

	ptt := &PttConfig{ChannelID: ch.ID, Method: "none"}
	if err := s.UpsertPttConfig(ptt); err != nil {
		t.Fatalf("UpsertPttConfig: %v", err)
	}
	ptt2 := &PttConfig{ChannelID: ch.ID, Method: "gpio", Device: "/dev/gpiochip0", GpioPin: 17}
	if err := s.UpsertPttConfig(ptt2); err != nil {
		t.Fatalf("Upsert replace: %v", err)
	}
	got, err := s.GetPttConfigForChannel(ch.ID)
	if err != nil {
		t.Fatalf("GetPttConfigForChannel: %v", err)
	}
	if got.Method != "gpio" || got.GpioPin != 17 {
		t.Fatalf("expected gpio ptt, got %+v", got)
	}

	// Verify only one row exists per channel.
	var count int64
	if err := s.DB().Model(&PttConfig{}).Where("channel_id = ?", ch.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 ptt row, got %d", count)
	}
}

func TestChannelValidation_InvalidDeviceID(t *testing.T) {
	s := newTestStore(t)
	ch := &Channel{
		Name: "bad", AudioDeviceID: 999, ModemType: "afsk",
		BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	err := s.CreateChannel(ch)
	if err == nil {
		t.Fatal("expected error for invalid device_id")
	}
}

func TestChannelValidation_AudioChannelOutOfRange(t *testing.T) {
	s := newTestStore(t)
	dev := &AudioDevice{Name: "mono", SourceType: "flac", SourcePath: "x.flac", SampleRate: 44100, Channels: 1, Format: "s16le"}
	if err := s.CreateAudioDevice(dev); err != nil {
		t.Fatal(err)
	}
	ch := &Channel{
		Name: "bad", AudioDeviceID: dev.ID, AudioChannel: 1, // mono device, channel 1 is out of range
		ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	err := s.CreateChannel(ch)
	if err == nil {
		t.Fatal("expected error for audio_channel out of range")
	}
}

func TestChannelValidation_StereoDeviceAcceptsBothChannels(t *testing.T) {
	s := newTestStore(t)
	dev := &AudioDevice{Name: "stereo", SourceType: "soundcard", SampleRate: 48000, Channels: 2, Format: "s16le"}
	if err := s.CreateAudioDevice(dev); err != nil {
		t.Fatal(err)
	}
	for _, ac := range []uint32{0, 1} {
		ch := &Channel{
			Name: "ch", AudioDeviceID: dev.ID, AudioChannel: ac,
			ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
			Profile: "A", NumSlicers: 1, FixBits: "none",
		}
		if err := s.CreateChannel(ch); err != nil {
			t.Fatalf("audio_channel %d should be valid on stereo device: %v", ac, err)
		}
	}
}

func TestFX25IL2PConfig(t *testing.T) {
	s := newTestStore(t)
	dev := &AudioDevice{Name: "d", SourceType: "flac", SourcePath: "x.flac", SampleRate: 44100, Channels: 1, Format: "s16le"}
	if err := s.CreateAudioDevice(dev); err != nil {
		t.Fatal(err)
	}
	ch := &Channel{
		Name: "rx0", AudioDeviceID: dev.ID,
		ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatal(err)
	}
	if ch.FX25Encode || ch.IL2PEncode {
		t.Fatal("expected defaults to be false")
	}
	if err := s.SetChannelFX25(ch.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChannelIL2P(ch.ID, true); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetChannel(ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.FX25Encode || !got.IL2PEncode {
		t.Fatalf("expected both true, got fx25=%v il2p=%v", got.FX25Encode, got.IL2PEncode)
	}
}

func TestWebAuthAndSession(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertWebAuth(&WebAuth{Username: "admin", BcryptHash: "$2a$..."}); err != nil {
		t.Fatal(err)
	}
	w, err := s.GetWebAuth("admin")
	if err != nil {
		t.Fatal(err)
	}
	if w.BcryptHash == "" {
		t.Fatalf("hash missing")
	}
	ws := &WebSession{Token: "tok", Username: "admin", ExpiresAt: time.Now().Add(time.Hour)}
	if err := s.CreateWebSession(ws); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetWebSession("tok"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteWebSession("tok"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetWebSession("tok"); err == nil {
		t.Fatalf("expected missing session")
	}
}
