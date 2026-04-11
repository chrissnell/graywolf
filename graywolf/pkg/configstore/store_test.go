package configstore

import (
	"context"
	"testing"
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

func TestSeedDefaults(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Empty DB should seed one device and one channel.
	if err := s.seedDefaults(); err != nil {
		t.Fatalf("seedDefaults: %v", err)
	}
	devs, _ := s.ListAudioDevices(ctx)
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
	chs, _ := s.ListChannels(ctx)
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
	devs2, _ := s.ListAudioDevices(ctx)
	if len(devs2) != 1 {
		t.Fatalf("expected still 1 device after re-seed, got %d", len(devs2))
	}
}

func TestAudioDeviceCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	d := &AudioDevice{
		Name:       "default",
		SourceType: "soundcard",
		SourcePath: "default",
		SampleRate: 48000,
		Channels:   1,
		Format:     "s16le",
	}
	if err := s.CreateAudioDevice(ctx, d); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if d.ID == 0 {
		t.Fatalf("expected autoincrement id")
	}

	got, err := s.GetAudioDevice(ctx, d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "default" || got.SourceType != "soundcard" {
		t.Fatalf("unexpected row: %+v", got)
	}

	got.Name = "renamed"
	if err := s.UpdateAudioDevice(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	list, err := s.ListAudioDevices(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Name != "renamed" {
		t.Fatalf("unexpected list: %+v", list)
	}

	if err := s.DeleteAudioDevice(ctx, got.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.GetAudioDevice(ctx, got.ID); err == nil {
		t.Fatalf("expected error for missing row")
	}
}

func TestChannelAndPtt(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	dev := &AudioDevice{Name: "a", Direction: "input", SourceType: "flac", SourcePath: "x.flac", SampleRate: 44100, Channels: 1, Format: "s16le"}
	if err := s.CreateAudioDevice(ctx, dev); err != nil {
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
	if err := s.CreateChannel(ctx, ch); err != nil {
		t.Fatal(err)
	}
	if ch.ID == 0 {
		t.Fatalf("expected channel id")
	}

	ptt := &PttConfig{ChannelID: ch.ID, Method: "none"}
	if err := s.UpsertPttConfig(ctx, ptt); err != nil {
		t.Fatalf("UpsertPttConfig: %v", err)
	}
	ptt2 := &PttConfig{ChannelID: ch.ID, Method: "gpio", Device: "/dev/gpiochip0", GpioPin: 17}
	if err := s.UpsertPttConfig(ctx, ptt2); err != nil {
		t.Fatalf("Upsert replace: %v", err)
	}
	got, err := s.GetPttConfigForChannel(ctx, ch.ID)
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
	ctx := context.Background()
	s := newTestStore(t)
	ch := &Channel{
		Name: "bad", InputDeviceID: 999, ModemType: "afsk",
		BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	err := s.CreateChannel(ctx, ch)
	if err == nil {
		t.Fatal("expected error for invalid input_device_id")
	}
}

func TestChannelValidation_InputChannelOutOfRange(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	dev := &AudioDevice{Name: "mono", Direction: "input", SourceType: "flac", SourcePath: "x.flac", SampleRate: 44100, Channels: 1, Format: "s16le"}
	if err := s.CreateAudioDevice(ctx, dev); err != nil {
		t.Fatal(err)
	}
	ch := &Channel{
		Name: "bad", InputDeviceID: dev.ID, InputChannel: 1, // mono device, channel 1 is out of range
		ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	err := s.CreateChannel(ctx, ch)
	if err == nil {
		t.Fatal("expected error for input_channel out of range")
	}
}

func TestChannelValidation_StereoDeviceAcceptsBothChannels(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	dev := &AudioDevice{Name: "stereo", Direction: "input", SourceType: "soundcard", SampleRate: 48000, Channels: 2, Format: "s16le"}
	if err := s.CreateAudioDevice(ctx, dev); err != nil {
		t.Fatal(err)
	}
	for _, ac := range []uint32{0, 1} {
		ch := &Channel{
			Name: "ch", InputDeviceID: dev.ID, InputChannel: ac,
			ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
			Profile: "A", NumSlicers: 1, FixBits: "none",
		}
		if err := s.CreateChannel(ctx, ch); err != nil {
			t.Fatalf("input_channel %d should be valid on stereo device: %v", ac, err)
		}
	}
}

func TestChannelValidation_DirectionEnforcement(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	outDev := &AudioDevice{Name: "out", Direction: "output", SourceType: "soundcard", SampleRate: 48000, Channels: 1, Format: "s16le"}
	if err := s.CreateAudioDevice(ctx, outDev); err != nil {
		t.Fatal(err)
	}
	inDev := &AudioDevice{Name: "in", Direction: "input", SourceType: "soundcard", SampleRate: 48000, Channels: 1, Format: "s16le"}
	if err := s.CreateAudioDevice(ctx, inDev); err != nil {
		t.Fatal(err)
	}

	// Input device must have direction=input
	ch := &Channel{
		Name: "bad", InputDeviceID: outDev.ID,
		ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	if err := s.CreateChannel(ctx, ch); err == nil {
		t.Fatal("expected error when input_device_id references an output device")
	}

	// Output device must have direction=output
	ch2 := &Channel{
		Name: "bad2", InputDeviceID: inDev.ID, OutputDeviceID: inDev.ID,
		ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	if err := s.CreateChannel(ctx, ch2); err == nil {
		t.Fatal("expected error when output_device_id references an input device")
	}

	// RX-only (OutputDeviceID=0) is valid
	ch3 := &Channel{
		Name: "rxonly", InputDeviceID: inDev.ID, OutputDeviceID: 0,
		ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	if err := s.CreateChannel(ctx, ch3); err != nil {
		t.Fatalf("rx-only channel should be valid: %v", err)
	}
}

func TestDeleteAudioDeviceChecked_NoRefs(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	dev := &AudioDevice{Name: "unused", Direction: "input", SourceType: "soundcard", SourcePath: "hw:0", SampleRate: 48000, Channels: 1, Format: "s16le"}
	if err := s.CreateAudioDevice(ctx, dev); err != nil {
		t.Fatal(err)
	}

	deleted, refs, err := s.DeleteAudioDeviceChecked(ctx, dev.ID, false)
	if err != nil {
		t.Fatalf("DeleteAudioDeviceChecked: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected no refs, got %+v", refs)
	}
	if len(deleted) != 0 {
		t.Fatalf("expected no cascaded channels, got %+v", deleted)
	}
	if _, err := s.GetAudioDevice(ctx, dev.ID); err == nil {
		t.Fatal("expected device to be gone")
	}
}

func TestDeleteAudioDeviceChecked_RefsRefusesWithoutCascade(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	inDev := &AudioDevice{Name: "mic", Direction: "input", SourceType: "soundcard", SourcePath: "hw:0", SampleRate: 48000, Channels: 1, Format: "s16le"}
	if err := s.CreateAudioDevice(ctx, inDev); err != nil {
		t.Fatal(err)
	}
	ch := &Channel{Name: "ch1", InputDeviceID: inDev.ID, ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200, Profile: "A", NumSlicers: 1}
	if err := s.CreateChannel(ctx, ch); err != nil {
		t.Fatal(err)
	}

	deleted, refs, err := s.DeleteAudioDeviceChecked(ctx, inDev.ID, false)
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
	if _, err := s.GetAudioDevice(ctx, inDev.ID); err != nil {
		t.Fatalf("device should still exist: %v", err)
	}
	if _, err := s.GetChannel(ctx, ch.ID); err != nil {
		t.Fatalf("channel should still exist: %v", err)
	}
}

func TestDeleteAudioDeviceChecked_CascadeDeletesRefs(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	inDev := &AudioDevice{Name: "mic", Direction: "input", SourceType: "soundcard", SourcePath: "hw:0", SampleRate: 48000, Channels: 1, Format: "s16le"}
	if err := s.CreateAudioDevice(ctx, inDev); err != nil {
		t.Fatal(err)
	}
	outDev := &AudioDevice{Name: "spk", Direction: "output", SourceType: "soundcard", SourcePath: "hw:1", SampleRate: 48000, Channels: 1, Format: "s16le"}
	if err := s.CreateAudioDevice(ctx, outDev); err != nil {
		t.Fatal(err)
	}
	ch1 := &Channel{Name: "ch1", InputDeviceID: inDev.ID, ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200, Profile: "A", NumSlicers: 1}
	ch2 := &Channel{Name: "ch2", InputDeviceID: inDev.ID, OutputDeviceID: outDev.ID, ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200, Profile: "A", NumSlicers: 1}
	if err := s.CreateChannel(ctx, ch1); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateChannel(ctx, ch2); err != nil {
		t.Fatal(err)
	}

	deleted, refs, err := s.DeleteAudioDeviceChecked(ctx, inDev.ID, true)
	if err != nil {
		t.Fatalf("DeleteAudioDeviceChecked: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected no refs returned when cascading, got %+v", refs)
	}
	if len(deleted) != 2 {
		t.Fatalf("expected 2 cascaded channels, got %+v", deleted)
	}
	if _, err := s.GetAudioDevice(ctx, inDev.ID); err == nil {
		t.Fatal("expected input device to be gone")
	}
	remaining, _ := s.ListChannels(ctx)
	if len(remaining) != 0 {
		t.Fatalf("expected 0 channels remaining, got %d", len(remaining))
	}
	// Output device is untouched.
	if _, err := s.GetAudioDevice(ctx, outDev.ID); err != nil {
		t.Fatalf("output device should still exist: %v", err)
	}
}

func TestFX25IL2PConfig(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	dev := &AudioDevice{Name: "d", Direction: "input", SourceType: "flac", SourcePath: "x.flac", SampleRate: 44100, Channels: 1, Format: "s16le"}
	if err := s.CreateAudioDevice(ctx, dev); err != nil {
		t.Fatal(err)
	}
	ch := &Channel{
		Name: "rx0", InputDeviceID: dev.ID,
		ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	if err := s.CreateChannel(ctx, ch); err != nil {
		t.Fatal(err)
	}
	if ch.FX25Encode || ch.IL2PEncode {
		t.Fatal("expected defaults to be false")
	}
	if err := s.SetChannelFX25(ctx, ch.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChannelIL2P(ctx, ch.ID, true); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetChannel(ctx, ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.FX25Encode || !got.IL2PEncode {
		t.Fatalf("expected both true, got fx25=%v il2p=%v", got.FX25Encode, got.IL2PEncode)
	}
}

func TestConfigTablesRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := OpenMemory()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	// Exercise every protocol-config table with an Upsert/Create + List/Get.
	if err := s.CreateKissInterface(ctx, &KissInterface{Name: "tcp0", InterfaceType: "tcp", ListenAddr: "127.0.0.1:8001", Channel: 1, Broadcast: true, Enabled: true}); err != nil {
		t.Fatalf("kiss create: %v", err)
	}
	if ks, err := s.ListKissInterfaces(ctx); err != nil || len(ks) != 1 {
		t.Fatalf("list kiss: %v len=%d", err, len(ks))
	}

	if err := s.UpsertAgwConfig(ctx, &AgwConfig{ListenAddr: "0.0.0.0:8000", Callsigns: "N0CALL", Enabled: true}); err != nil {
		t.Fatalf("agw upsert: %v", err)
	}
	if c, err := s.GetAgwConfig(ctx); err != nil || c == nil || c.ListenAddr != "0.0.0.0:8000" {
		t.Fatalf("agw get: %v %+v", err, c)
	}

	if err := s.UpsertTxTiming(ctx, &TxTiming{Channel: 1, TxDelayMs: 250, TxTailMs: 100, SlotMs: 100, Persist: 63}); err != nil {
		t.Fatalf("tx timing upsert: %v", err)
	}
	if err := s.UpsertTxTiming(ctx, &TxTiming{Channel: 1, TxDelayMs: 400, TxTailMs: 100, SlotMs: 100, Persist: 63}); err != nil {
		t.Fatalf("tx timing second upsert: %v", err)
	}
	if ts, err := s.ListTxTimings(ctx); err != nil || len(ts) != 1 || ts[0].TxDelayMs != 400 {
		t.Fatalf("tx list: %v %+v", err, ts)
	}

	if err := s.UpsertDigipeaterConfig(ctx, &DigipeaterConfig{Enabled: true, DedupeWindowSeconds: 30, MyCall: "N0CAL"}); err != nil {
		t.Fatalf("digi cfg: %v", err)
	}
	if err := s.CreateDigipeaterRule(ctx, &DigipeaterRule{FromChannel: 1, ToChannel: 1, Alias: "WIDE", AliasType: "widen", MaxHops: 2, Action: "repeat", Enabled: true}); err != nil {
		t.Fatalf("digi rule: %v", err)
	}
	if rs, err := s.ListDigipeaterRulesForChannel(ctx, 1); err != nil || len(rs) != 1 {
		t.Fatalf("digi rule list: %v len=%d", err, len(rs))
	}

	if err := s.UpsertIGateConfig(ctx, &IGateConfig{Enabled: true, Server: "rotate.aprs2.net", Port: 14580, Callsign: "N0CALL", Passcode: "-1"}); err != nil {
		t.Fatalf("igate cfg: %v", err)
	}
	if err := s.CreateIGateRfFilter(ctx, &IGateRfFilter{Channel: 1, Type: "callsign", Pattern: "KK6*", Action: "allow", Priority: 100, Enabled: true}); err != nil {
		t.Fatalf("igate filter: %v", err)
	}
	if fs, err := s.ListIGateRfFiltersForChannel(ctx, 1); err != nil || len(fs) != 1 {
		t.Fatalf("igate filter list: %v len=%d", err, len(fs))
	}

	if err := s.CreateBeacon(ctx, &Beacon{Type: "position", Channel: 1, Callsign: "N0CAL", Path: "WIDE1-1", Latitude: 40, Longitude: -105, SymbolTable: "/", Symbol: ">", EverySeconds: 1800, Enabled: true}); err != nil {
		t.Fatalf("beacon create: %v", err)
	}
	if bs, err := s.ListBeacons(ctx); err != nil || len(bs) != 1 {
		t.Fatalf("beacon list: %v len=%d", err, len(bs))
	}

	if _, err := s.ListPacketFilters(ctx); err != nil {
		t.Fatalf("packet filter list: %v", err)
	}

	if err := s.UpsertGPSConfig(ctx, &GPSConfig{SourceType: "gpsd", GpsdHost: "localhost", GpsdPort: 2947, Enabled: true}); err != nil {
		t.Fatalf("gps config upsert: %v", err)
	}
	if gc, err := s.GetGPSConfig(ctx); err != nil || gc == nil || gc.SourceType != "gpsd" {
		t.Fatalf("gps config get: %v %+v", err, gc)
	}
}

// TestBeaconUseGpsRoundTrip verifies that the use_gps column survives
// AutoMigrate + Create + Read. Guards against accidental tag drift or a
// dropped column on the Beacon model.
func TestBeaconUseGpsRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := OpenMemory()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	gpsBeacon := &Beacon{
		Type: "position", Channel: 1, Callsign: "N0CAL-1", Path: "WIDE1-1",
		UseGps: true, SymbolTable: "/", Symbol: ">",
		EverySeconds: 1800, Enabled: true,
	}
	if err := s.CreateBeacon(ctx, gpsBeacon); err != nil {
		t.Fatalf("create gps beacon: %v", err)
	}
	fixedBeacon := &Beacon{
		Type: "position", Channel: 1, Callsign: "N0CAL-2", Path: "WIDE1-1",
		Latitude: 37.5, Longitude: -122.0, SymbolTable: "/", Symbol: ">",
		EverySeconds: 1800, Enabled: true,
	}
	if err := s.CreateBeacon(ctx, fixedBeacon); err != nil {
		t.Fatalf("create fixed beacon: %v", err)
	}

	got, err := s.GetBeacon(ctx, gpsBeacon.ID)
	if err != nil {
		t.Fatalf("get gps beacon: %v", err)
	}
	if !got.UseGps {
		t.Errorf("use_gps not persisted: %+v", got)
	}
	got, err = s.GetBeacon(ctx, fixedBeacon.ID)
	if err != nil {
		t.Fatalf("get fixed beacon: %v", err)
	}
	if got.UseGps {
		t.Errorf("use_gps should default to false, got true: %+v", got)
	}
	if got.Latitude != 37.5 || got.Longitude != -122.0 {
		t.Errorf("lat/lon not persisted: %+v", got)
	}
}
