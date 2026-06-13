package mapscache

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

func newTestManager(t *testing.T, upstreamHandler http.Handler) (*Manager, *configstore.Store, *httptest.Server) {
	t.Helper()
	upstream := httptest.NewServer(upstreamHandler)
	t.Cleanup(upstream.Close)

	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cacheDir := t.TempDir()
	mgr := New(cacheDir, store, func(context.Context) string { return "test-token" }, upstream.URL, 2)
	return mgr, store, upstream
}

func TestManager_HappyPath(t *testing.T) {
	body := strings.Repeat("X", 64*1024) // 64 KB
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("t"); got != "test-token" {
			t.Errorf("expected t=test-token query param; got %q", got)
		}
		if h := r.Header.Get("Authorization"); h != "" {
			t.Errorf("expected no Authorization header; got %q", h)
		}
		w.Header().Set("Content-Length", "65536")
		w.Header().Set("Content-Type", "application/vnd.pmtiles")
		w.WriteHeader(http.StatusOK)
		// Slow write so progress is observable
		for i := 0; i < 8; i++ {
			_, _ = w.Write([]byte(body[i*8192 : (i+1)*8192]))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
	mgr, _, _ := newTestManager(t, upstream)

	if err := mgr.Start(context.Background(), "state/georgia", nil, 0); err != nil {
		t.Fatal(err)
	}

	// Wait for completion (max 5s)
	deadline := time.Now().Add(5 * time.Second)
	var final Status
	for time.Now().Before(deadline) {
		s, err := mgr.Status(context.Background(), "state/georgia")
		if err != nil {
			t.Fatal(err)
		}
		if s.State == "complete" {
			final = s
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if final.State != "complete" {
		t.Fatalf("download did not complete; final state %+v", final)
	}
	if final.BytesTotal != 65536 || final.BytesDownloaded != 65536 {
		t.Fatalf("bytes mismatch: %+v", final)
	}

	// File must exist at PathFor and contain the expected bytes
	data, err := os.ReadFile(mgr.PathFor("state/georgia"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 65536 || string(data) != body {
		t.Fatalf("file content mismatch")
	}
}

func TestManager_AlreadyInflight(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Hold the response open
		time.Sleep(2 * time.Second)
		_, _ = io.WriteString(w, "x")
	})
	mgr, _, _ := newTestManager(t, upstream)

	if err := mgr.Start(context.Background(), "state/texas", nil, 0); err != nil {
		t.Fatal(err)
	}
	err := mgr.Start(context.Background(), "state/texas", nil, 0)
	if !errors.Is(err, ErrAlreadyInflight) {
		t.Fatalf("expected ErrAlreadyInflight, got %v", err)
	}
}

func TestManager_DeleteDuringActiveDownload(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the request is canceled
		<-r.Context().Done()
	})
	mgr, _, _ := newTestManager(t, upstream)

	if err := mgr.Start(context.Background(), "state/ohio", nil, 0); err != nil {
		t.Fatal(err)
	}
	// Give the goroutine a moment to start the request
	time.Sleep(100 * time.Millisecond)

	if err := mgr.Delete(context.Background(), "state/ohio"); err != nil {
		t.Fatal(err)
	}
	s, _ := mgr.Status(context.Background(), "state/ohio")
	if s.State != "absent" {
		t.Fatalf("expected absent after delete, got %+v", s)
	}
	if _, err := os.Stat(mgr.PathFor("state/ohio")); !os.IsNotExist(err) {
		t.Fatalf("file should not exist: %v", err)
	}
}

// TestManager_CancelLeavesNoErrorRow guards the cancel-button path:
// deleting an in-flight download must not leave a phantom "error" row
// behind once the download goroutine unwinds. The upstream streams a
// little, then blocks, so the cancel lands mid-transfer (after the .tmp
// file has bytes in it) -- the realistic accidental-download scenario.
func TestManager_CancelLeavesNoErrorRow(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1048576") // claim 1 MiB
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Repeat("X", 8192)))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done() // block until the download is cancelled
	})
	mgr, _, _ := newTestManager(t, upstream)

	if err := mgr.Start(context.Background(), "state/nebraska", nil, 0); err != nil {
		t.Fatal(err)
	}
	// Let the goroutine grab the semaphore, issue the request, and start
	// streaming bytes into the .tmp file.
	time.Sleep(150 * time.Millisecond)

	if err := mgr.Delete(context.Background(), "state/nebraska"); err != nil {
		t.Fatal(err)
	}

	// Poll past the point where the download goroutine has fully unwound
	// (the blocked handler returns once the context is cancelled, then
	// run() calls fail()). The state must stay "absent" the whole time --
	// never flicker to "error".
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		s, err := mgr.Status(context.Background(), "state/nebraska")
		if err != nil {
			t.Fatal(err)
		}
		if s.State != "absent" {
			t.Fatalf("expected state to stay absent after cancel, got %+v", s)
		}
		time.Sleep(25 * time.Millisecond)
	}

	if _, err := os.Stat(mgr.PathFor("state/nebraska")); !os.IsNotExist(err) {
		t.Fatalf("archive should not exist after cancel: %v", err)
	}
	if _, err := os.Stat(mgr.PathFor("state/nebraska") + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf(".tmp file should not exist after cancel: %v", err)
	}
}

