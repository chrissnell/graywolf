package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// ---------------------------------------------------------------------------
// GET /api/station/config
// ---------------------------------------------------------------------------

// Fresh DB — no StationConfig row yet. GET returns 200 with empty
// callsign and omits the Disabled field from the JSON envelope.
func TestGetStationConfig_EmptyStoreReturnsEmptyCallsign(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/station/config", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// Parse as a generic map so we can confirm the absence of
	// `disabled` on the read path, not just its emptiness.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v (body %q)", err, rec.Body.String())
	}
	if _, present := raw["disabled"]; present {
		t.Errorf("GET response should omit `disabled`, got body %q", rec.Body.String())
	}
	var resp dto.StationConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Callsign != "" {
		t.Errorf("Callsign = %q, want empty", resp.Callsign)
	}
}

// GET after a PUT returns the stored callsign, normalized.
func TestGetStationConfig_ReturnsStoredValue(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	if err := srv.store.UpsertStationConfig(context.Background(),
		configstore.StationConfig{Callsign: "ke7xyz-9"}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/station/config", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.StationConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Callsign != "KE7XYZ-9" {
		t.Errorf("Callsign = %q, want KE7XYZ-9", resp.Callsign)
	}
}

// ---------------------------------------------------------------------------
// PUT /api/station/config
// ---------------------------------------------------------------------------

// Happy path: PUT lowercase + whitespace, expect normalization on
// write and no `disabled` in the response (neither dependent was
// enabled before the call).
func TestPutStationConfig_NormalizesCallsign(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"callsign":"ke7xyz-9"}`
	req := httptest.NewRequest(http.MethodPut, "/api/station/config", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.StationConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Callsign != "KE7XYZ-9" {
		t.Errorf("Callsign = %q, want KE7XYZ-9", resp.Callsign)
	}
	if len(resp.Disabled) != 0 {
		t.Errorf("Disabled = %v, want empty", resp.Disabled)
	}
	// Persistence round-trip.
	got, err := srv.store.GetStationConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Callsign != "KE7XYZ-9" {
		t.Errorf("stored Callsign = %q, want KE7XYZ-9", got.Callsign)
	}
}

// Clear with both iGate and Digipeater enabled. Both should flip to
// disabled and appear (in canonical order) in the response.
func TestPutStationConfig_EmptyClearsAndAutoDisables(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ctx := context.Background()

	// Seed: station callsign set, iGate + Digi both enabled.
	if err := srv.store.UpsertStationConfig(ctx,
		configstore.StationConfig{Callsign: "KE7XYZ-9"}); err != nil {
		t.Fatal(err)
	}
	if err := srv.store.UpsertIGateConfig(ctx, &configstore.IGateConfig{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := srv.store.UpsertDigipeaterConfig(ctx, &configstore.DigipeaterConfig{Enabled: true}); err != nil {
		t.Fatal(err)
	}

	body := `{"callsign":""}`
	req := httptest.NewRequest(http.MethodPut, "/api/station/config", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.StationConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Callsign != "" {
		t.Errorf("Callsign = %q, want empty", resp.Callsign)
	}
	wantDisabled := []string{"igate", "digipeater"}
	if len(resp.Disabled) != 2 || resp.Disabled[0] != wantDisabled[0] || resp.Disabled[1] != wantDisabled[1] {
		t.Errorf("Disabled = %v, want %v", resp.Disabled, wantDisabled)
	}

	// Post-condition: both dependents actually flipped.
	igCfg, err := srv.store.GetIGateConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if igCfg == nil || igCfg.Enabled {
		t.Errorf("iGate still enabled: %+v", igCfg)
	}
	diCfg, err := srv.store.GetDigipeaterConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if diCfg == nil || diCfg.Enabled {
		t.Errorf("Digipeater still enabled: %+v", diCfg)
	}
}

// Clear when neither dependent was enabled: the response does not
// include a `disabled` key in the JSON envelope.
func TestPutStationConfig_EmptyNoDependentsReturnsNoDisabledKey(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"callsign":""}`
	req := httptest.NewRequest(http.MethodPut, "/api/station/config", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, present := raw["disabled"]; present {
		t.Errorf("expected no `disabled` key, got body %q", rec.Body.String())
	}
}

