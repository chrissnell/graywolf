package webapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/modembridge"
)

func newTestServer(t *testing.T) (*Server, *modembridge.Bridge) {
	t.Helper()
	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	dev := &configstore.AudioDevice{
		Name: "test", Direction: "input", SourceType: "flac", SourcePath: "/tmp/x.flac",
		SampleRate: 44100, Channels: 1, Format: "s16le",
	}
	if err := store.CreateAudioDevice(dev); err != nil {
		t.Fatal(err)
	}
	ch := &configstore.Channel{
		Name: "rx0", InputDeviceID: dev.ID,
		ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	if err := store.CreateChannel(ch); err != nil {
		t.Fatal(err)
	}

	bridge := modembridge.New(modembridge.Config{
		Store:  store,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	srv, err := NewServer(Config{
		Store:  store,
		Bridge: bridge,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv, bridge
}

func TestChannelStatsEndpoint(t *testing.T) {
	srv, bridge := newTestServer(t)
	bridge.InjectStatusForTest(1, 42, 3, 10, 0.5, 0.3, 0.6, true)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/channels/1/stats", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var stats modembridge.ChannelStats
	if err := json.NewDecoder(rec.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
	if stats.RxFrames != 42 || stats.DcdState != true || stats.Channel != 1 {
		t.Errorf("unexpected stats: %+v", stats)
	}
}

func TestChannelStatsNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/channels/99/stats", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestChannelStatsBadPath(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/channels/abc/stats", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestAudioDevicesEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/audio-devices", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var devices []configstore.AudioDevice
	if err := json.NewDecoder(rec.Body).Decode(&devices); err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].Name != "test" {
		t.Errorf("unexpected devices: %+v", devices)
	}
}
