package webapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chrissnell/graywolf/pkg/igate"
)

// Wire-level regressions for the disabled-state HTTP contract introduced
// alongside the iGate runtime-toggle fix (graywolf issue #84). When the
// status / toggle closures report the iGate is currently off, the
// handlers must respond 503 — NOT 200-with-empty-snapshot, NOT 500 —
// because the Svelte "Disabled" badge logic in
// web/src/routes/Igate.svelte keys off a non-2xx response.

func TestGetIgateStatus_NilCallback_Returns503(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	RegisterIgate(srv, mux,
		func(bool) error { return nil },
		nil, // callback==nil mimics a server constructed before SetIgate*
	)

	req := httptest.NewRequest(http.MethodGet, "/api/igate", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when status callback is nil, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetIgateStatus_DisabledClosureReturnsNil_503(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	RegisterIgate(srv, mux,
		func(bool) error { return igate.ErrNotEnabled },
		func() *igate.Status { return nil }, // disabled
	)

	req := httptest.NewRequest(http.MethodGet, "/api/igate", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when iGate is disabled, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetIgateStatus_EnabledClosureReturnsSnapshot_200(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	RegisterIgate(srv, mux,
		func(bool) error { return nil },
		func() *igate.Status {
			return &igate.Status{Connected: true, Server: "test:14580"}
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/igate", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when iGate is enabled, got %d: %s", rec.Code, rec.Body.String())
	}
	var got igate.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal status body: %v", err)
	}
	if !got.Connected || got.Server != "test:14580" {
		t.Fatalf("status body did not round-trip the snapshot: %+v", got)
	}
}

func TestSetIgateSimulation_DisabledToggleReturnsErrNotEnabled_503(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	RegisterIgate(srv, mux,
		func(bool) error { return igate.ErrNotEnabled },
		func() *igate.Status { return nil },
	)

	body, _ := json.Marshal(IgateToggleRequest{Enabled: true})
	req := httptest.NewRequest(http.MethodPost, "/api/igate/simulation", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when toggle returns ErrNotEnabled, got %d: %s", rec.Code, rec.Body.String())
	}
}
