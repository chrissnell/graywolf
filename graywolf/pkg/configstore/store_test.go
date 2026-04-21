package configstore

import (
	"context"
	"strings"
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
		InputDeviceID: U32Ptr(dev.ID),
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
		Name: "bad", InputDeviceID: U32Ptr(999), ModemType: "afsk",
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
		Name: "bad", InputDeviceID: U32Ptr(dev.ID), InputChannel: 1, // mono device, channel 1 is out of range
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
			Name: "ch", InputDeviceID: U32Ptr(dev.ID), InputChannel: ac,
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
		Name: "bad", InputDeviceID: U32Ptr(outDev.ID),
		ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	if err := s.CreateChannel(ctx, ch); err == nil {
		t.Fatal("expected error when input_device_id references an output device")
	}

	// Output device must have direction=output
	ch2 := &Channel{
		Name: "bad2", InputDeviceID: U32Ptr(inDev.ID), OutputDeviceID: inDev.ID,
		ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	if err := s.CreateChannel(ctx, ch2); err == nil {
		t.Fatal("expected error when output_device_id references an input device")
	}

	// RX-only (OutputDeviceID=0) is valid
	ch3 := &Channel{
		Name: "rxonly", InputDeviceID: U32Ptr(inDev.ID), OutputDeviceID: 0,
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
	ch := &Channel{Name: "ch1", InputDeviceID: U32Ptr(inDev.ID), ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200, Profile: "A", NumSlicers: 1}
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
	ch1 := &Channel{Name: "ch1", InputDeviceID: U32Ptr(inDev.ID), ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200, Profile: "A", NumSlicers: 1}
	ch2 := &Channel{Name: "ch2", InputDeviceID: U32Ptr(inDev.ID), OutputDeviceID: outDev.ID, ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200, Profile: "A", NumSlicers: 1}
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
		Name: "rx0", InputDeviceID: U32Ptr(dev.ID),
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

// TestCreateKissInterfaceModeDefaulting confirms the store-boundary
// defense: an empty Mode is upgraded to KissModeModem, zero rate fields
// are replaced with the documented defaults, and invalid Mode values
// are rejected with a wrapped error before the row ever reaches SQLite.
func TestCreateKissInterfaceModeDefaulting(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	t.Run("empty mode defaults to modem", func(t *testing.T) {
		ki := &KissInterface{Name: "kiss-default", InterfaceType: "tcp", ListenAddr: "127.0.0.1:8101", Channel: 1}
		if err := s.CreateKissInterface(ctx, ki); err != nil {
			t.Fatalf("create: %v", err)
		}
		got, err := s.GetKissInterface(ctx, ki.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.Mode != KissModeModem {
			t.Errorf("Mode = %q, want %q", got.Mode, KissModeModem)
		}
		if got.TncIngressRateHz != 50 || got.TncIngressBurst != 100 {
			t.Errorf("rate defaults not applied: rate=%d burst=%d", got.TncIngressRateHz, got.TncIngressBurst)
		}
	})

	t.Run("explicit tnc mode round-trips", func(t *testing.T) {
		ki := &KissInterface{
			Name: "kiss-tnc", InterfaceType: "tcp", ListenAddr: "127.0.0.1:8102", Channel: 1,
			Mode: KissModeTnc, TncIngressRateHz: 25, TncIngressBurst: 75,
		}
		if err := s.CreateKissInterface(ctx, ki); err != nil {
			t.Fatalf("create: %v", err)
		}
		got, err := s.GetKissInterface(ctx, ki.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.Mode != KissModeTnc || got.TncIngressRateHz != 25 || got.TncIngressBurst != 75 {
			t.Errorf("round-trip lost fields: %+v", got)
		}
	})

	t.Run("invalid mode is rejected", func(t *testing.T) {
		ki := &KissInterface{Name: "kiss-bad", InterfaceType: "tcp", ListenAddr: "127.0.0.1:8103", Channel: 1, Mode: "bogus"}
		err := s.CreateKissInterface(ctx, ki)
		if err == nil {
			t.Fatal("expected error for invalid mode")
		}
	})

	t.Run("update applies same defaults", func(t *testing.T) {
		ki := &KissInterface{Name: "kiss-upd", InterfaceType: "tcp", ListenAddr: "127.0.0.1:8104", Channel: 1, Mode: KissModeTnc}
		if err := s.CreateKissInterface(ctx, ki); err != nil {
			t.Fatalf("create: %v", err)
		}
		// Clear the rate fields and clear Mode to simulate a legacy update.
		ki.Mode = ""
		ki.TncIngressRateHz = 0
		ki.TncIngressBurst = 0
		if err := s.UpdateKissInterface(ctx, ki); err != nil {
			t.Fatalf("update: %v", err)
		}
		got, err := s.GetKissInterface(ctx, ki.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.Mode != KissModeModem || got.TncIngressRateHz != 50 || got.TncIngressBurst != 100 {
			t.Errorf("update did not re-apply defaults: %+v", got)
		}
	})
}

// TestChannel_NullableInputDeviceRoundTrip verifies that Phase 2's
// *uint32 InputDeviceID survives Create + Read + Update without
// getting coerced to zero.
func TestChannel_NullableInputDeviceRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	t.Run("nil input: kiss-only create and read", func(t *testing.T) {
		ch := &Channel{Name: "kiss-only", InputDeviceID: nil, ModemType: "afsk",
			BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200, Profile: "A", NumSlicers: 1, FixBits: "none"}
		if err := s.CreateChannel(ctx, ch); err != nil {
			t.Fatalf("CreateChannel(kiss-only): %v", err)
		}
		got, err := s.GetChannel(ctx, ch.ID)
		if err != nil {
			t.Fatalf("GetChannel: %v", err)
		}
		if got.InputDeviceID != nil {
			t.Errorf("expected nil InputDeviceID, got %v", got.InputDeviceID)
		}
	})

	t.Run("kiss-only rejects non-zero output device", func(t *testing.T) {
		ch := &Channel{Name: "bad", InputDeviceID: nil, OutputDeviceID: 5,
			ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
			Profile: "A", NumSlicers: 1, FixBits: "none"}
		if err := s.CreateChannel(ctx, ch); err == nil {
			t.Fatal("expected error for kiss-only channel with OutputDeviceID != 0")
		}
	})

	t.Run("explicit modem channel round-trips", func(t *testing.T) {
		dev := &AudioDevice{Name: "mic", Direction: "input", SourceType: "soundcard", SourcePath: "hw:0",
			SampleRate: 48000, Channels: 1, Format: "s16le"}
		if err := s.CreateAudioDevice(ctx, dev); err != nil {
			t.Fatal(err)
		}
		ch := &Channel{Name: "modem", InputDeviceID: U32Ptr(dev.ID), ModemType: "afsk",
			BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200, Profile: "A", NumSlicers: 1, FixBits: "none"}
		if err := s.CreateChannel(ctx, ch); err != nil {
			t.Fatalf("CreateChannel(modem): %v", err)
		}
		got, err := s.GetChannel(ctx, ch.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.InputDeviceID == nil || *got.InputDeviceID != dev.ID {
			t.Errorf("expected *uint32(%d), got %v", dev.ID, got.InputDeviceID)
		}
	})

	t.Run("modem channel can be converted to kiss-only", func(t *testing.T) {
		dev := &AudioDevice{Name: "mic2", Direction: "input", SourceType: "soundcard", SourcePath: "hw:1",
			SampleRate: 48000, Channels: 1, Format: "s16le"}
		if err := s.CreateAudioDevice(ctx, dev); err != nil {
			t.Fatal(err)
		}
		ch := &Channel{Name: "convertible", InputDeviceID: U32Ptr(dev.ID), ModemType: "afsk",
			BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200, Profile: "A", NumSlicers: 1, FixBits: "none"}
		if err := s.CreateChannel(ctx, ch); err != nil {
			t.Fatal(err)
		}
		ch.InputDeviceID = nil
		ch.OutputDeviceID = 0
		if err := s.UpdateChannel(ctx, ch); err != nil {
			t.Fatalf("UpdateChannel(convert to kiss-only): %v", err)
		}
		got, err := s.GetChannel(ctx, ch.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.InputDeviceID != nil {
			t.Errorf("after convert, expected nil InputDeviceID, got %v", got.InputDeviceID)
		}
	})
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

// TestChannelKissInterfaceMutualExclusivity exercises the Phase 3 D3
// rule: a modem-backed channel cannot also have a KISS-TNC interface
// with AllowTxFromGovernor=true attached to it (dual-backend would
// double-transmit every frame).
func TestChannelKissInterfaceMutualExclusivity(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Set up an input audio device + modem-backed channel + TNC-mode
	// KISS interface NOT (yet) opted into TX. This is the baseline —
	// the validator should accept it.
	dev := &AudioDevice{Name: "Mic", Direction: "input", SourceType: "soundcard", Channels: 1}
	if err := s.CreateAudioDevice(ctx, dev); err != nil {
		t.Fatalf("create device: %v", err)
	}
	ch := &Channel{
		Name:          "rf-vhf",
		InputDeviceID: U32Ptr(dev.ID),
		ModemType:     "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	if err := s.CreateChannel(ctx, ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	ki := &KissInterface{
		Name: "tnc-passive", InterfaceType: "tcp", ListenAddr: "127.0.0.1:8201",
		Channel: ch.ID, Mode: KissModeTnc, AllowTxFromGovernor: false,
	}
	if err := s.CreateKissInterface(ctx, ki); err != nil {
		t.Fatalf("create passive tnc: %v", err)
	}

	// 1. Flipping AllowTxFromGovernor on while the channel has a
	// bound input device must be rejected.
	t.Run("update kiss_interface to allow tx on modem channel is rejected", func(t *testing.T) {
		ki.AllowTxFromGovernor = true
		err := s.UpdateKissInterface(ctx, ki)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "audio input device") {
			t.Errorf("err=%v, want mention of audio input device", err)
		}
		// Revert so the other subtests start clean.
		ki.AllowTxFromGovernor = false
		if err := s.UpdateKissInterface(ctx, ki); err != nil {
			t.Fatalf("revert: %v", err)
		}
	})

	// 2. Creating a fresh TNC interface with allow_tx_from_governor
	// targeting the modem channel is rejected.
	t.Run("create kiss_interface with allow tx on modem channel is rejected", func(t *testing.T) {
		dup := &KissInterface{
			Name: "tnc-active", InterfaceType: "tcp", ListenAddr: "127.0.0.1:8202",
			Channel: ch.ID, Mode: KissModeTnc, AllowTxFromGovernor: true,
		}
		err := s.CreateKissInterface(ctx, dup)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	// 3. Opposite direction: start with a KISS-only channel + TNC
	// interface with allow tx, then try to add an input audio device
	// to the channel.
	t.Run("attach audio input to channel with active tnc is rejected", func(t *testing.T) {
		kissOnlyCh := &Channel{
			Name: "kiss-only", InputDeviceID: nil,
			ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
			Profile: "A", NumSlicers: 1, FixBits: "none",
		}
		if err := s.CreateChannel(ctx, kissOnlyCh); err != nil {
			t.Fatalf("create kiss-only channel: %v", err)
		}
		activeKi := &KissInterface{
			Name: "tnc-live", InterfaceType: "tcp", ListenAddr: "127.0.0.1:8203",
			Channel: kissOnlyCh.ID, Mode: KissModeTnc, AllowTxFromGovernor: true,
		}
		if err := s.CreateKissInterface(ctx, activeKi); err != nil {
			t.Fatalf("create active tnc on kiss-only channel: %v", err)
		}

		// Now add an input device → expect rejection.
		kissOnlyCh.InputDeviceID = U32Ptr(dev.ID)
		err := s.UpdateChannel(ctx, kissOnlyCh)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "allow_tx_from_governor") {
			t.Errorf("err=%v, want mention of allow_tx_from_governor", err)
		}
	})

	// 4. Passive TNC (AllowTxFromGovernor=false) on a modem-backed
	// channel is always fine — this is pure RX.
	t.Run("passive tnc on modem channel is always accepted", func(t *testing.T) {
		passive := &KissInterface{
			Name: "tnc-rx", InterfaceType: "tcp", ListenAddr: "127.0.0.1:8204",
			Channel: ch.ID, Mode: KissModeTnc, AllowTxFromGovernor: false,
		}
		if err := s.CreateKissInterface(ctx, passive); err != nil {
			t.Fatalf("create passive tnc: %v", err)
		}
	})
}
