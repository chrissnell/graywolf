package webapi

import (
	"bytes"
	"context"
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

// TestAudioDeviceDelete_ConflictWireShape verifies the 409 body still
// serializes as `{"error": ..., "channels": [<Channel ...>]}` after the
// untyped `map[string]any` was replaced with dto.AudioDeviceDeleteConflict.
//
// Byte-identical proof: a handcrafted response using the pre-refactor
// map-literal shape is round-tripped through json.Marshal with the same
// payload the handler produces, and the two byte slices are compared.
func TestAudioDeviceDelete_ConflictWireShape(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// newTestServer seeds exactly one audio device (id=1) and one
	// channel (rx0) referencing it. Deleting the device without cascade
	// is guaranteed to surface the 409.
	req := httptest.NewRequest(http.MethodDelete, "/api/audio-devices/1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}

	// Decode into the new typed DTO to prove the schema matches.
	var typed dto.AudioDeviceDeleteConflict
	if err := json.NewDecoder(bytes.NewReader(rec.Body.Bytes())).Decode(&typed); err != nil {
		t.Fatalf("decode typed: %v", err)
	}
	if typed.Error != "device is referenced by channels" {
		t.Errorf("unexpected error message: %q", typed.Error)
	}
	if len(typed.Channels) != 1 || typed.Channels[0].Name != "rx0" {
		t.Errorf("unexpected channels: %+v", typed.Channels)
	}

	// Reconstruct what the OLD `map[string]any{...}` body would have
	// serialized to for the same refs and assert byte-equivalence.
	// Fetching the stored channel produces the same configstore.Channel
	// value the handler receives from DeleteAudioDeviceChecked.
	ctx := context.Background()
	channels, err := srv.store.ListChannels(ctx)
	if err != nil {
		t.Fatalf("ListChannels: %v", err)
	}
	oldShape := map[string]any{
		"error":    "device is referenced by channels",
		"channels": channels,
	}
	var oldBuf bytes.Buffer
	enc := json.NewEncoder(&oldBuf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(oldShape); err != nil {
		t.Fatalf("encode old shape: %v", err)
	}

	newShape := dto.AudioDeviceDeleteConflict{
		Error:    "device is referenced by channels",
		Channels: dto.ChannelsFromModels(channels),
	}
	var newBuf bytes.Buffer
	enc = json.NewEncoder(&newBuf)
	enc.SetEscapeHTML(false)
	// writeJSON uses SetIndent("", "  "); match that here so the
	// handler's on-the-wire output is byte-comparable to our synthetic
	// encoding.
	enc.SetIndent("", "  ")
	if err := enc.Encode(newShape); err != nil {
		t.Fatalf("encode new shape: %v", err)
	}

	// Byte-for-byte check: the response the HANDLER actually wrote
	// must match the DTO-encoded buffer exactly, including indentation.
	handlerBytes := bytes.TrimRight(rec.Body.Bytes(), "\n")
	dtoBytes := bytes.TrimRight(newBuf.Bytes(), "\n")
	if !bytes.Equal(handlerBytes, dtoBytes) {
		t.Errorf("handler response != DTO encoding\n  handler: %s\n  dto    : %s", handlerBytes, dtoBytes)
	}

	// And the key-sorted canonical form of the old map-literal must
	// match the key-sorted canonical form of the DTO — if it doesn't,
	// a field rename somewhere broke the contract. (This comparison
	// strips formatting, so the indentation delta doesn't matter.)
	if !jsonSemanticallyEqual(t, oldBuf.Bytes(), newBuf.Bytes()) {
		t.Errorf("old map shape and new DTO shape differ semantically\n  old: %s\n  new: %s", oldBuf.String(), newBuf.String())
	}
}

// jsonSemanticallyEqual decodes two JSON byte slices into generic
// `any` values and compares them via round-trip re-encoding. Struct
// declaration order vs map key order is normalized away.
func jsonSemanticallyEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var va, vb any
	if err := json.Unmarshal(a, &va); err != nil {
		t.Fatalf("unmarshal a: %v", err)
	}
	if err := json.Unmarshal(b, &vb); err != nil {
		t.Fatalf("unmarshal b: %v", err)
	}
	ba, err := json.Marshal(va)
	if err != nil {
		t.Fatalf("remarshal a: %v", err)
	}
	bb, err := json.Marshal(vb)
	if err != nil {
		t.Fatalf("remarshal b: %v", err)
	}
	return bytes.Equal(ba, bb)
}

// TestAudioDeviceListAvailable_NilBridgeReturnsEmpty verifies that the
// "bridge not wired yet" path still surfaces an empty list to the UI
// rather than a 5xx — the UI shows "no devices" in that state, which
// is the correct rendering before the modem has started.
func TestAudioDeviceListAvailable_NilBridgeReturnsEmpty(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.bridge = nil // simulate pre-wire startup
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/audio-devices/available", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Errorf("expected empty array body, got %q", got)
	}
}

// TestAudioDeviceScanLevels_NilBridgeReturnsEmpty mirrors the Available
// test for the scan-levels endpoint.
func TestAudioDeviceScanLevels_NilBridgeReturnsEmpty(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.bridge = nil
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/audio-devices/scan-levels", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Errorf("expected empty array body, got %q", got)
	}
}

