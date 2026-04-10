package configstore

import "testing"

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

func TestSeedDefaults(t *testing.T) {
	s := newTestStore(t)

	// Empty DB should seed one device and one channel.
	if err := s.seedDefaults(); err != nil {
		t.Fatalf("seedDefaults: %v", err)
	}
	devs, _ := s.ListAudioDevices()
	if len(devs) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devs))
	}
	if devs[0].SourceType != "soundcard" {
		t.Errorf("expected soundcard, got %q", devs[0].SourceType)
	}
	if devs[0].SourcePath != "default" {
		t.Errorf("expected source_path 'default', got %q", devs[0].SourcePath)
	}
	if devs[0].SampleRate != 48000 {
		t.Errorf("expected sample_rate 48000, got %d", devs[0].SampleRate)
	}
	chs, _ := s.ListChannels()
	if len(chs) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(chs))
	}
	if chs[0].ModemType != "afsk" || chs[0].BitRate != 1200 {
		t.Errorf("unexpected channel config: %+v", chs[0])
	}

	// Calling again should be a no-op.
	if err := s.seedDefaults(); err != nil {
		t.Fatalf("seedDefaults second call: %v", err)
	}
	devs2, _ := s.ListAudioDevices()
	if len(devs2) != 1 {
		t.Fatalf("expected still 1 device after re-seed, got %d", len(devs2))
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

	dev := &AudioDevice{Name: "a", Direction: "input", SourceType: "flac", SourcePath: "x.flac", SampleRate: 44100, Channels: 1, Format: "s16le"}
	if err := s.CreateAudioDevice(dev); err != nil {
		t.Fatal(err)
	}
	ch := &Channel{
		Name:          "rx1",
		InputDeviceID: dev.ID,
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
		Name: "bad", InputDeviceID: 999, ModemType: "afsk",
		BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	err := s.CreateChannel(ch)
	if err == nil {
		t.Fatal("expected error for invalid input_device_id")
	}
}

func TestChannelValidation_InputChannelOutOfRange(t *testing.T) {
	s := newTestStore(t)
	dev := &AudioDevice{Name: "mono", Direction: "input", SourceType: "flac", SourcePath: "x.flac", SampleRate: 44100, Channels: 1, Format: "s16le"}
	if err := s.CreateAudioDevice(dev); err != nil {
		t.Fatal(err)
	}
	ch := &Channel{
		Name: "bad", InputDeviceID: dev.ID, InputChannel: 1, // mono device, channel 1 is out of range
		ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	err := s.CreateChannel(ch)
	if err == nil {
		t.Fatal("expected error for input_channel out of range")
	}
}

func TestChannelValidation_StereoDeviceAcceptsBothChannels(t *testing.T) {
	s := newTestStore(t)
	dev := &AudioDevice{Name: "stereo", Direction: "input", SourceType: "soundcard", SampleRate: 48000, Channels: 2, Format: "s16le"}
	if err := s.CreateAudioDevice(dev); err != nil {
		t.Fatal(err)
	}
	for _, ac := range []uint32{0, 1} {
		ch := &Channel{
			Name: "ch", InputDeviceID: dev.ID, InputChannel: ac,
			ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
			Profile: "A", NumSlicers: 1, FixBits: "none",
		}
		if err := s.CreateChannel(ch); err != nil {
			t.Fatalf("input_channel %d should be valid on stereo device: %v", ac, err)
		}
	}
}

func TestChannelValidation_DirectionEnforcement(t *testing.T) {
	s := newTestStore(t)
	outDev := &AudioDevice{Name: "out", Direction: "output", SourceType: "soundcard", SampleRate: 48000, Channels: 1, Format: "s16le"}
	if err := s.CreateAudioDevice(outDev); err != nil {
		t.Fatal(err)
	}
	inDev := &AudioDevice{Name: "in", Direction: "input", SourceType: "soundcard", SampleRate: 48000, Channels: 1, Format: "s16le"}
	if err := s.CreateAudioDevice(inDev); err != nil {
		t.Fatal(err)
	}

	// Input device must have direction=input
	ch := &Channel{
		Name: "bad", InputDeviceID: outDev.ID,
		ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	if err := s.CreateChannel(ch); err == nil {
		t.Fatal("expected error when input_device_id references an output device")
	}

	// Output device must have direction=output
	ch2 := &Channel{
		Name: "bad2", InputDeviceID: inDev.ID, OutputDeviceID: inDev.ID,
		ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	if err := s.CreateChannel(ch2); err == nil {
		t.Fatal("expected error when output_device_id references an input device")
	}

	// RX-only (OutputDeviceID=0) is valid
	ch3 := &Channel{
		Name: "rxonly", InputDeviceID: inDev.ID, OutputDeviceID: 0,
		ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	if err := s.CreateChannel(ch3); err != nil {
		t.Fatalf("rx-only channel should be valid: %v", err)
	}
}

func TestDeleteAudioDeviceChecked_NoRefs(t *testing.T) {
	s := newTestStore(t)
	dev := &AudioDevice{Name: "unused", Direction: "input", SourceType: "soundcard", SourcePath: "hw:0", SampleRate: 48000, Channels: 1, Format: "s16le"}
	if err := s.CreateAudioDevice(dev); err != nil {
		t.Fatal(err)
	}

	deleted, refs, err := s.DeleteAudioDeviceChecked(dev.ID, false)
	if err != nil {
		t.Fatalf("DeleteAudioDeviceChecked: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected no refs, got %+v", refs)
	}
	if len(deleted) != 0 {
		t.Fatalf("expected no cascaded channels, got %+v", deleted)
	}
	if _, err := s.GetAudioDevice(dev.ID); err == nil {
		t.Fatal("expected device to be gone")
	}
}

func TestDeleteAudioDeviceChecked_RefsRefusesWithoutCascade(t *testing.T) {
	s := newTestStore(t)
	inDev := &AudioDevice{Name: "mic", Direction: "input", SourceType: "soundcard", SourcePath: "hw:0", SampleRate: 48000, Channels: 1, Format: "s16le"}
	if err := s.CreateAudioDevice(inDev); err != nil {
		t.Fatal(err)
	}
	ch := &Channel{Name: "ch1", InputDeviceID: inDev.ID, ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200, Profile: "A", NumSlicers: 1}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatal(err)
	}

	deleted, refs, err := s.DeleteAudioDeviceChecked(inDev.ID, false)
	if err != nil {
		t.Fatalf("DeleteAudioDeviceChecked: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("expected nothing deleted when refusing, got %+v", deleted)
	}
	if len(refs) != 1 || refs[0].ID != ch.ID {
		t.Fatalf("expected refs=[ch1], got %+v", refs)
	}
	// Device and channel must still exist.
	if _, err := s.GetAudioDevice(inDev.ID); err != nil {
		t.Fatalf("device should still exist: %v", err)
	}
	if _, err := s.GetChannel(ch.ID); err != nil {
		t.Fatalf("channel should still exist: %v", err)
	}
}

func TestDeleteAudioDeviceChecked_CascadeDeletesRefs(t *testing.T) {
	s := newTestStore(t)
	inDev := &AudioDevice{Name: "mic", Direction: "input", SourceType: "soundcard", SourcePath: "hw:0", SampleRate: 48000, Channels: 1, Format: "s16le"}
	if err := s.CreateAudioDevice(inDev); err != nil {
		t.Fatal(err)
	}
	outDev := &AudioDevice{Name: "spk", Direction: "output", SourceType: "soundcard", SourcePath: "hw:1", SampleRate: 48000, Channels: 1, Format: "s16le"}
	if err := s.CreateAudioDevice(outDev); err != nil {
		t.Fatal(err)
	}
	ch1 := &Channel{Name: "ch1", InputDeviceID: inDev.ID, ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200, Profile: "A", NumSlicers: 1}
	ch2 := &Channel{Name: "ch2", InputDeviceID: inDev.ID, OutputDeviceID: outDev.ID, ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200, Profile: "A", NumSlicers: 1}
	if err := s.CreateChannel(ch1); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateChannel(ch2); err != nil {
		t.Fatal(err)
	}

	deleted, refs, err := s.DeleteAudioDeviceChecked(inDev.ID, true)
	if err != nil {
		t.Fatalf("DeleteAudioDeviceChecked: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected no refs returned when cascading, got %+v", refs)
	}
	if len(deleted) != 2 {
		t.Fatalf("expected 2 cascaded channels, got %+v", deleted)
	}
	if _, err := s.GetAudioDevice(inDev.ID); err == nil {
		t.Fatal("expected input device to be gone")
	}
	remaining, _ := s.ListChannels()
	if len(remaining) != 0 {
		t.Fatalf("expected 0 channels remaining, got %d", len(remaining))
	}
	// Output device is untouched.
	if _, err := s.GetAudioDevice(outDev.ID); err != nil {
		t.Fatalf("output device should still exist: %v", err)
	}
}

func TestFX25IL2PConfig(t *testing.T) {
	s := newTestStore(t)
	dev := &AudioDevice{Name: "d", Direction: "input", SourceType: "flac", SourcePath: "x.flac", SampleRate: 44100, Channels: 1, Format: "s16le"}
	if err := s.CreateAudioDevice(dev); err != nil {
		t.Fatal(err)
	}
	ch := &Channel{
		Name: "rx0", InputDeviceID: dev.ID,
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
