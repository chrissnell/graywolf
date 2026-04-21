package webapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/modembridge"
)

func newTestServer(t *testing.T) (*Server, *modembridge.Bridge) {
	t.Helper()
	ctx := context.Background()
	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	dev := &configstore.AudioDevice{
		Name: "test", Direction: "input", SourceType: "flac", SourcePath: "/tmp/x.flac",
		SampleRate: 44100, Channels: 1, Format: "s16le",
	}
	if err := store.CreateAudioDevice(ctx, dev); err != nil {
		t.Fatal(err)
	}
	// Seed channel is TX-capable (both input + output audio device
	// configured) so the webapi tests that create beacons / iGate / digi
	// referrers against it pass the Phase-1 TX-capability gate. A
	// separate output device is required because validateChannel
	// enforces device direction=output on OutputDeviceID.
	outDev := &configstore.AudioDevice{
		Name: "test-out", Direction: "output", SourceType: "null", SourcePath: "/tmp/out.raw",
		SampleRate: 44100, Channels: 1, Format: "s16le",
	}
	if err := store.CreateAudioDevice(ctx, outDev); err != nil {
		t.Fatal(err)
	}
	ch := &configstore.Channel{
		Name: "rx0", InputDeviceID: configstore.U32Ptr(dev.ID), OutputDeviceID: outDev.ID,
		ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	if err := store.CreateChannel(ctx, ch); err != nil {
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
	// The test fixture seeds two devices: "test" (input) and "test-out"
	// (output) so the seed channel can be TX-capable for beacon /
	// iGate / digipeater referrer tests.
	if len(devices) != 2 || devices[0].Name != "test" || devices[1].Name != "test-out" {
		t.Errorf("unexpected devices: %+v", devices)
	}
}

// TestDeleteAudioDeviceHandler exercises the four spec'd outcomes of
// DELETE /api/audio-devices/{id}: no refs → 200, refs + no cascade → 409
// with channel list, refs + cascade=true → 200 with deleted list, and
// any store-side error → 500 with the generic sanitized body.
func TestDeleteAudioDeviceHandler(t *testing.T) {
	t.Run("no refs returns 200", func(t *testing.T) {
		srv, _ := newTestServer(t)
		mux := http.NewServeMux()
		srv.RegisterRoutes(mux)

		// Add a second, unreferenced device. The fixture device is
		// already referenced by rx0 — use a fresh one here.
		dev := &configstore.AudioDevice{
			Name: "unused", Direction: "input", SourceType: "flac",
			SourcePath: "/tmp/unused.flac", SampleRate: 44100, Channels: 1, Format: "s16le",
		}
		if err := srv.store.CreateAudioDevice(context.Background(), dev); err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodDelete, "/api/audio-devices/"+strconv.FormatUint(uint64(dev.ID), 10), nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Deleted []configstore.Channel `json:"deleted"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Deleted) != 0 {
			t.Errorf("expected no cascaded deletions, got %+v", body.Deleted)
		}
	})

	t.Run("refs without cascade returns 409", func(t *testing.T) {
		srv, _ := newTestServer(t)
		mux := http.NewServeMux()
		srv.RegisterRoutes(mux)

		// The fixture device id=1 is referenced by rx0.
		req := httptest.NewRequest(http.MethodDelete, "/api/audio-devices/1", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Error    string                `json:"error"`
			Channels []configstore.Channel `json:"channels"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Channels) != 1 || body.Channels[0].Name != "rx0" {
			t.Errorf("expected channels=[rx0], got %+v", body.Channels)
		}
		// Device must still exist — 409 means nothing was deleted.
		if _, err := srv.store.GetAudioDevice(context.Background(), 1); err != nil {
			t.Errorf("device should still exist after 409: %v", err)
		}
	})

	t.Run("refs with cascade returns 200 and deleted list", func(t *testing.T) {
		srv, _ := newTestServer(t)
		mux := http.NewServeMux()
		srv.RegisterRoutes(mux)

		req := httptest.NewRequest(http.MethodDelete, "/api/audio-devices/1?cascade=true", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Deleted []configstore.Channel `json:"deleted"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Deleted) != 1 || body.Deleted[0].Name != "rx0" {
			t.Errorf("expected deleted=[rx0], got %+v", body.Deleted)
		}
		if _, err := srv.store.GetAudioDevice(context.Background(), 1); err == nil {
			t.Error("expected device to be gone after cascade")
		}
	})

	t.Run("store error returns sanitized 500", func(t *testing.T) {
		srv, _ := newTestServer(t)
		mux := http.NewServeMux()
		srv.RegisterRoutes(mux)

		// Close the underlying DB so the next store call fails. This
		// exercises the sanitized 500 path without having to introduce
		// an interface shim.
		if err := srv.store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}

		req := httptest.NewRequest(http.MethodDelete, "/api/audio-devices/1", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
		}
		var body map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["error"] != "internal error" {
			t.Errorf("expected sanitized error, got %q", body["error"])
		}
	})
}
