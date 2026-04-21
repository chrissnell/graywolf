package webapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/updatescheck"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// ---------------------------------------------------------------------------
// GET /api/updates/config
// ---------------------------------------------------------------------------

// Fresh DB — no UpdatesConfig row yet. GET returns 200 with
// enabled=true per the zero-value-on-missing contract (Phase 1 D2).
func TestGetUpdatesConfig_DefaultEnabled(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/updates/config", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.UpdatesConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Enabled {
		t.Errorf("Enabled = false, want true (default)")
	}
}

// ---------------------------------------------------------------------------
// PUT /api/updates/config
// ---------------------------------------------------------------------------

// Round-trip: PUT enabled=false, expect the response echoes the stored
// value and a follow-up GET returns enabled=false.
func TestPutUpdatesConfig_Persists(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"enabled":false}`
	req := httptest.NewRequest(http.MethodPut, "/api/updates/config", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.UpdatesConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Enabled {
		t.Errorf("PUT response Enabled = true, want false")
	}

	// Follow-up GET reflects the stored value.
	req = httptest.NewRequest(http.MethodGet, "/api/updates/config", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("follow-up GET: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got dto.UpdatesConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Errorf("GET after PUT: Enabled = true, want false")
	}

	// Persistence in the store.
	c, err := srv.store.GetUpdatesConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.Enabled {
		t.Errorf("store.GetUpdatesConfig.Enabled = true, want false")
	}
}

// A successful PUT must nudge the reload channel so the checker
// re-evaluates immediately (D4). Coalescing via the size-1 buffer
// means a single send is observable as exactly one receive.
func TestPutUpdatesConfig_SignalsReload(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"enabled":false}`
	req := httptest.NewRequest(http.MethodPut, "/api/updates/config", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// A wakeup must be present. A short timeout prevents the test from
	// hanging if the signal was never sent.
	select {
	case <-srv.UpdatesReloadCh():
	case <-time.After(250 * time.Millisecond):
		t.Fatal("expected a wakeup on UpdatesReloadCh, got none")
	}

	// The channel must now be empty (size-1 buffer, single send).
	select {
	case <-srv.UpdatesReloadCh():
		t.Fatal("UpdatesReloadCh should be drained after one receive")
	default:
	}
}

// Malformed JSON in the PUT body surfaces as 400.
func TestPutUpdatesConfig_MalformedBodyReturns400(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPut, "/api/updates/config", strings.NewReader("{bogus"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// GET /api/updates/status
// ---------------------------------------------------------------------------

// When no checker has been installed yet (wiring not run, or a test
// skipping Phase 4), GET /api/updates/status must return a synthesized
// "pending" response with the running version as Current — not panic.
func TestGetUpdatesStatus_NoCheckerReturnsPending(t *testing.T) {
	srv, _ := newTestServer(t)
	// newTestServer does not set Config.Version; stamp one in so the
	// "Current" projection is observable. Same-package field access is
	// idiomatic across this test file set.
	srv.version = "0.10.11"

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/updates/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.UpdatesStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != updatescheck.StatusPending {
		t.Errorf("Status = %q, want %q", resp.Status, updatescheck.StatusPending)
	}
	if resp.Current != "0.10.11" {
		t.Errorf("Current = %q, want %q", resp.Current, "0.10.11")
	}
	if resp.Latest != "" || resp.URL != "" || resp.CheckedAt != "" {
		t.Errorf("Latest/URL/CheckedAt should be empty on pending; got %+v", resp)
	}
}

// A freshly-constructed Checker (before its first evaluate fires) holds
// the initial "pending" status in its cache. Wiring it into the Server
// and reading /api/updates/status exercises the Snapshot -> DTO
// projection path end-to-end without waiting on the 10s startup timer.
// The full "available" / "current" projections are covered by the
// pkg/updatescheck checker_test.go suite against the Status struct —
// an end-to-end test that drove a live check through the webapi would
// require an exported test hook inside pkg/updatescheck (out of scope
// for Phase 3). See the Phase 3 handoff for rationale.
func TestGetUpdatesStatus_ProjectsCheckerSnapshot(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.version = "0.10.11"

	// Real Checker pointed at a httptest server; we do NOT call Run, so
	// the cached Status stays at the constructor's initial
	// {Status: StatusPending} value. That's enough to prove the Server
	// -> Snapshot -> DTO wiring without the 10s startup delay.
	//
	// The store stub returns Enabled=true so a hypothetical evaluate
	// would actually hit the httptest.Server; we keep the httptest
	// server so a future test change that does drive Run has a sane
	// default target.
	ghSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"tag_name":"v0.12.0","html_url":"https://example.com/tag/v0.12.0"}`)
	}))
	defer ghSrv.Close()

	checker := updatescheck.NewChecker(
		"0.10.11",
		&updatesStubStore{cfg: configstore.UpdatesConfig{Enabled: true}},
		ghSrv.URL,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	srv.SetUpdatesChecker(checker)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/updates/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.UpdatesStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != updatescheck.StatusPending {
		t.Errorf("Status = %q, want %q (initial cache)", resp.Status, updatescheck.StatusPending)
	}
	// Checker.Snapshot() at construction returns Current="" (not copied
	// from the Checker's version field until evaluate runs), so the
	// projection is faithful to the cached struct. This asserts the
	// handler does NOT substitute Server.version when a checker is
	// installed — it projects only what the checker returned.
	if resp.Current != "" {
		t.Errorf("Current = %q, want empty (checker initial status has no Current)", resp.Current)
	}
	if resp.CheckedAt != "" {
		t.Errorf("CheckedAt = %q, want empty (no successful check yet)", resp.CheckedAt)
	}
}

// updatesStubStore implements pkg/updatescheck.Store without a real DB.
// Kept local to this test file so it doesn't leak into production code.
type updatesStubStore struct {
	cfg configstore.UpdatesConfig
	err error
}

func (s *updatesStubStore) GetUpdatesConfig(_ context.Context) (configstore.UpdatesConfig, error) {
	return s.cfg, s.err
}