func TestManager_BadUpstreamStatus(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "no")
	})
	mgr, _, _ := newTestManager(t, upstream)

	if err := mgr.Start(context.Background(), "state/florida", nil, 0); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var final Status
	for time.Now().Before(deadline) {
		s, _ := mgr.Status(context.Background(), "state/florida")
		if s.State == "error" {
			final = s
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if final.State != "error" {
		t.Fatalf("expected error state, got %+v", final)
	}
	if !strings.Contains(final.ErrorMessage, "401") {
		t.Fatalf("error message should mention 401: %q", final.ErrorMessage)
	}
	// File must not exist (the .tmp was cleaned up too)
	if _, err := os.Stat(mgr.PathFor("state/florida")); !os.IsNotExist(err) {
		t.Fatalf("file should not exist after failed download: %v", err)
	}
	if _, err := os.Stat(mgr.PathFor("state/florida") + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf(".tmp file should not exist after failed download: %v", err)
	}
}

func TestManager_RetryAfterError(t *testing.T) {
	calls := 0
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Length", "16")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "0123456789ABCDEF")
	})
	mgr, _, _ := newTestManager(t, upstream)

	_ = mgr.Start(context.Background(), "state/ohio", nil, 0)
	// Wait for first attempt to fail
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s, _ := mgr.Status(context.Background(), "state/ohio")
		if s.State == "error" {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}

	// Second attempt
	if err := mgr.Start(context.Background(), "state/ohio", nil, 0); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(2 * time.Second)
	var final Status
	for time.Now().Before(deadline) {
		s, _ := mgr.Status(context.Background(), "state/ohio")
		if s.State == "complete" {
			final = s
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if final.State != "complete" {
		t.Fatalf("retry did not complete: %+v", final)
	}
	if final.BytesTotal != 16 {
		t.Fatalf("expected 16 bytes, got %+v", final)
	}
}

func TestURLForSlug(t *testing.T) {
	m := &Manager{mapsBaseURL: "https://maps.example"}
	cases := []struct {
		slug string
		want string
	}{
		{"state/colorado", "https://maps.example/download/state/colorado.pmtiles"},
		{"country/de", "https://maps.example/download/country/de.pmtiles"},
		{"province/ca/british-columbia", "https://maps.example/download/province/ca/british-columbia.pmtiles"},
		{"world", "https://maps.example/download/world.pmtiles"},
	}
	for _, tc := range cases {
		got, err := m.urlForSlug(tc.slug, "")
		if err != nil {
			t.Fatalf("urlForSlug(%q): %v", tc.slug, err)
		}
		if got != tc.want {
			t.Errorf("urlForSlug(%q) = %q, want %q", tc.slug, got, tc.want)
		}
	}
	if p := m.PathFor("world"); !strings.HasSuffix(p, "world.pmtiles") {
		t.Fatalf("PathFor(world) = %q, want suffix world.pmtiles", p)
	}
}

func TestURLForSlug_AppendsToken(t *testing.T) {
	m := &Manager{mapsBaseURL: "https://maps.example"}
	got, err := m.urlForSlug("state/colorado", "tok")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://maps.example/download/state/colorado.pmtiles?t=tok"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestURLForSlug_RejectsBad(t *testing.T) {
	m := &Manager{mapsBaseURL: "https://maps.example"}
	if _, err := m.urlForSlug("colorado", ""); err == nil {
		t.Fatal("expected error for legacy bare slug")
	}
	if _, err := m.urlForSlug("country/cn", ""); err == nil {
		t.Fatal("expected error for forbidden country")
	}
}

func TestMigrateLegacyArchives(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"colorado.pmtiles", "wyoming.pmtiles"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "country"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "country", "de.pmtiles"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &Manager{cacheDir: dir}
	if err := m.MigrateLegacyArchives(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"colorado", "wyoming"} {
		newPath := filepath.Join(dir, "state", slug+".pmtiles")
		if _, err := os.Stat(newPath); err != nil {
			t.Errorf("expected %s to exist: %v", newPath, err)
		}
		oldPath := filepath.Join(dir, slug+".pmtiles")
		if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
			t.Errorf("expected %s to be gone, err=%v", oldPath, err)
		}
	}
	if err := m.MigrateLegacyArchives(context.Background()); err != nil {
		t.Fatalf("second run: %v", err)
	}
}