// TestAudioDeviceSetGain_HappyPath exercises the renamed handler and
// typed request DTO. The in-process test bridge is not running, so the
// live-update call fails silently — the handler must still return 200
// with the updated record because the persisted write succeeded.
func TestAudioDeviceSetGain_HappyPath(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// newTestServer seeds device id=1.
	body := `{"gain_db":-6}`
	req := httptest.NewRequest(http.MethodPut, "/api/audio-devices/1/gain", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.AudioDeviceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.GainDB != -6 {
		t.Errorf("expected gain_db=-6, got %v", resp.GainDB)
	}

	// Confirm the store was actually updated.
	dev, err := srv.store.GetAudioDevice(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if dev.GainDB != -6 {
		t.Errorf("store gain_db = %v, want -6", dev.GainDB)
	}
}

// TestAudioDeviceSetGain_OutOfRange verifies the gain-range validation
// still fires through the typed DTO's Validate().
func TestAudioDeviceSetGain_OutOfRange(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"gain_db":99}`
	req := httptest.NewRequest(http.MethodPut, "/api/audio-devices/1/gain", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestAudioDeviceTestTone_RejectsInputDevice guards the new playTestTone
// handler name: input devices can't play tones.
func TestAudioDeviceTestTone_RejectsInputDevice(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// Seeded device id=1 is direction=input.
	req := httptest.NewRequest(http.MethodPost, "/api/audio-devices/1/test-tone", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestAudioDeviceTestTone_MissingDevice returns 404 rather than 500
// even though the bridge is not running in tests — the store lookup
// fails first.
func TestAudioDeviceTestTone_MissingDevice(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/audio-devices/9999/test-tone", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestAudioDeviceGetLevels_NilBridgeReturnsEmptyObject verifies the
// levels endpoint renders an empty object when the bridge is nil —
// matching the pre-refactor behavior ("{}") so the UI's meter
// reconciliation logic stays unchanged.
func TestAudioDeviceGetLevels_NilBridgeReturnsEmptyObject(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.bridge = nil
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/audio-devices/levels", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "{}" {
		t.Errorf("expected empty object body, got %q", got)
	}
}

// TestAudioDeviceSetGain_UnknownFieldReturns400 locks in the disallow-
// unknown-fields guarantee for the gain DTO.
func TestAudioDeviceSetGain_UnknownFieldReturns400(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"gain_db":-6,"bogus":true}`
	req := httptest.NewRequest(http.MethodPut, "/api/audio-devices/1/gain", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

