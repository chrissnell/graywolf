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
