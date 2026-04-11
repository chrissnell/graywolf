package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// TestAudioDeviceCreate_HappyPath uses the DTO contract and asserts id
// assignment + field mapping.
func TestAudioDeviceCreate_HappyPath(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{
		"name":"new-dev",
		"direction":"output",
		"source_type":"soundcard",
		"device_path":"hw:0,0",
		"sample_rate":48000,
		"channels":1,
		"format":"s16le",
		"gain_db":0
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/audio-devices", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.AudioDeviceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID == 0 || resp.Name != "new-dev" || resp.Direction != "output" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestAudioDeviceCreate_InvalidDirectionReturns400(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"name":"bad","direction":"sideways","source_type":"soundcard","sample_rate":48000,"channels":1,"format":"s16le"}`
	req := httptest.NewRequest(http.MethodPost, "/api/audio-devices", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAudioDeviceCreate_UnknownFieldReturns400(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"name":"bad","direction":"input","source_type":"soundcard","sample_rate":48000,"channels":1,"format":"s16le","extra":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/audio-devices", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAudioDeviceCreate_GainOutOfRange(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"name":"x","direction":"input","source_type":"soundcard","sample_rate":48000,"channels":1,"format":"s16le","gain_db":50}`
	req := httptest.NewRequest(http.MethodPost, "/api/audio-devices", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
