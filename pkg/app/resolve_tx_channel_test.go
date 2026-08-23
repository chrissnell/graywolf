package app

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// TestResolveTxChannel exercises the fallback truth table in
// (*App).resolveTxChannel: configured-with-modem returns configured;
// configured-without-modem falls back to the lowest modem-backed
// channel; configured-not-present falls back the same way; no
// modem-backed channel returns the lowest channel ID overall and logs
// the dedicated "tx will fail at submit" warning; an empty channel
// list returns the configured value unchanged.
func TestResolveTxChannel(t *testing.T) {
	ctx := context.Background()

	// channelSpec describes a row to seed. modem=true binds an audio
	// input device (via U32Ptr); modem=false leaves InputDeviceID nil
	// to model a KISS-only or otherwise unbacked channel.
	type channelSpec struct {
		name  string
		modem bool
	}

	mkStore := func(t *testing.T, specs []channelSpec) (*configstore.Store, []uint32) {
		t.Helper()
		s, err := configstore.OpenMemory()
		if err != nil {
			t.Fatalf("OpenMemory: %v", err)
		}
		t.Cleanup(func() { _ = s.Close() })

		dev := &configstore.AudioDevice{
			Name: "dev", Direction: "input", SourceType: "flac",
			SourcePath: "/tmp/x.flac", SampleRate: 44100, Channels: 1, Format: "s16le",
		}
		if err := s.CreateAudioDevice(ctx, dev); err != nil {
			t.Fatalf("CreateAudioDevice: %v", err)
		}

		ids := make([]uint32, 0, len(specs))
		for _, sp := range specs {
			ch := &configstore.Channel{
				Name:      sp.name,
				ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
				Profile: "A", NumSlicers: 1, FixBits: "none",
			}
			if sp.modem {
				ch.InputDeviceID = configstore.U32Ptr(dev.ID)
			}
			if err := s.CreateChannel(ctx, ch); err != nil {
				t.Fatalf("CreateChannel %q: %v", sp.name, err)
			}
			ids = append(ids, ch.ID)
		}
		return s, ids
	}

	cases := []struct {
		name             string
		specs            []channelSpec
		configuredIdx    int // index into ids; -1 → configured=0
		wantIdx          int // -1 → expect configured value verbatim (empty-list case)
		wantWarnContains string
	}{
		{
			name:          "configured channel has modem returns configured",
			specs:         []channelSpec{{"a", true}, {"b", true}},
			configuredIdx: 1,
			wantIdx:       1,
		},
		{
			name:             "configured channel without modem falls back to lowest with modem",
			specs:            []channelSpec{{"a", true}, {"b", false}},
			configuredIdx:    1,
			wantIdx:          0,
			wantWarnContains: "no modem backend",
		},
		{
			name:             "configured channel id absent falls back to lowest with modem",
			specs:            []channelSpec{{"a", true}, {"b", true}},
			configuredIdx:    -1, // configured=0; resolver does not match any row
			wantIdx:          0,
			wantWarnContains: "",
		},
		{
			name:             "no channel has modem returns lowest id and warns",
			specs:            []channelSpec{{"a", false}, {"b", false}},
			configuredIdx:    1,
			wantIdx:          0,
			wantWarnContains: "tx will fail at submit",
		},
		{
			name:          "empty channel list returns configured unchanged",
			specs:         nil,
			configuredIdx: -1, // no ids; expect configured value (0) returned verbatim
			wantIdx:       -1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, ids := mkStore(t, tc.specs)

			var configured uint32
			if tc.configuredIdx >= 0 {
				configured = ids[tc.configuredIdx]
			}

			var logBuf bytes.Buffer
			a := &App{
				store:  s,
				logger: slog.New(slog.NewTextHandler(io.Writer(&logBuf), nil)),
			}

			got := a.resolveTxChannel(ctx, configured)

			var want uint32
			switch {
			case tc.wantIdx == -1:
				want = configured
			default:
				want = ids[tc.wantIdx]
			}
			if got != want {
				t.Fatalf("got channel %d, want %d", got, want)
			}

			if tc.wantWarnContains != "" {
				if !strings.Contains(logBuf.String(), tc.wantWarnContains) {
					t.Fatalf("expected warn containing %q, got logs:\n%s",
						tc.wantWarnContains, logBuf.String())
				}
			} else if strings.Contains(logBuf.String(), "level=WARN") {
				t.Fatalf("unexpected warn for happy path: %s", logBuf.String())
			}
		})
	}
}

