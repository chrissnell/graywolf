package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// Fresh DB has no UnitsConfig row yet. GET returns 200 with
// system="imperial" per the singleton-on-missing contract.
func TestGetUnitsConfig_DefaultImperial(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/preferences/units", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.UnitsConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.System != "imperial" {
		t.Errorf("System = %q, want %q (default)", resp.System, "imperial")
	}
}

// Round-trip: PUT metric, expect response echoes the stored value
// and a follow-up GET returns system=metric. Pins that the preference
// actually survives in the store so reloading the UI picks it up.
func TestPutUnitsConfig_Persists(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"system":"metric"}`
	req := httptest.NewRequest(http.MethodPut, "/api/preferences/units", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.UnitsConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.System != "metric" {
		t.Errorf("PUT response System = %q, want %q", resp.System, "metric")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/preferences/units", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("follow-up GET: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got dto.UnitsConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.System != "metric" {
		t.Errorf("GET after PUT: System = %q, want %q", got.System, "metric")
	}

	c, err := srv.store.GetUnitsConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.System != "metric" {
		t.Errorf("store.GetUnitsConfig.System = %q, want %q", c.System, "metric")
	}
}

// Unknown system value is a 400, not a 500. A single PUT with a junk
// value must not corrupt the row — the store validates too, but we
// want the DTO-level gate to fire first for a clean error body.
func TestPutUnitsConfig_UnknownSystemReturns400(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"system":"parsecs"}`
	req := httptest.NewRequest(http.MethodPut, "/api/preferences/units", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Malformed JSON in the PUT body surfaces as 400.
func TestPutUnitsConfig_MalformedBodyReturns400(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPut, "/api/preferences/units", strings.NewReader("{bogus"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
