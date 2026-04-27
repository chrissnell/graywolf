package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/mapscache"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// newTestServerWithCache wires a fresh mapscache.Manager into a test
// server. Returns the server, the manager, and the upstream stub the
// manager will fetch from. Callers swap in their own upstream by
// passing a HandlerFunc.
func newTestServerWithCache(t *testing.T, upstream http.Handler) (*Server, *mapscache.Manager, *httptest.Server) {
	t.Helper()
	srv, _ := newTestServer(t)
	up := httptest.NewServer(upstream)
	t.Cleanup(up.Close)
	cacheDir := t.TempDir()
	mgr := mapscache.New(cacheDir, srv.store, func(context.Context) string { return "test-token" }, up.URL, 2)
	srv.mapsCache = mgr
	return srv, mgr, up
}

// waitFor polls fn at 30ms intervals until it returns true or the
// deadline passes. Returns true on success, false on timeout. Used
// to ride out the async download goroutine without baking sleeps
// into individual tests.
func waitFor(t *testing.T, timeout time.Duration, fn func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(30 * time.Millisecond)
	}
	return false
}

// mux + ServeHTTP helper to keep tests short.
func newDownloadsMux(srv *Server) *http.ServeMux {
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	return mux
}

func TestListMapsDownloads_Empty(t *testing.T) {
	srv, _, _ := newTestServerWithCache(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	mux := newDownloadsMux(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/maps/downloads", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var out []dto.DownloadStatus
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty list, got %+v", out)
	}
}

func TestGetMapsDownloadStatus_AbsentSlug(t *testing.T) {
	srv, _, _ := newTestServerWithCache(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	mux := newDownloadsMux(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/maps/downloads/georgia", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (absent is a valid state), got %d: %s", rec.Code, rec.Body.String())
	}
	var out dto.DownloadStatus
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.State != "absent" {
		t.Errorf("expected state=absent, got %q", out.State)
	}
	if out.Slug != "georgia" {
		t.Errorf("expected slug=georgia, got %q", out.Slug)
	}
}

func TestGetMapsDownloadStatus_InvalidSlug(t *testing.T) {
	srv, _, _ := newTestServerWithCache(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	mux := newDownloadsMux(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/maps/downloads/xxx", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body["error"], "unknown state slug") {
		t.Errorf("expected 'unknown state slug', got %q", body["error"])
	}
}

func TestStartMapsDownload_Lifecycle(t *testing.T) {
	body := strings.Repeat("Y", 64*1024)
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "65536")
		w.Header().Set("Content-Type", "application/vnd.pmtiles")
		w.WriteHeader(http.StatusOK)
		// Slow-drip writes so a GET can observe state="downloading"
		// before completion.
		for i := 0; i < 8; i++ {
			_, _ = w.Write([]byte(body[i*8192 : (i+1)*8192]))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(15 * time.Millisecond)
		}
	})
	srv, mgr, _ := newTestServerWithCache(t, upstream)
	mux := newDownloadsMux(srv)

	// POST starts the download — must return 202 with a status payload.
	req := httptest.NewRequest(http.MethodPost, "/api/maps/downloads/georgia", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	var started dto.DownloadStatus
	if err := json.NewDecoder(rec.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	if started.Slug != "georgia" {
		t.Errorf("expected slug=georgia, got %q", started.Slug)
	}
	if started.State != "downloading" {
		t.Errorf("expected state=downloading, got %q", started.State)
	}

	// Wait for completion via the GET endpoint.
	completed := waitFor(t, 5*time.Second, func() bool {
		st, _ := mgr.Status(context.Background(), "georgia")
		return st.State == "complete"
	})
	if !completed {
		t.Fatalf("download did not complete in time")
	}

	// Now GET via HTTP and confirm the same.
	req = httptest.NewRequest(http.MethodGet, "/api/maps/downloads/georgia", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var final dto.DownloadStatus
	if err := json.NewDecoder(rec.Body).Decode(&final); err != nil {
		t.Fatal(err)
	}
	if final.State != "complete" {
		t.Errorf("expected state=complete, got %q", final.State)
	}
	if final.BytesTotal != 65536 || final.BytesDownloaded != 65536 {
		t.Errorf("expected 65536 bytes, got total=%d done=%d", final.BytesTotal, final.BytesDownloaded)
	}
}

func TestStartMapsDownload_AlreadyInflight(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the request is canceled — keeps the download
		// goroutine inflight long enough for the second POST to race
		// the in-memory map check.
		<-r.Context().Done()
	})
	srv, mgr, _ := newTestServerWithCache(t, upstream)
	mux := newDownloadsMux(srv)

	req := httptest.NewRequest(http.MethodPost, "/api/maps/downloads/texas", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("first POST: expected 202, got %d", rec.Code)
	}

	// Second POST while the first is still inflight should be 409.
	req = httptest.NewRequest(http.MethodPost, "/api/maps/downloads/texas", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second POST: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "already_inflight" {
		t.Errorf("expected error=already_inflight, got %q", body["error"])
	}
	if body["message"] == "" {
		t.Errorf("expected non-empty message, got %q", body["message"])
	}

	// Cleanup so the goroutine doesn't keep the upstream server alive
	// past test cleanup.
	_ = mgr.Delete(context.Background(), "texas")
}

func TestDeleteMapsDownload_Idempotent(t *testing.T) {
	body := strings.Repeat("Z", 4096)
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "4096")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})
	srv, mgr, _ := newTestServerWithCache(t, upstream)
	mux := newDownloadsMux(srv)

	// Drive a download to completion so DELETE has both a row and a
	// file to remove.
	if err := mgr.Start(context.Background(), "ohio"); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 3*time.Second, func() bool {
		st, _ := mgr.Status(context.Background(), "ohio")
		return st.State == "complete"
	}) {
		t.Fatal("setup: download did not complete")
	}

	// Sanity: file exists before the delete.
	if _, err := os.Stat(mgr.PathFor("ohio")); err != nil {
		t.Fatalf("expected file to exist before delete: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/maps/downloads/ohio", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// File gone, GET reports absent.
	if _, err := os.Stat(mgr.PathFor("ohio")); !os.IsNotExist(err) {
		t.Fatalf("file should be gone after delete: %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/maps/downloads/ohio", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var st dto.DownloadStatus
	if err := json.NewDecoder(rec.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if st.State != "absent" {
		t.Errorf("expected absent after delete, got %q", st.State)
	}

	// Second DELETE is a no-op (idempotent).
	req = httptest.NewRequest(http.MethodDelete, "/api/maps/downloads/ohio", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("idempotent DELETE: expected 204, got %d", rec.Code)
	}
}

func TestServeTilesPMTiles_AbsentReturns404(t *testing.T) {
	srv, _, _ := newTestServerWithCache(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/tiles/florida.pmtiles", nil)
	req.SetPathValue("slug", "florida")
	rec := httptest.NewRecorder()
	srv.ServeTilesPMTiles(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestServeTilesPMTiles_ServesBytes(t *testing.T) {
	srv, mgr, _ := newTestServerWithCache(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	// Stage a file directly so the test doesn't depend on an upstream
	// download.
	want := []byte("PMTILES_FAKE_BODY_0123456789ABCDEF")
	if err := os.WriteFile(mgr.PathFor("nevada"), want, 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/tiles/nevada.pmtiles", nil)
	req.SetPathValue("slug", "nevada")
	rec := httptest.NewRecorder()
	srv.ServeTilesPMTiles(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/vnd.pmtiles" {
		t.Errorf("Content-Type = %q, want application/vnd.pmtiles", got)
	}
	gotBody, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotBody) != string(want) {
		t.Errorf("body mismatch: got %q want %q", gotBody, want)
	}
}

func TestServeTilesPMTiles_RangeRequest(t *testing.T) {
	srv, mgr, _ := newTestServerWithCache(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	body := []byte("0123456789ABCDEFGHIJ") // 20 bytes
	if err := os.WriteFile(mgr.PathFor("vermont"), body, 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/tiles/vermont.pmtiles", nil)
	req.SetPathValue("slug", "vermont")
	req.Header.Set("Range", "bytes=0-15")
	rec := httptest.NewRecorder()
	srv.ServeTilesPMTiles(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d", rec.Code)
	}
	got, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	// HTTP Range "bytes=0-15" requests a 16-byte slice (inclusive).
	if len(got) != 16 {
		t.Errorf("expected 16 bytes, got %d", len(got))
	}
	if string(got) != string(body[:16]) {
		t.Errorf("range body = %q, want %q", got, body[:16])
	}
}

// 503 path: handlers must guard against a nil mapsCache. Ensures the
// wiring sequence (P2-T5) can defer Manager construction without
// crashing handlers that fire in the meantime.
func TestDownloads_NilCacheReturns503(t *testing.T) {
	srv, _ := newTestServer(t)
	// mapsCache is intentionally nil.
	mux := newDownloadsMux(srv)

	for _, route := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/maps/downloads"},
		{http.MethodGet, "/api/maps/downloads/georgia"},
		{http.MethodPost, "/api/maps/downloads/georgia"},
		{http.MethodDelete, "/api/maps/downloads/georgia"},
	} {
		req := httptest.NewRequest(route.method, route.path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: expected 503, got %d: %s", route.method, route.path, rec.Code, rec.Body.String())
		}
	}
}

// Sanity that the regex pre-check rejects unicode before the closed
// list does. Belt-and-suspenders coverage.
func TestDownloads_UnicodeSlugRejected(t *testing.T) {
	srv, _, _ := newTestServerWithCache(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	mux := newDownloadsMux(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/maps/downloads/g%C3%A9orgia", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// Compile-time check that ErrAlreadyInflight is still wired into
// mapscache.Manager — the handler depends on errors.Is for the 409
// branch.
var _ = errors.Is(mapscache.ErrAlreadyInflight, mapscache.ErrAlreadyInflight)
