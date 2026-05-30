package webapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/mapsstyle"
)

func TestStyleHandler_ServesViaPullThrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/style/americana-roboto/style.json":
			w.Write([]byte(`{"glyphs":"https://maps.nw5w.com/style/roboto-glyphs/{fontstack}/{range}.pbf","sources":{}}`))
		case "/style/americana/sprites/sprite.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("PNGDATA"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	cache := mapsstyle.New(mapsstyle.Config{
		BaseURL:     upstream.URL,
		CacheDir:    t.TempDir(),
		LocalPrefix: "/api/maps/style",
	})

	s := &Server{style: cache}
	mux := http.NewServeMux()
	s.registerStyle(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/maps/style/americana-roboto/style.json", nil).
		WithContext(context.Background())
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("style.json status: %d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type: %s", ct)
	}
	if !strings.Contains(w.Body.String(), `"glyphs":"/api/maps/style/roboto-glyphs/{fontstack}/{range}.pbf"`) {
		t.Errorf("body not rewritten: %s", w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/maps/style/americana/sprites/sprite.png", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("sprite.png status: %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("content-type: %s", ct)
	}
	if w.Body.String() != "PNGDATA" {
		t.Errorf("body: %q", w.Body.String())
	}
}

func TestStyleHandler_Returns502OnUpstreamFailure(t *testing.T) {
	// Closed port => upstream unreachable. Nothing on disk yet.
	cache := mapsstyle.New(mapsstyle.Config{
		BaseURL:     "http://127.0.0.1:1",
		CacheDir:    t.TempDir(),
		LocalPrefix: "/api/maps/style",
	})
	s := &Server{style: cache}
	mux := http.NewServeMux()
	s.registerStyle(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/maps/style/tiles.json", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestStyleHandler_Rejects_PathTraversal(t *testing.T) {
	cache := mapsstyle.New(mapsstyle.Config{
		BaseURL:     "https://x.invalid",
		CacheDir:    t.TempDir(),
		LocalPrefix: "/api/maps/style",
	})
	s := &Server{style: cache}
	mux := http.NewServeMux()
	s.registerStyle(mux)
	// Literal ".." segments are cleaned by net/http.ServeMux to a path
	// outside our route prefix and replied with a 301/307 — the
	// traversal never reaches our handler. Verify the request is
	// either redirected away or 404'd; what matters is no 2xx body.
	req := httptest.NewRequest(http.MethodGet, "/api/maps/style/../../../etc/passwd", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code >= 200 && w.Code < 300 {
		t.Fatalf("traversal returned 2xx: %d body=%s", w.Code, w.Body.String())
	}

	// Percent-encoded ".." is NOT cleaned by the mux, so the request
	// reaches our handler. The mapsstyle.Cache must reject it; our
	// handler maps that rejection to 404.
	req = httptest.NewRequest(http.MethodGet, "/api/maps/style/%2E%2E/%2E%2E/etc/passwd", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("encoded traversal: expected 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestStyleHandler_Returns503WhenCacheNil(t *testing.T) {
	s := &Server{style: nil}
	mux := http.NewServeMux()
	s.registerStyle(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/maps/style/tiles.json", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 503 {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}
