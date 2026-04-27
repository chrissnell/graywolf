package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
	"github.com/chrissnell/graywolf/pkg/webtypes"
)

// ---------------------------------------------------------------------------
// GET /api/smart-beacon
// ---------------------------------------------------------------------------

// TestGetSmartBeacon_EmptyStoreReturnsDefaults pins the "no row yet →
// 200 with DefaultSmartBeacon-sourced defaults" contract documented in
// the Phase 1 handoff.
func TestGetSmartBeacon_EmptyStoreReturnsDefaults(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/smart-beacon", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got dto.SmartBeaconConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	want := dto.SmartBeaconConfigDefaults()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %+v, want %+v", got, want)
	}
}

// TestGetSmartBeacon_AfterUpsertReturnsStored exercises the GET path
// after a prior Upsert so we confirm the model → DTO mapping on the
// read side.
func TestGetSmartBeacon_AfterUpsertReturnsStored(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	stored := &configstore.SmartBeaconConfig{
		Enabled:     true,
		FastSpeedKt: 70,
		FastRateSec: 120,
		SlowSpeedKt: 4,
		SlowRateSec: 1200,
		MinTurnDeg:  25,
		TurnSlope:   200,
		MinTurnSec:  20,
	}
	if err := srv.store.UpsertSmartBeaconConfig(context.Background(), stored); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/smart-beacon", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got dto.SmartBeaconConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	want := dto.SmartBeaconConfigFromModel(*stored)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %+v, want %+v", got, want)
	}
}

// ---------------------------------------------------------------------------
// PUT /api/smart-beacon — validation failures
// ---------------------------------------------------------------------------

// validRequest is the canonical known-good request body used as a base
// for the per-rule invalid-payload tests below. Each sub-test mutates
// exactly one field to violate exactly one rule, so the failure
// attribution is unambiguous.
func validRequest() dto.SmartBeaconConfigRequest {
	return dto.SmartBeaconConfigRequest{
		Enabled:     true,
		FastSpeedKt: 60,
		FastRateSec: 60,
		SlowSpeedKt: 5,
		SlowRateSec: 1800,
		MinTurnDeg:  28,
		TurnSlope:   26,
		MinTurnSec:  30,
	}
}

func putSmartBeacon(t *testing.T, srv *Server, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPut, "/api/smart-beacon", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestUpdateSmartBeacon_ValidationFailures(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*dto.SmartBeaconConfigRequest)
		wantMsg string
	}{
		{
			name:    "slow_speed_zero",
			mutate:  func(r *dto.SmartBeaconConfigRequest) { r.SlowSpeedKt = 0 },
			wantMsg: "slow_speed",
		},
		{
			name:    "fast_speed_not_greater_than_slow",
			mutate:  func(r *dto.SmartBeaconConfigRequest) { r.FastSpeedKt = r.SlowSpeedKt },
			wantMsg: "fast_speed",
		},
		{
			name:    "fast_rate_zero",
			mutate:  func(r *dto.SmartBeaconConfigRequest) { r.FastRateSec = 0 },
			wantMsg: "fast_rate",
		},
		{
			name:    "slow_rate_zero",
			mutate:  func(r *dto.SmartBeaconConfigRequest) { r.SlowRateSec = 0 },
			wantMsg: "slow_rate",
		},
		{
			name:    "fast_rate_not_shorter_than_slow_rate",
			mutate:  func(r *dto.SmartBeaconConfigRequest) { r.FastRateSec = r.SlowRateSec },
			wantMsg: "fast_rate",
		},
		{
			name:    "min_turn_angle_zero",
			mutate:  func(r *dto.SmartBeaconConfigRequest) { r.MinTurnDeg = 0 },
			wantMsg: "min_turn_angle",
		},
		{
			name:    "min_turn_angle_over_limit",
			mutate:  func(r *dto.SmartBeaconConfigRequest) { r.MinTurnDeg = 180 },
			wantMsg: "min_turn_angle",
		},
		{
			name:    "turn_slope_zero",
			mutate:  func(r *dto.SmartBeaconConfigRequest) { r.TurnSlope = 0 },
			wantMsg: "turn_slope",
		},
		{
			name:    "min_turn_time_zero",
			mutate:  func(r *dto.SmartBeaconConfigRequest) { r.MinTurnSec = 0 },
			wantMsg: "min_turn_time",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newTestServer(t)
			reload := make(chan struct{}, 1)
			srv.SetSmartBeaconReload(reload)

			r := validRequest()
			tc.mutate(&r)
			body, _ := json.Marshal(r)

			rec := putSmartBeacon(t, srv, body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
			var errBody webtypes.ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(errBody.Error, tc.wantMsg) {
				t.Errorf("error %q should mention %q", errBody.Error, tc.wantMsg)
			}

			select {
			case <-reload:
				t.Error("reload must not fire on validation failure")
			default:
			}
		})
	}
}

