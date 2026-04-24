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

func TestGetThemeConfig_DefaultGraywolf(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/preferences/theme", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.ThemeConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID != "graywolf" {
		t.Errorf("ID = %q, want %q (default)", resp.ID, "graywolf")
	}
}

func TestPutThemeConfig_Persists(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"id":"chonky"}`
	req := httptest.NewRequest(http.MethodPut, "/api/preferences/theme", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.ThemeConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID != "chonky" {
		t.Errorf("PUT response ID = %q, want %q", resp.ID, "chonky")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/preferences/theme", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("follow-up GET: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got dto.ThemeConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "chonky" {
		t.Errorf("GET after PUT: ID = %q, want %q", got.ID, "chonky")
	}

	c, err := srv.store.GetThemeConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.ThemeID != "chonky" {
		t.Errorf("store.GetThemeConfig.ThemeID = %q, want %q", c.ThemeID, "chonky")
	}
}

func TestPutThemeConfig_AcceptsWellFormedUnknownID(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"id":"field-day-2026"}`
	req := httptest.NewRequest(http.MethodPut, "/api/preferences/theme", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for well-formed unknown id, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPutThemeConfig_MalformedIDReturns400(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	for _, bad := range []string{
		`{"id":""}`,
		`{"id":"UPPERCASE"}`,
		`{"id":"has space"}`,
		`{"id":"path/traversal"}`,
		`{"id":"under_score"}`,
	} {
		req := httptest.NewRequest(http.MethodPut, "/api/preferences/theme", strings.NewReader(bad))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body=%s: expected 400, got %d: %s", bad, rec.Code, rec.Body.String())
		}
	}
}

func TestPutThemeConfig_MalformedBodyReturns400(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPut, "/api/preferences/theme", strings.NewReader("{bogus"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
