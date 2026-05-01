package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/mapscache"
	"github.com/chrissnell/graywolf/pkg/mapscatalog"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// newTestServerWithCache wires a fresh mapscache.Manager into a test
// server. Returns the server, the manager, and the upstream stub the
// manager will fetch from. Callers swap in their own upstream by
// passing a HandlerFunc. A fake catalog Cache is also wired so the
// resolveSlug membership check passes for the canned slugs that the
// existing tests use plus one country and one province.
func newTestServerWithCache(t *testing.T, upstream http.Handler) (*Server, *mapscache.Manager, *httptest.Server) {
	t.Helper()
	srv, _ := newTestServer(t)
	up := httptest.NewServer(upstream)
	t.Cleanup(up.Close)
	cacheDir := t.TempDir()
	mgr := mapscache.New(cacheDir, srv.store, func(context.Context) string { return "test-token" }, up.URL, 2)
	srv.mapsCache = mgr
	srv.catalog = fakeCatalogCache(t)
	return srv, mgr, up
}

// fakeCatalogCache stands up a tiny catalog Cache pointed at an
// httptest server returning a fixed catalog. Slugs covered:
//
//	state/georgia, state/texas, state/ohio, state/florida,
//	state/nevada, state/vermont, state/colorado
//	country/de
//	province/ca/british-columbia
func fakeCatalogCache(t *testing.T) *mapscatalog.Cache {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(mapscatalog.Catalog{
			SchemaVersion: 1,
			GeneratedAt:   "2026-04-30T00:00:00Z",
			Countries: []mapscatalog.Country{
				{ISO2: "de", Name: "Germany", SizeBytes: 1, SHA256: "x"},
			},
			Provinces: []mapscatalog.Province{
				{ISO2: "ca", Slug: "british-columbia", Name: "British Columbia", Code: "BC", SizeBytes: 1, SHA256: "x"},
			},
			States: []mapscatalog.State{
				{Slug: "georgia", Name: "Georgia", Code: "GA"},
				{Slug: "texas", Name: "Texas", Code: "TX"},
				{Slug: "ohio", Name: "Ohio", Code: "OH"},
				{Slug: "florida", Name: "Florida", Code: "FL"},
				{Slug: "nevada", Name: "Nevada", Code: "NV"},
				{Slug: "vermont", Name: "Vermont", Code: "VT"},
				{Slug: "colorado", Name: "Colorado", Code: "CO"},
			},
		})
	}))
	t.Cleanup(srv.Close)
	return mapscatalog.New(srv.URL, func(_ context.Context) string { return "tok" }, time.Hour)
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

	req := httptest.NewRequest(http.MethodGet, "/api/maps/downloads/state/georgia", nil)
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
	if out.Slug != "state/georgia" {
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
	// "xxx" doesn't match any of state/, country/, province/ prefixes,
	// so resolveSlug fails grammar before catalog membership.
	if !strings.Contains(body["error"], "invalid slug") {
		t.Errorf("expected 'invalid slug', got %q", body["error"])
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
	req := httptest.NewRequest(http.MethodPost, "/api/maps/downloads/state/georgia", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	var started dto.DownloadStatus
	if err := json.NewDecoder(rec.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	if started.Slug != "state/georgia" {
		t.Errorf("expected slug=georgia, got %q", started.Slug)
	}
	if started.State != "downloading" {
		t.Errorf("expected state=downloading, got %q", started.State)
	}

	// Wait for completion via the GET endpoint.
	completed := waitFor(t, 5*time.Second, func() bool {
		st, _ := mgr.Status(context.Background(), "state/georgia")
		return st.State == "complete"
	})
	if !completed {
		t.Fatalf("download did not complete in time")
	}

	// Now GET via HTTP and confirm the same.
	req = httptest.NewRequest(http.MethodGet, "/api/maps/downloads/state/georgia", nil)
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

	req := httptest.NewRequest(http.MethodPost, "/api/maps/downloads/state/texas", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("first POST: expected 202, got %d", rec.Code)
	}

	// Second POST while the first is still inflight should be 409.
	req = httptest.NewRequest(http.MethodPost, "/api/maps/downloads/state/texas", nil)
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
	_ = mgr.Delete(context.Background(), "state/texas")
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
	if err := mgr.Start(context.Background(), "state/ohio"); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 3*time.Second, func() bool {
		st, _ := mgr.Status(context.Background(), "state/ohio")
		return st.State == "complete"
	}) {
		t.Fatal("setup: download did not complete")
	}

	// Sanity: file exists before the delete.
	if _, err := os.Stat(mgr.PathFor("state/ohio")); err != nil {
		t.Fatalf("expected file to exist before delete: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/maps/downloads/state/ohio", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// File gone, GET reports absent.
	if _, err := os.Stat(mgr.PathFor("state/ohio")); !os.IsNotExist(err) {
		t.Fatalf("file should be gone after delete: %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/maps/downloads/state/ohio", nil)
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
	req = httptest.NewRequest(http.MethodDelete, "/api/maps/downloads/state/ohio", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("idempotent DELETE: expected 204, got %d", rec.Code)
	}
}

func TestServeTilesPMTiles_AbsentReturns404(t *testing.T) {
	srv, _, _ := newTestServerWithCache(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/tiles/florida.pmtiles", nil)
	req.SetPathValue("slug", "state/florida")
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
	stagePath := mgr.PathFor("state/nevada")
	if err := os.MkdirAll(filepath.Dir(stagePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagePath, want, 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/tiles/nevada.pmtiles", nil)
	req.SetPathValue("slug", "state/nevada")
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
	stagePath := mgr.PathFor("state/vermont")
	if err := os.MkdirAll(filepath.Dir(stagePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagePath, body, 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/tiles/vermont.pmtiles", nil)
	req.SetPathValue("slug", "state/vermont")
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
		{http.MethodGet, "/api/maps/downloads/state/georgia"},
		{http.MethodPost, "/api/maps/downloads/state/georgia"},
		{http.MethodDelete, "/api/maps/downloads/state/georgia"},
	} {
		req := httptest.NewRequest(route.method, route.path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: expected 503, got %d: %s", route.method, route.path, rec.Code, rec.Body.String())
		}
	}
}

// Sanity that the slug grammar rejects unicode and other malformed
// inputs before catalog membership runs.
func TestDownloads_UnicodeSlugRejected(t *testing.T) {
	srv, _, _ := newTestServerWithCache(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	mux := newDownloadsMux(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/maps/downloads/state/g%C3%A9orgia", nil)
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

// Country happy path: the namespaced country slug round-trips through
// resolveSlug + manager + on-disk path.
func TestStartMapsDownload_Country(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "16")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("0123456789ABCDEF"))
	})
	srv, mgr, _ := newTestServerWithCache(t, upstream)
	mux := newDownloadsMux(srv)

	req := httptest.NewRequest(http.MethodPost, "/api/maps/downloads/country/de", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var st dto.DownloadStatus
	if err := json.NewDecoder(rec.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if st.Slug != "country/de" {
		t.Errorf("slug=%q want country/de", st.Slug)
	}
	if !waitFor(t, 3*time.Second, func() bool {
		s, _ := mgr.Status(context.Background(), "country/de")
		return s.State == "complete"
	}) {
		t.Fatal("country download did not complete")
	}
	if _, err := os.Stat(mgr.PathFor("country/de")); err != nil {
		t.Fatalf("country file missing: %v", err)
	}
}

// Province happy path: province slugs include a third path segment;
// the wildcard route + resolveSlug + nested cache layout must all
// agree.
func TestStartMapsDownload_Province(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("12345678"))
	})
	srv, mgr, _ := newTestServerWithCache(t, upstream)
	mux := newDownloadsMux(srv)

	req := httptest.NewRequest(http.MethodPost, "/api/maps/downloads/province/ca/british-columbia", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !waitFor(t, 3*time.Second, func() bool {
		s, _ := mgr.Status(context.Background(), "province/ca/british-columbia")
		return s.State == "complete"
	}) {
		t.Fatal("province download did not complete")
	}
	if _, err := os.Stat(mgr.PathFor("province/ca/british-columbia")); err != nil {
		t.Fatalf("province file missing: %v", err)
	}
}

// country/cn is rejected at the grammar stage (not just catalog
// membership). We don't ship China archives.
func TestStartMapsDownload_ForbiddenCountry(t *testing.T) {
	srv, _, _ := newTestServerWithCache(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	mux := newDownloadsMux(srv)

	req := httptest.NewRequest(http.MethodPost, "/api/maps/downloads/country/cn", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// state/wisconsin matches the grammar but the fake catalog does not
// list it, so resolveSlug rejects on membership.
func TestStartMapsDownload_NotInCatalog(t *testing.T) {
	srv, _, _ := newTestServerWithCache(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	mux := newDownloadsMux(srv)

	req := httptest.NewRequest(http.MethodPost, "/api/maps/downloads/state/wisconsin", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body["error"], "unknown slug") {
		t.Errorf("expected 'unknown slug', got %q", body["error"])
	}
}
