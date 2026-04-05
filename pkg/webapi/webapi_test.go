package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

func newTestStore(t *testing.T) *configstore.Store {
	t.Helper()
	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestChannelsEndpoint(t *testing.T) {
	store := newTestStore(t)
	// Seed an audio device + channel so the response isn't empty.
	ad := &configstore.AudioDevice{Name: "mic", SourceType: "soundcard", SourcePath: "default"}
	if err := store.CreateAudioDevice(ad); err != nil {
		t.Fatal(err)
	}
	ch := &configstore.Channel{Name: "144.39", AudioDeviceID: ad.ID, ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200, Profile: "A"}
	if err := store.CreateChannel(ch); err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(Config{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/channels", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	var out []ChannelDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Name != "144.39" {
		t.Errorf("got %+v", out)
	}
}

func TestBeaconsEndpoint(t *testing.T) {
	store := newTestStore(t)
	srv, _ := NewServer(Config{Store: store})
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/beacons", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	var out []BeaconDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty, got %+v", out)
	}
}

func TestHealthEndpoint(t *testing.T) {
	store := newTestStore(t)
	srv, _ := NewServer(Config{Store: store})
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/health", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
}
