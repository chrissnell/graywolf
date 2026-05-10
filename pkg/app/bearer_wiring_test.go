package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBearerWiring_GatesApiPath: API paths require bearer when token set.
func TestBearerWiring_GatesApiPath(t *testing.T) {
	cfg := Config{BearerToken: "abc"}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	h := wrapWithBearerIfSet(cfg, inner)
	req := httptest.NewRequest("GET", "/api/x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("want 401 (no auth header) got %d", rec.Code)
	}
}

// TestBearerWiring_PassesSPAStaticPaths: SPA static surface must
// remain unauthenticated even when token is set, so the WebView's
// first navigation can load the SPA shell that then installs the
// bearer-injecting fetch wrapper.
func TestBearerWiring_PassesSPAStaticPaths(t *testing.T) {
	cfg := Config{BearerToken: "abc"}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	h := wrapWithBearerIfSet(cfg, inner)
	for _, path := range []string{"/", "/index.html", "/assets/index.js", "/favicon.ico"} {
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("path %q: want 200 got %d", path, rec.Code)
		}
	}
}

// TestBearerWiring_GatesWSPath: /ws/ also requires bearer (or
// ?token= query for upgrade requests, handled by the middleware).
func TestBearerWiring_GatesWSPath(t *testing.T) {
	cfg := Config{BearerToken: "abc"}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	h := wrapWithBearerIfSet(cfg, inner)
	req := httptest.NewRequest("GET", "/ws/feed", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("want 401 got %d", rec.Code)
	}
}

func TestBearerWiring_NoOpWhenEmpty(t *testing.T) {
	cfg := Config{BearerToken: ""}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	h := wrapWithBearerIfSet(cfg, inner)
	req := httptest.NewRequest("GET", "/api/x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("want 200 (no middleware) got %d", rec.Code)
	}
}
