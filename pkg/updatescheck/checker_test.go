package updatescheck

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// stubStore is a minimal Store implementation for tests. The returned
// UpdatesConfig and optional error are supplied per test.
type stubStore struct {
	cfg configstore.UpdatesConfig
	err error
}

func (s *stubStore) GetUpdatesConfig(_ context.Context) (configstore.UpdatesConfig, error) {
	return s.cfg, s.err
}

// spyTransport wraps an http.RoundTripper and records call count +
// the most recent request. failIfCalled flips to a test failure when
// the transport is used while we expected it not to be.
type spyTransport struct {
	t            *testing.T
	inner        http.RoundTripper
	calls        int64
	lastReq      atomic.Pointer[http.Request]
	failIfCalled atomic.Bool
}

func (s *spyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	atomic.AddInt64(&s.calls, 1)
	s.lastReq.Store(req)
	if s.failIfCalled.Load() {
		s.t.Errorf("unexpected HTTP call to %s", req.URL)
	}
	if s.inner == nil {
		s.inner = http.DefaultTransport
	}
	return s.inner.RoundTrip(req)
}

// newTestChecker wires a Checker with zero startup delay and a long
// tick interval so evaluate() can be called directly without Run
// firing twice.
func newTestChecker(t *testing.T, version string, store Store, serverURL string) (*Checker, *spyTransport) {
	t.Helper()
	spy := &spyTransport{t: t, inner: http.DefaultTransport}
	c := NewChecker(version, store, serverURL, nil)
	c.client.Transport = spy
	c.setStartupDelay(0)
	c.setTickInterval(time.Hour) // long enough not to fire in the test window
	return c, spy
}

// fixedHandler returns an http.Handler that always responds with
// status and body.
func fixedHandler(status int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
}

func TestNewerTagAvailable(t *testing.T) {
	srv := httptest.NewServer(fixedHandler(http.StatusOK,
		`{"tag_name":"v0.12.0","html_url":"https://github.com/chrissnell/graywolf/releases/tag/v0.12.0"}`))
	defer srv.Close()

	store := &stubStore{cfg: configstore.UpdatesConfig{Enabled: true}}
	c, _ := newTestChecker(t, "0.10.11", store, srv.URL)

	c.evaluate(context.Background())

	got := c.Snapshot()
	if got.Status != StatusAvailable {
		t.Errorf("status: got %q, want %q", got.Status, StatusAvailable)
	}
	if got.Latest != "0.12.0" {
		t.Errorf("latest: got %q, want %q", got.Latest, "0.12.0")
	}
	if got.URL != "https://github.com/chrissnell/graywolf/releases/tag/v0.12.0" {
		t.Errorf("url: got %q", got.URL)
	}
	if got.Current != "0.10.11" {
		t.Errorf("current: got %q, want %q", got.Current, "0.10.11")
	}
	if got.CheckedAt.IsZero() {
		t.Error("CheckedAt should be set on successful check")
	}
}

func TestNoNewerTag(t *testing.T) {
	srv := httptest.NewServer(fixedHandler(http.StatusOK,
		`{"tag_name":"v0.10.0","html_url":"https://example.invalid"}`))
	defer srv.Close()

	store := &stubStore{cfg: configstore.UpdatesConfig{Enabled: true}}
	c, _ := newTestChecker(t, "0.10.11", store, srv.URL)

	c.evaluate(context.Background())

	got := c.Snapshot()
	if got.Status != StatusCurrent {
		t.Errorf("status: got %q, want %q", got.Status, StatusCurrent)
	}
}

func TestSameTag(t *testing.T) {
	srv := httptest.NewServer(fixedHandler(http.StatusOK,
		`{"tag_name":"v0.10.11","html_url":"https://example.invalid"}`))
	defer srv.Close()

	store := &stubStore{cfg: configstore.UpdatesConfig{Enabled: true}}
	c, _ := newTestChecker(t, "0.10.11", store, srv.URL)

	c.evaluate(context.Background())

	got := c.Snapshot()
	if got.Status != StatusCurrent {
		t.Errorf("status: got %q, want %q (same tag should be current)", got.Status, StatusCurrent)
	}
}

func TestHTTPError(t *testing.T) {
	srv := httptest.NewServer(fixedHandler(http.StatusInternalServerError, `{"message":"boom"}`))
	defer srv.Close()

	store := &stubStore{cfg: configstore.UpdatesConfig{Enabled: true}}
	c, _ := newTestChecker(t, "0.10.11", store, srv.URL)

	// Fresh Checker — initial cached status is "pending". A 500
	// should leave that untouched rather than flipping to some error
	// state.
	c.evaluate(context.Background())

	got := c.Snapshot()
	if got.Status != StatusPending {
		t.Errorf("status: got %q, want %q after HTTP 500", got.Status, StatusPending)
	}
	if got.Latest != "" {
		t.Errorf("latest should stay empty on error, got %q", got.Latest)
	}
}

func TestMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(fixedHandler(http.StatusOK, "this is not json { "))
	defer srv.Close()

	store := &stubStore{cfg: configstore.UpdatesConfig{Enabled: true}}
	c, _ := newTestChecker(t, "0.10.11", store, srv.URL)

	c.evaluate(context.Background())

	got := c.Snapshot()
	if got.Status != StatusPending {
		t.Errorf("status: got %q, want %q after malformed JSON", got.Status, StatusPending)
	}
}

func TestDisabledSkipsHTTP(t *testing.T) {
	// This server should never be hit; if it is, the spy transport
	// fails the test.
	srv := httptest.NewServer(fixedHandler(http.StatusOK, `{"tag_name":"v99.0.0"}`))
	defer srv.Close()

	store := &stubStore{cfg: configstore.UpdatesConfig{Enabled: false}}
	c, spy := newTestChecker(t, "0.10.11", store, srv.URL)
	spy.failIfCalled.Store(true)

	c.evaluate(context.Background())

	got := c.Snapshot()
	if got.Status != StatusDisabled {
		t.Errorf("status: got %q, want %q when toggle off", got.Status, StatusDisabled)
	}
	if atomic.LoadInt64(&spy.calls) != 0 {
		t.Errorf("expected zero HTTP calls when disabled, got %d", spy.calls)
	}
}

func TestReloadWakesEvaluate(t *testing.T) {
	srv := httptest.NewServer(fixedHandler(http.StatusOK,
		`{"tag_name":"v0.12.0","html_url":"https://example.invalid/v0.12.0"}`))
	defer srv.Close()

	store := &stubStore{cfg: configstore.UpdatesConfig{Enabled: true}}
	spy := &spyTransport{t: t, inner: http.DefaultTransport}
	c := NewChecker("0.10.11", store, srv.URL, nil)
	c.client.Transport = spy
	c.setStartupDelay(0)
	// Long enough that the ticker doesn't fire during the test
	// window; we're isolating the reload branch.
	c.setTickInterval(time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reload := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		_ = c.Run(ctx, reload)
		close(done)
	}()

	// Wait for the initial post-startup evaluate() to record a call.
	waitForCalls(t, spy, 1, 2*time.Second)

	// Send a reload nudge; verify a second evaluate() fires.
	reload <- struct{}{}
	waitForCalls(t, spy, 2, 2*time.Second)

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after ctx cancel")
	}
}

func TestUserAgentSent(t *testing.T) {
	var gotUA string
	var gotAccept string
	var gotAPIVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
		gotAPIVersion = r.Header.Get("X-GitHub-Api-Version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"tag_name":"v0.10.11","html_url":"x"}`)
	}))
	defer srv.Close()

	store := &stubStore{cfg: configstore.UpdatesConfig{Enabled: true}}
	c, _ := newTestChecker(t, "0.10.11", store, srv.URL)

	c.evaluate(context.Background())

	if !strings.Contains(gotUA, "graywolf/") {
		t.Errorf("User-Agent missing graywolf/ prefix: %q", gotUA)
	}
	if gotAccept != "application/vnd.github+json" {
		t.Errorf("Accept header: got %q", gotAccept)
	}
	if gotAPIVersion != "2022-11-28" {
		t.Errorf("X-GitHub-Api-Version: got %q", gotAPIVersion)
	}
}

func TestCurrentStrippedForDevBuild(t *testing.T) {
	srv := httptest.NewServer(fixedHandler(http.StatusOK,
		`{"tag_name":"v0.11.0","html_url":"https://example.invalid/v0.11.0"}`))
	defer srv.Close()

	store := &stubStore{cfg: configstore.UpdatesConfig{Enabled: true}}
	// Dev-build-style version string with a suffix.
	c, _ := newTestChecker(t, "0.10.11-dirty", store, srv.URL)

	c.evaluate(context.Background())

	got := c.Snapshot()
	if got.Current != "0.10.11" {
		t.Errorf("Current: got %q, want %q (suffix should be stripped)", got.Current, "0.10.11")
	}
	if got.Status != StatusAvailable {
		t.Errorf("Status: got %q, want %q", got.Status, StatusAvailable)
	}
}

func TestPathHitsReleasesLatest(t *testing.T) {
	// Risk #3 in the plan asserts we target /releases/latest (which
	// excludes prereleases) rather than /releases.
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"tag_name":"v0.10.11","html_url":"x"}`)
	}))
	defer srv.Close()

	store := &stubStore{cfg: configstore.UpdatesConfig{Enabled: true}}
	c, _ := newTestChecker(t, "0.10.11", store, srv.URL)
	c.evaluate(context.Background())

	want := "/repos/chrissnell/graywolf/releases/latest"
	if gotPath != want {
		t.Errorf("path: got %q, want %q", gotPath, want)
	}
}

// waitForCalls polls spy.calls until it reaches n or the deadline
// expires. Preferable to a fixed sleep because it latches the moment
// the background goroutine's evaluate() completes.
func waitForCalls(t *testing.T, spy *spyTransport, n int64, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&spy.calls) >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d HTTP calls, got %d", n, atomic.LoadInt64(&spy.calls))
}