// TestResolveTxChannelKissTnc is the regression guard for graywolf#503:
// with both an internal APRS modem channel and a KISS-TNC channel
// configured, a message directed to the KISS channel must resolve to
// that channel — the earlier modem-only resolver overrode it to the
// modem channel, so KISS traffic was logged but egressed on the wrong
// interface. A KISS-TNC channel has no InputDeviceID; it earns a
// governor TX backend via an enabled tnc-mode interface with
// AllowTxFromGovernor.
func TestResolveTxChannelKissTnc(t *testing.T) {
	ctx := context.Background()

	newStore := func(t *testing.T) *configstore.Store {
		t.Helper()
		s, err := configstore.OpenMemory()
		if err != nil {
			t.Fatalf("OpenMemory: %v", err)
		}
		t.Cleanup(func() { _ = s.Close() })
		return s
	}
	// mkChannel creates a channel; modem=true binds an audio input device.
	mkChannel := func(t *testing.T, s *configstore.Store, name string, modem bool) uint32 {
		t.Helper()
		ch := &configstore.Channel{
			Name:      name,
			ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
			Profile: "A", NumSlicers: 1, FixBits: "none",
		}
		if modem {
			dev := &configstore.AudioDevice{
				Name: name + "-dev", Direction: "input", SourceType: "flac",
				SourcePath: "/tmp/x.flac", SampleRate: 44100, Channels: 1, Format: "s16le",
			}
			if err := s.CreateAudioDevice(ctx, dev); err != nil {
				t.Fatalf("CreateAudioDevice: %v", err)
			}
			ch.InputDeviceID = configstore.U32Ptr(dev.ID)
		}
		if err := s.CreateChannel(ctx, ch); err != nil {
			t.Fatalf("CreateChannel %q: %v", name, err)
		}
		return ch.ID
	}
	mkKissTnc := func(t *testing.T, s *configstore.Store, name string, channel uint32, allowTx bool) {
		t.Helper()
		ki := &configstore.KissInterface{
			Name:                name,
			InterfaceType:       "tcp",
			ListenAddr:          ":8001",
			Channel:             channel,
			Enabled:             true,
			Mode:                configstore.KissModeTnc,
			AllowTxFromGovernor: allowTx,
		}
		if err := s.CreateKissInterface(ctx, ki); err != nil {
			t.Fatalf("CreateKissInterface %q: %v", name, err)
		}
	}

	newApp := func(s *configstore.Store) (*App, *bytes.Buffer) {
		buf := &bytes.Buffer{}
		return &App{
			store:  s,
			logger: slog.New(slog.NewTextHandler(io.Writer(buf), nil)),
		}, buf
	}

	t.Run("modem plus kiss configured to kiss returns kiss", func(t *testing.T) {
		s := newStore(t)
		mkChannel(t, s, "modem", true)
		kissCh := mkChannel(t, s, "kiss", false)
		mkKissTnc(t, s, "vara", kissCh, true)

		a, buf := newApp(s)
		if got := a.resolveTxChannel(ctx, kissCh); got != kissCh {
			t.Fatalf("got channel %d, want kiss channel %d", got, kissCh)
		}
		if strings.Contains(buf.String(), "level=WARN") {
			t.Fatalf("unexpected warn honoring configured kiss channel: %s", buf.String())
		}
	})

	t.Run("modem plus kiss configured to modem returns modem", func(t *testing.T) {
		s := newStore(t)
		modemCh := mkChannel(t, s, "modem", true)
		kissCh := mkChannel(t, s, "kiss", false)
		mkKissTnc(t, s, "vara", kissCh, true)

		a, _ := newApp(s)
		if got := a.resolveTxChannel(ctx, modemCh); got != modemCh {
			t.Fatalf("got channel %d, want modem channel %d", got, modemCh)
		}
	})

	t.Run("kiss only configured to zero falls back to kiss", func(t *testing.T) {
		s := newStore(t)
		kissCh := mkChannel(t, s, "kiss", false)
		mkKissTnc(t, s, "vara", kissCh, true)

		a, _ := newApp(s)
		if got := a.resolveTxChannel(ctx, 0); got != kissCh {
			t.Fatalf("got channel %d, want kiss channel %d", got, kissCh)
		}
	})

	t.Run("no modem configured to unbacked falls back to kiss and warns", func(t *testing.T) {
		s := newStore(t)
		// A channel with no modem and no governor backend (dead), plus a
		// separate KISS-TNC egress channel. Configuring the dead channel
		// must fall back to the KISS channel and log the override.
		deadCh := mkChannel(t, s, "dead", false)
		kissCh := mkChannel(t, s, "kiss", false)
		mkKissTnc(t, s, "vara", kissCh, true)

		a, buf := newApp(s)
		if got := a.resolveTxChannel(ctx, deadCh); got != kissCh {
			t.Fatalf("got channel %d, want kiss channel %d", got, kissCh)
		}
		if !strings.Contains(buf.String(), "no tx backend") {
			t.Fatalf("expected override warn, got: %s", buf.String())
		}
	})

	t.Run("tnc without allow_tx is not an egress target", func(t *testing.T) {
		s := newStore(t)
		modemCh := mkChannel(t, s, "modem", true)
		kissCh := mkChannel(t, s, "kiss", false)
		mkKissTnc(t, s, "rx-only", kissCh, false)

		a, buf := newApp(s)
		// Configured KISS channel has no TX backend (AllowTxFromGovernor
		// false), so it is overridden to the modem channel as before.
		if got := a.resolveTxChannel(ctx, kissCh); got != modemCh {
			t.Fatalf("got channel %d, want modem channel %d", got, modemCh)
		}
		if !strings.Contains(buf.String(), "no modem backend") {
			t.Fatalf("expected override warn, got: %s", buf.String())
		}
	})
}