// Clear with N0CALL (any SSID, any case) behaves identically to
// empty: it triggers the clear + auto-disable path.
func TestPutStationConfig_N0CallAutoDisables(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ctx := context.Background()

	if err := srv.store.UpsertIGateConfig(ctx, &configstore.IGateConfig{Enabled: true}); err != nil {
		t.Fatal(err)
	}

	body := `{"callsign":"N0CALL-7"}`
	req := httptest.NewRequest(http.MethodPut, "/api/station/config", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.StationConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	// The stored value is the normalized "N0CALL-7" — the auto-
	// disable fires because IsN0Call is true, but we don't wipe the
	// stored string (user sees what they typed on the next GET).
	if resp.Callsign != "N0CALL-7" {
		t.Errorf("Callsign = %q, want N0CALL-7", resp.Callsign)
	}
	if len(resp.Disabled) != 1 || resp.Disabled[0] != "igate" {
		t.Errorf("Disabled = %v, want [igate]", resp.Disabled)
	}
}

// ---------------------------------------------------------------------------
// PUT /api/igate/config — enable guard
// ---------------------------------------------------------------------------

func TestPutIGateConfig_EnableWithoutStationReturns400(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// No StationConfig row — expected 400 with the exact unset message.
	body := `{"enabled":true,"server":"rotate.aprs2.net","port":14580,"rf_channel":1,"tx_channel":1,"max_msg_hops":2,"software_name":"graywolf","software_version":"0.1"}`
	req := httptest.NewRequest(http.MethodPut, "/api/igate/config", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] != errStationCallsignUnset {
		t.Errorf("error = %q, want %q", resp["error"], errStationCallsignUnset)
	}
}

func TestPutIGateConfig_EnableWithStationReturns200(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ctx := context.Background()

	if err := srv.store.UpsertStationConfig(ctx,
		configstore.StationConfig{Callsign: "KE7XYZ-9"}); err != nil {
		t.Fatal(err)
	}

	body := `{"enabled":true,"server":"rotate.aprs2.net","port":14580,"rf_channel":1,"tx_channel":1,"max_msg_hops":2,"software_name":"graywolf","software_version":"0.1"}`
	req := httptest.NewRequest(http.MethodPut, "/api/igate/config", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Saving with Enabled=false while the station is unset is always
// allowed — the guard only fires when the incoming request flips to
// Enabled=true.
func TestPutIGateConfig_DisabledSaveWithoutStationReturns200(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"enabled":false,"server":"rotate.aprs2.net","port":14581,"rf_channel":1,"tx_channel":1,"max_msg_hops":2,"software_name":"graywolf","software_version":"0.1"}`
	req := httptest.NewRequest(http.MethodPut, "/api/igate/config", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// The iGate response body must NOT include `callsign` or `passcode`
// keys — the fields are removed from the DTO as of the centralized
// station-callsign plan (D3/D4).
func TestGetIGateConfig_ResponseExcludesCallsignAndPasscode(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/igate/config", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, present := raw["callsign"]; present {
		t.Errorf("iGate response should not include `callsign`, got body %q", rec.Body.String())
	}
	if _, present := raw["passcode"]; present {
		t.Errorf("iGate response should not include `passcode`, got body %q", rec.Body.String())
	}
}

// PUT /api/igate/config must also reject unknown fields like
// `callsign` and `passcode` (the json decoder is strict).
func TestPutIGateConfig_RejectsCallsignField(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"enabled":false,"server":"rotate.aprs2.net","port":14580,"callsign":"W5XYZ-10","rf_channel":1,"tx_channel":1,"max_msg_hops":2,"software_name":"graywolf","software_version":"0.1"}`
	req := httptest.NewRequest(http.MethodPut, "/api/igate/config", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// PUT /api/digipeater — enable guard + override semantics
// ---------------------------------------------------------------------------

func TestPutDigipeaterConfig_EnableWithoutStationReturns400(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"enabled":true,"dedupe_window_seconds":30}`
	req := httptest.NewRequest(http.MethodPut, "/api/digipeater", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] != errStationCallsignUnset {
		t.Errorf("error = %q, want %q", resp["error"], errStationCallsignUnset)
	}
}

func TestPutDigipeaterConfig_EnableWithStationReturns200(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ctx := context.Background()

	if err := srv.store.UpsertStationConfig(ctx,
		configstore.StationConfig{Callsign: "KE7XYZ-9"}); err != nil {
		t.Fatal(err)
	}

	body := `{"enabled":true,"dedupe_window_seconds":30}`
	req := httptest.NewRequest(http.MethodPut, "/api/digipeater", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPutDigipeaterConfig_DisabledSaveWithoutStationReturns200(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"enabled":false,"dedupe_window_seconds":30}`
	req := httptest.NewRequest(http.MethodPut, "/api/digipeater", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// my_call as *string: nil (field omitted) preserves the stored
// value; "" explicitly inherits; non-empty is a verbatim override.
func TestPutDigipeaterConfig_MyCallOverrideSemantics(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ctx := context.Background()

	if err := srv.store.UpsertStationConfig(ctx,
		configstore.StationConfig{Callsign: "KE7XYZ-9"}); err != nil {
		t.Fatal(err)
	}

	// Seed an explicit override.
	if err := srv.store.UpsertDigipeaterConfig(ctx, &configstore.DigipeaterConfig{
		Enabled: true, DedupeWindowSeconds: 30, MyCall: "MTNTOP-1",
	}); err != nil {
		t.Fatal(err)
	}

	// 1. Field omitted (my_call == nil) → MyCall preserved.
	body := `{"enabled":true,"dedupe_window_seconds":30}`
	req := httptest.NewRequest(http.MethodPut, "/api/digipeater", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for omitted my_call, got %d: %s", rec.Code, rec.Body.String())
	}
	cfg, err := srv.store.GetDigipeaterConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || cfg.MyCall != "MTNTOP-1" {
		t.Errorf("nil my_call should preserve stored value; got %+v", cfg)
	}

	// 2. "" → inherit (clears the override).
	body = `{"enabled":true,"dedupe_window_seconds":30,"my_call":""}`
	req = httptest.NewRequest(http.MethodPut, "/api/digipeater", strings.NewReader(body))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for my_call=\"\", got %d: %s", rec.Code, rec.Body.String())
	}
	cfg, err = srv.store.GetDigipeaterConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || cfg.MyCall != "" {
		t.Errorf("my_call=\"\" should clear override; got %+v", cfg)
	}

	// 3. Non-empty → override.
	body = `{"enabled":true,"dedupe_window_seconds":30,"my_call":"MTNTOP-1"}`
	req = httptest.NewRequest(http.MethodPut, "/api/digipeater", strings.NewReader(body))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for my_call override, got %d: %s", rec.Code, rec.Body.String())
	}
	cfg, err = srv.store.GetDigipeaterConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || cfg.MyCall != "MTNTOP-1" {
		t.Errorf("my_call override did not persist; got %+v", cfg)
	}
}

// ---------------------------------------------------------------------------
// Beacon callsign override — create + update
// ---------------------------------------------------------------------------

func TestBeaconCreate_CallsignOmittedInherits(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// No callsign field at all → stored as "".
	body := `{"type":"position","channel":1,"latitude":1,"longitude":2,"interval":1800,"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/beacons", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.BeaconResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Callsign != "" {
		t.Errorf("Callsign = %q, want empty (inherit)", resp.Callsign)
	}
}

func TestBeaconCreate_CallsignEmptyInherits(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"type":"position","channel":1,"callsign":"","latitude":1,"longitude":2,"interval":1800,"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/beacons", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.BeaconResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Callsign != "" {
		t.Errorf("Callsign = %q, want empty (inherit)", resp.Callsign)
	}
}

func TestBeaconCreate_CallsignOverride(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"type":"position","channel":1,"callsign":"MTNTOP-1","latitude":1,"longitude":2,"interval":1800,"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/beacons", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.BeaconResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Callsign != "MTNTOP-1" {
		t.Errorf("Callsign = %q, want MTNTOP-1", resp.Callsign)
	}
}

// PUT with a nil (omitted) callsign field preserves the stored
// override. PUT with "" clears it. PUT with non-empty overrides.
func TestBeaconUpdate_CallsignPointerSemantics(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ctx := context.Background()

	// Seed a beacon with an explicit override.
	b := &configstore.Beacon{
		Type: "position", Channel: 1, Callsign: "MTNTOP-1",
		Latitude: 1, Longitude: 2, EverySeconds: 1800, Enabled: true,
		Path: "WIDE1-1", SymbolTable: "/", Symbol: ">",
	}
	if err := srv.store.CreateBeacon(ctx, b); err != nil {
		t.Fatal(err)
	}

	// 1. Field omitted → stored MyCall preserved.
	body := `{"type":"position","channel":1,"latitude":1,"longitude":2,"path":"WIDE1-1","symbol_table":"/","symbol":">","interval":1800,"enabled":true}`
	req := httptest.NewRequest(http.MethodPut, "/api/beacons/1", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for nil callsign, got %d: %s", rec.Code, rec.Body.String())
	}
	got, err := srv.store.GetBeacon(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Callsign != "MTNTOP-1" {
		t.Errorf("nil callsign should preserve MTNTOP-1; got %q", got.Callsign)
	}

	// 2. "" → clears the override (inherit).
	body = `{"type":"position","channel":1,"callsign":"","latitude":1,"longitude":2,"path":"WIDE1-1","symbol_table":"/","symbol":">","interval":1800,"enabled":true}`
	req = httptest.NewRequest(http.MethodPut, "/api/beacons/1", strings.NewReader(body))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for callsign=\"\", got %d: %s", rec.Code, rec.Body.String())
	}
	got, err = srv.store.GetBeacon(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Callsign != "" {
		t.Errorf("callsign=\"\" should clear override; got %q", got.Callsign)
	}

	// 3. Non-empty → override.
	body = `{"type":"position","channel":1,"callsign":"VANITY-1","latitude":1,"longitude":2,"path":"WIDE1-1","symbol_table":"/","symbol":">","interval":1800,"enabled":true}`
	req = httptest.NewRequest(http.MethodPut, "/api/beacons/1", strings.NewReader(body))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for callsign override, got %d: %s", rec.Code, rec.Body.String())
	}
	got, err = srv.store.GetBeacon(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Callsign != "VANITY-1" {
		t.Errorf("override did not persist; got %q", got.Callsign)
	}
}
