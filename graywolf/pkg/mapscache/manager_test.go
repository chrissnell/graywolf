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
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected bearer token; got %q", r.Header.Get("Authorization"))
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

	if err := mgr.Start(context.Background(), "georgia"); err != nil {
		t.Fatal(err)
	}

	// Wait for completion (max 5s)
	deadline := time.Now().Add(5 * time.Second)
	var final Status
	for time.Now().Before(deadline) {
		s, err := mgr.Status(context.Background(), "georgia")
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
	data, err := os.ReadFile(mgr.PathFor("georgia"))
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

	if err := mgr.Start(context.Background(), "texas"); err != nil {
		t.Fatal(err)
	}
	err := mgr.Start(context.Background(), "texas")
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

	if err := mgr.Start(context.Background(), "ohio"); err != nil {
		t.Fatal(err)
	}
	// Give the goroutine a moment to start the request
	time.Sleep(100 * time.Millisecond)

	if err := mgr.Delete(context.Background(), "ohio"); err != nil {
		t.Fatal(err)
	}
	s, _ := mgr.Status(context.Background(), "ohio")
	if s.State != "absent" {
		t.Fatalf("expected absent after delete, got %+v", s)
	}
	if _, err := os.Stat(mgr.PathFor("ohio")); !os.IsNotExist(err) {
		t.Fatalf("file should not exist: %v", err)
	}
}

func TestManager_BadUpstreamStatus(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "no")
	})
	mgr, _, _ := newTestManager(t, upstream)

	if err := mgr.Start(context.Background(), "florida"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var final Status
	for time.Now().Before(deadline) {
		s, _ := mgr.Status(context.Background(), "florida")
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
	if _, err := os.Stat(mgr.PathFor("florida")); !os.IsNotExist(err) {
		t.Fatalf("file should not exist after failed download: %v", err)
	}
	if _, err := os.Stat(mgr.PathFor("florida") + ".tmp"); !os.IsNotExist(err) {
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

	_ = mgr.Start(context.Background(), "ohio")
	// Wait for first attempt to fail
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s, _ := mgr.Status(context.Background(), "ohio")
		if s.State == "error" {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}

	// Second attempt
	if err := mgr.Start(context.Background(), "ohio"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(2 * time.Second)
	var final Status
	for time.Now().Before(deadline) {
		s, _ := mgr.Status(context.Background(), "ohio")
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