// TestUpdateSmartBeacon_UnknownFieldRejected confirms the strict decode
// contract — unknown body fields produce a 400 with the field name in
// the error message rather than silently dropping them.
func TestUpdateSmartBeacon_UnknownFieldRejected(t *testing.T) {
	srv, _ := newTestServer(t)

	body := []byte(`{"enabled":true,"fast_speed":60,"fast_rate":60,"slow_speed":5,"slow_rate":1800,"min_turn_angle":28,"turn_slope":26,"min_turn_time":30,"bogus_field":"value"}`)
	rec := putSmartBeacon(t, srv, body)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var errBody webtypes.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errBody.Error, "bogus_field") {
		t.Errorf("expected error to name the unknown field, got %q", errBody.Error)
	}
}

// ---------------------------------------------------------------------------
// PUT /api/smart-beacon — happy path and reload signaling
// ---------------------------------------------------------------------------

// TestUpdateSmartBeacon_SignalsReload confirms the wiring contract
// Phase 3 depends on: a successful PUT fires the reload channel
// exactly once.
func TestUpdateSmartBeacon_SignalsReload(t *testing.T) {
	srv, _ := newTestServer(t)

	reload := make(chan struct{}, 1)
	srv.SetSmartBeaconReload(reload)

	body, _ := json.Marshal(validRequest())
	rec := putSmartBeacon(t, srv, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got dto.SmartBeaconConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	want := dto.SmartBeaconConfigFromModel(dto.SmartBeaconConfigToModel(validRequest()))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("response body = %+v, want %+v", got, want)
	}

	select {
	case <-reload:
		// ok
	default:
		t.Fatal("expected reload signal after PUT /api/smart-beacon")
	}

	// Confirm persistence — a follow-up GET should read back the same
	// shape from the store rather than defaults.
	cfg, err := srv.store.GetSmartBeaconConfig(context.Background())
	if err != nil {
		t.Fatalf("get after put: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected a row to exist after PUT")
	}
}

// TestUpdateSmartBeacon_ReloadCoalesces matches the pattern used in
// agw_test.go: prefill the buffered channel so the next non-blocking
// send has nowhere to go. A naïve implementation would deadlock; the
// signalSmartBeaconReload helper must drop the extra signal silently.
func TestUpdateSmartBeacon_ReloadCoalesces(t *testing.T) {
	srv, _ := newTestServer(t)

	reload := make(chan struct{}, 1)
	reload <- struct{}{} // prefill
	srv.SetSmartBeaconReload(reload)

	body, _ := json.Marshal(validRequest())
	rec := putSmartBeacon(t, srv, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// Exactly one signal should be pending — the prefill — regardless
	// of how many PUTs fired.
	select {
	case <-reload:
		// good, drained the prefilled one
	default:
		t.Fatal("expected a pending reload signal")
	}
	select {
	case <-reload:
		t.Fatal("expected coalesced reload, but a second signal was pending")
	default:
	}
}

// ---------------------------------------------------------------------------
// JSON tag parity — catches silent rename drift vs. the UI wire shape
// ---------------------------------------------------------------------------

// wantSmartBeaconJSONTags is the exact, ordered JSON tag set the UI's
// mockSmartBeacon contract uses. Any rename on the struct side breaks
// the Beacons page without any Go-side signal, so this test reflects
// over both Request and Response DTOs and pins the tag order
// byte-for-byte.
var wantSmartBeaconJSONTags = []string{
	"enabled",
	"fast_speed",
	"fast_rate",
	"slow_speed",
	"slow_rate",
	"min_turn_angle",
	"turn_slope",
	"min_turn_time",
}

func TestSmartBeaconJSONTagParity(t *testing.T) {
	cases := []struct {
		name string
		t    reflect.Type
	}{
		{"SmartBeaconConfigRequest", reflect.TypeOf(dto.SmartBeaconConfigRequest{})},
		{"SmartBeaconConfigResponse", reflect.TypeOf(dto.SmartBeaconConfigResponse{})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.t.NumField() != len(wantSmartBeaconJSONTags) {
				t.Fatalf("expected %d fields, got %d",
					len(wantSmartBeaconJSONTags), tc.t.NumField())
			}
			for i := 0; i < tc.t.NumField(); i++ {
				f := tc.t.Field(i)
				got := f.Tag.Get("json")
				// Defensive: strip any future `,omitempty` or similar —
				// none are used today, but this keeps the assertion
				// focused on the tag name.
				if comma := strings.Index(got, ","); comma >= 0 {
					got = got[:comma]
				}
				if got != wantSmartBeaconJSONTags[i] {
					t.Errorf("field[%d] %s: json tag %q, want %q",
						i, f.Name, got, wantSmartBeaconJSONTags[i])
				}
			}
		})
	}
}
