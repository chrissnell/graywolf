package web

import (
	"net/http/httptest"
	"testing"
)

// A missing hashed asset (stale client after a server rebuild) must 404,
// not SPA-fallback to index.html -- serving text/html for a .js request
// is what trips the browser's "disallowed MIME type" module-load error.
func TestSPAHandler_MissingAsset404s(t *testing.T) {
	h := SPAHandler()
	req := httptest.NewRequest("GET", "/assets/Login-doesnotexist.js", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Fatalf("want 404, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct == "text/html; charset=utf-8" {
		t.Fatalf("want non-HTML content type for a missing asset, got %q", ct)
	}
}

// An unknown client-side route (no file on disk, not under /assets/)
// still SPA-falls-back to index.html so svelte-spa-router can render it.
// The ETag is a content hash of index.html, not a caller-supplied
// version, so just assert it's present and stable across requests.
func TestSPAHandler_UnknownRouteFallsBackToIndex(t *testing.T) {
	h := SPAHandler()
	req := httptest.NewRequest("GET", "/some/client/route", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("want a non-empty ETag for index.html")
	}

	req2 := httptest.NewRequest("GET", "/other/client/route", nil)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if got := rec2.Header().Get("ETag"); got != etag {
		t.Fatalf("want stable ETag across requests, got %q then %q", etag, got)
	}
}