// Collision: legacy bare file AND namespaced file both exist. Migration
// must NOT overwrite the namespaced file; it removes the legacy one.
func TestMigrateLegacyArchives_CollisionDropsLegacy(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "colorado.pmtiles"), []byte("LEGACY"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(dir, "state", "colorado.pmtiles")
	if err := os.WriteFile(newPath, []byte("NEW"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &Manager{cacheDir: dir}
	if err := m.MigrateLegacyArchives(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEW" {
		t.Fatalf("namespaced file was overwritten by legacy: got %q", string(got))
	}
	if _, err := os.Stat(filepath.Join(dir, "colorado.pmtiles")); !os.IsNotExist(err) {
		t.Fatalf("legacy file should have been removed; err=%v", err)
	}
}

func TestPathFor(t *testing.T) {
	m := &Manager{cacheDir: "/var/lib/graywolf/tiles"}
	cases := []struct {
		slug string
		want string
	}{
		{"state/colorado", "/var/lib/graywolf/tiles/state/colorado.pmtiles"},
		{"country/de", "/var/lib/graywolf/tiles/country/de.pmtiles"},
		{"province/ca/british-columbia", "/var/lib/graywolf/tiles/province/ca/british-columbia.pmtiles"},
	}
	for _, tc := range cases {
		if got := m.PathFor(tc.slug); got != tc.want {
			t.Errorf("PathFor(%q) = %q, want %q", tc.slug, got, tc.want)
		}
	}
}

// silentUpstream returns 500 so any download spawned by Start() fails
// fast — bbox-persistence tests only need to observe the initial row
// upsert, not a completed transfer.
func silentUpstream() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }
}

func TestStart_SnapshotsBBoxIntoFirstRow(t *testing.T) {
	ctx := context.Background()
	mgr, store, _ := newTestManager(t, silentUpstream())

	bbox := [4]float64{-109.05, 36.99, -102.04, 41.0}
	if err := mgr.Start(ctx, "state/colorado", &bbox, 0); err != nil {
		t.Fatalf("Start: %v", err)
	}

	row, err := store.GetMapsDownload(ctx, "state/colorado")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.BBox == nil {
		t.Fatalf("expected bbox to be persisted, got NULL")
	}
	want := `[-109.05,36.99,-102.04,41]`
	if *row.BBox != want {
		t.Fatalf("bbox: got %q want %q", *row.BBox, want)
	}
}

func TestStart_NilBBoxLeavesColumnNull(t *testing.T) {
	ctx := context.Background()
	mgr, store, _ := newTestManager(t, silentUpstream())

	if err := mgr.Start(ctx, "state/wyoming", nil, 0); err != nil {
		t.Fatalf("Start: %v", err)
	}
	row, err := store.GetMapsDownload(ctx, "state/wyoming")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.BBox != nil {
		t.Fatalf("expected NULL bbox, got %q", *row.BBox)
	}
}

func TestBackfillBBoxes_FillsNullForCompletedRows(t *testing.T) {
	ctx := context.Background()
	mgr, store, _ := newTestManager(t, silentUpstream())

	// Seed a completed row without a bbox and write a fake archive at
	// the path the manager expects.
	if err := store.UpsertMapsDownload(ctx, configstore.MapsDownload{
		Slug: "state/colorado", Status: "complete",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	archive := mgr.PathFor("state/colorado")
	if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	hdr := buildPMTilesV3Header(t, -109.05, 36.99, -102.04, 41.0)
	if err := os.WriteFile(archive, hdr, 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	if err := mgr.BackfillBBoxes(ctx); err != nil {
		t.Fatalf("BackfillBBoxes: %v", err)
	}
	row, _ := store.GetMapsDownload(ctx, "state/colorado")
	if row.BBox == nil {
		t.Fatalf("bbox still NULL after backfill")
	}
	want := `[-109.05,36.99,-102.04,41]`
	if *row.BBox != want {
		t.Fatalf("bbox: got %q want %q", *row.BBox, want)
	}
}

func TestBackfillBBoxes_LeavesPopulatedRowsAlone(t *testing.T) {
	ctx := context.Background()
	mgr, store, _ := newTestManager(t, silentUpstream())

	existing := `[1,2,3,4]`
	if err := store.UpsertMapsDownload(ctx, configstore.MapsDownload{
		Slug: "state/colorado", Status: "complete", BBox: &existing,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// No archive on disk -- if backfill tried to read it, it would
	// log a warning. The bbox value being unchanged proves the
	// populated row was skipped without an archive read.
	if err := mgr.BackfillBBoxes(ctx); err != nil {
		t.Fatalf("BackfillBBoxes: %v", err)
	}
	row, _ := store.GetMapsDownload(ctx, "state/colorado")
	if row.BBox == nil || *row.BBox != existing {
		t.Fatalf("expected bbox %q preserved, got %v", existing, row.BBox)
	}
}

func TestBackfillBBoxes_SkipsMissingArchives(t *testing.T) {
	ctx := context.Background()
	mgr, store, _ := newTestManager(t, silentUpstream())

	if err := store.UpsertMapsDownload(ctx, configstore.MapsDownload{
		Slug: "state/ghost", Status: "complete",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// No archive on disk. Backfill should log and continue, not error.
	if err := mgr.BackfillBBoxes(ctx); err != nil {
		t.Fatalf("BackfillBBoxes returned error on missing archive: %v", err)
	}
	row, _ := store.GetMapsDownload(ctx, "state/ghost")
	if row.BBox != nil {
		t.Fatalf("expected bbox to remain NULL for missing archive, got %q", *row.BBox)
	}
}
