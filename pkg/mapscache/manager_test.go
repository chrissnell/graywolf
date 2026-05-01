package mapscache

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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

	if err := mgr.Start(context.Background(), "state/georgia"); err != nil {
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

	if err := mgr.Start(context.Background(), "state/texas"); err != nil {
		t.Fatal(err)
	}
	err := mgr.Start(context.Background(), "state/texas")
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

	if err := mgr.Start(context.Background(), "state/ohio"); err != nil {
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

func TestManager_BadUpstreamStatus(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "no")
	})
	mgr, _, _ := newTestManager(t, upstream)

	if err := mgr.Start(context.Background(), "state/florida"); err != nil {
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

	_ = mgr.Start(context.Background(), "state/ohio")
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
	if err := mgr.Start(context.Background(), "state/ohio"); err != nil {
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
