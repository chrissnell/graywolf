package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// Fresh DB has no MapsConfig row. GET returns the singleton default
// of source=osm, registered=false, and (per the suppress-token
// contract) no `token` field in the body.
func TestGetMapsConfig_DefaultsForFreshInstall(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/preferences/maps", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// Decode into a generic map so we can assert on field presence as
	// well as values — the typed DTO uses omitempty for Token so a
	// missing field is indistinguishable from "" after typed decode.
	var raw map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if raw["source"] != "osm" {
		t.Errorf("source = %v, want %q", raw["source"], "osm")
	}
	if raw["registered"] != false {
		t.Errorf("registered = %v, want false", raw["registered"])
	}
	if _, hasToken := raw["token"]; hasToken {
		t.Errorf("token field must be omitted on fresh-install GET; body=%v", raw)
	}
}

// Selecting source=graywolf when no token is stored is a 400 — the
// frontend forces source=osm until registration succeeds, but the
// backend gate is the actual contract.
func TestPutMapsConfig_RejectsGraywolfWithoutToken(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"source":"graywolf"}`
	req := httptest.NewRequest(http.MethodPut, "/api/preferences/maps", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	// Persisted Source must remain the default (osm) — a rejected PUT
	// must not have mutated the row.
	c, err := srv.store.GetMapsConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.Source != "osm" {
		t.Errorf("Source after rejected PUT = %q, want %q", c.Source, "osm")
	}
}

// Happy-path registration: stub auth server returns token, handler
// normalizes the inbound callsign to uppercase + no SSID, persists
// source=graywolf + token, and returns the token in the response
// body (the only place it's ever returned).
func TestRegister_HappyPath_PersistsAndReturnsToken(t *testing.T) {
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/register" {
			t.Errorf("unexpected upstream req: %s %s", r.Method, r.URL.Path)
		}
		// Verify the handler normalized before forwarding.
		var body struct {
			Callsign string `json:"callsign"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		if body.Callsign != "N5XXX" {
			t.Errorf("upstream callsign = %q, want %q (normalized)", body.Callsign, "N5XXX")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"callsign":"N5XXX","token":"tok-abc-123"}`))
	}))
	defer auth.Close()

	srv, _ := newTestServerWithAuth(t, auth.URL)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"callsign":"n5xxx-9"}`
	req := httptest.NewRequest(http.MethodPost, "/api/preferences/maps/register", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var raw map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if raw["callsign"] != "N5XXX" {
		t.Errorf("response callsign = %v, want %q", raw["callsign"], "N5XXX")
	}
	if raw["source"] != "graywolf" {
		t.Errorf("response source = %v, want %q", raw["source"], "graywolf")
	}
	if raw["registered"] != true {
		t.Errorf("response registered = %v, want true", raw["registered"])
	}
	if raw["token"] != "tok-abc-123" {
		t.Errorf("response token = %v, want %q", raw["token"], "tok-abc-123")
	}

	// Persisted state.
	c, err := srv.store.GetMapsConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.Source != "graywolf" || c.Callsign != "N5XXX" || c.Token != "tok-abc-123" {
		t.Errorf("stored config = %+v, want graywolf/N5XXX/tok-abc-123", c)
	}
	if c.RegisteredAt.IsZero() {
		t.Error("RegisteredAt should be set after a successful registration")
	}
}

// Upstream auth-server failures are forwarded verbatim (status, code,
// message) so the UI can display the GH-issues URL the auth server
// embeds in real failure messages. Nothing is persisted on error.
func TestRegister_PropagatesUpstreamError(t *testing.T) {
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"device_limit_reached","message":"Per-callsign device limit reached. File an issue at https://github.com/chrissnell/graywolf/issues"}`))
	}))
	defer auth.Close()

	srv, _ := newTestServerWithAuth(t, auth.URL)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"callsign":"n5xxx"}`
	req := httptest.NewRequest(http.MethodPost, "/api/preferences/maps/register", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] != "device_limit_reached" {
		t.Errorf("error = %q, want %q", resp["error"], "device_limit_reached")
	}
	if !strings.Contains(resp["message"], "github.com/chrissnell/graywolf/issues") {
		t.Errorf("message should reference the issue tracker, got %q", resp["message"])
	}

	// No persisted token after an upstream failure.
	c, err := srv.store.GetMapsConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.Token != "" {
		t.Errorf("Token should remain empty after upstream failure, got %q", c.Token)
	}
}

// ?include_token=1 is the lone way to retrieve the stored token after
// the one-shot register response. The default GET must continue to
// suppress it.
func TestGetMapsConfig_IncludeTokenFlag(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// Seed a registered row directly so we don't need the auth stub.
	if err := srv.store.UpsertMapsConfig(context.Background(), configstore.MapsConfig{
		Source:       "graywolf",
		Callsign:     "N5XXX",
		Token:        "seed-token",
		RegisteredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	// Default GET — token must be omitted.
	req := httptest.NewRequest(http.MethodGet, "/api/preferences/maps", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("default GET: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var noToken map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&noToken); err != nil {
		t.Fatal(err)
	}
	if _, has := noToken["token"]; has {
		t.Errorf("default GET must omit token, body=%v", noToken)
	}
	if noToken["registered"] != true {
		t.Errorf("registered = %v, want true", noToken["registered"])
	}

	// With include_token=1 the field is present.
	req = httptest.NewRequest(http.MethodGet, "/api/preferences/maps?include_token=1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("include_token GET: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var withToken map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&withToken); err != nil {
		t.Fatal(err)
	}
	if withToken["token"] != "seed-token" {
		t.Errorf("token = %v, want %q", withToken["token"], "seed-token")
	}
}