// TestResolveTxChannelDisabled is the regression guard for graywolf#517:
// a disabled channel is fully inert and must never be selected as an
// egress target, whether it is modem-backed or KISS-TNC-backed, and
// whether it is the explicitly-configured channel or would only be
// reached as a fallback. Toggling a channel back on restores it.
func TestResolveTxChannelDisabled(t *testing.T) {
	ctx := context.Background()

	newStore := func(t *testing.T) *configstore.Store {
		t.Helper()
		s, err := configstore.OpenMemory()
		if err != nil {
			t.Fatalf("OpenMemory: %v", err)
		}
		t.Cleanup(func() { _ = s.Close() })
		return s
	}
	mkChannel := func(t *testing.T, s *configstore.Store, name string, modem, enabled bool) uint32 {
		t.Helper()
		ch := &configstore.Channel{
			Name:      name,
			ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
			Profile: "A", NumSlicers: 1, FixBits: "none",
		}
		if modem {
			dev := &configstore.AudioDevice{
				Name: name + "-dev", Direction: "input", SourceType: "flac",
				SourcePath: "/tmp/x.flac", SampleRate: 44100, Channels: 1, Format: "s16le",
			}
			if err := s.CreateAudioDevice(ctx, dev); err != nil {
				t.Fatalf("CreateAudioDevice: %v", err)
			}
			ch.InputDeviceID = configstore.U32Ptr(dev.ID)
		}
		if err := s.CreateChannel(ctx, ch); err != nil {
			t.Fatalf("CreateChannel %q: %v", name, err)
		}
		// The store creates every channel enabled; disable via the same
		// path the REST toggle uses when the case wants a disabled row.
		if !enabled {
			if err := s.SetChannelEnabled(ctx, ch.ID, false); err != nil {
				t.Fatalf("SetChannelEnabled %q: %v", name, err)
			}
		}
		return ch.ID
	}
	mkKissTnc := func(t *testing.T, s *configstore.Store, name string, channel uint32) {
		t.Helper()
		ki := &configstore.KissInterface{
			Name:          name,
			InterfaceType: "tcp",
			ListenAddr:    ":8001",
			Channel:       channel,
			Enabled:       true,
			Mode:          configstore.KissModeTnc,
			// AllowTxFromGovernor makes the KISS channel a governor egress
			// target — the disabled-channel filter is what must suppress it.
			AllowTxFromGovernor: true,
		}
		if err := s.CreateKissInterface(ctx, ki); err != nil {
			t.Fatalf("CreateKissInterface %q: %v", name, err)
		}
	}
	newApp := func(s *configstore.Store) (*App, *bytes.Buffer) {
		buf := &bytes.Buffer{}
		return &App{
			store:  s,
			logger: slog.New(slog.NewTextHandler(io.Writer(buf), nil)),
		}, buf
	}

	t.Run("disabled modem channel is skipped as fallback", func(t *testing.T) {
		s := newStore(t)
		disabled := mkChannel(t, s, "disabled", true, false)
		enabled := mkChannel(t, s, "enabled", true, true)

		a, _ := newApp(s)
		// Configured=0 must fall back to the enabled modem channel, never
		// the lower-id disabled one.
		if got := a.resolveTxChannel(ctx, 0); got != enabled {
			t.Fatalf("got channel %d, want enabled channel %d (disabled=%d)", got, enabled, disabled)
		}
	})

	t.Run("configured disabled modem channel falls back to enabled", func(t *testing.T) {
		s := newStore(t)
		enabled := mkChannel(t, s, "enabled", true, true)
		disabled := mkChannel(t, s, "disabled", true, false)

		a, buf := newApp(s)
		if got := a.resolveTxChannel(ctx, disabled); got != enabled {
			t.Fatalf("got channel %d, want enabled channel %d", got, enabled)
		}
		if !strings.Contains(buf.String(), "no modem backend") {
			t.Fatalf("expected override warn, got: %s", buf.String())
		}
	})

	t.Run("disabled kiss-tnc channel is not an egress target", func(t *testing.T) {
		s := newStore(t)
		modemCh := mkChannel(t, s, "modem", true, true)
		kissCh := mkChannel(t, s, "kiss", false, false)
		mkKissTnc(t, s, "vara", kissCh)

		a, buf := newApp(s)
		// Even though the KISS interface is enabled and allows governor TX,
		// its channel is disabled, so a message configured to it must be
		// overridden to the modem channel — not honored like #503.
		if got := a.resolveTxChannel(ctx, kissCh); got != modemCh {
			t.Fatalf("got channel %d, want modem channel %d", got, modemCh)
		}
		if !strings.Contains(buf.String(), "no modem backend") {
			t.Fatalf("expected override warn, got: %s", buf.String())
		}
	})

	t.Run("re-enabling a channel restores it as an egress target", func(t *testing.T) {
		s := newStore(t)
		modemCh := mkChannel(t, s, "modem", true, true)
		kissCh := mkChannel(t, s, "kiss", false, false)
		mkKissTnc(t, s, "vara", kissCh)

		a, _ := newApp(s)
		if got := a.resolveTxChannel(ctx, kissCh); got != modemCh {
			t.Fatalf("disabled: got %d, want modem %d", got, modemCh)
		}
		if err := s.SetChannelEnabled(ctx, kissCh, true); err != nil {
			t.Fatalf("SetChannelEnabled: %v", err)
		}
		// Now the KISS channel is a valid egress target again (#503 rule).
		if got := a.resolveTxChannel(ctx, kissCh); got != kissCh {
			t.Fatalf("re-enabled: got %d, want kiss %d", got, kissCh)
		}
	})
}
