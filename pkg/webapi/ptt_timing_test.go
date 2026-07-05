package webapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// TestGetPttTiming_EmptyStoreReturnsDefaults pins the contract that a
// GET before any row exists returns the protocol defaults (300/100),
// not a zero-value body, so the PTT page always renders sane numbers.
func TestGetPttTiming_EmptyStoreReturnsDefaults(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/ptt-timing", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got dto.PttTimingResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.TxDelayMs != 300 || got.TxTailMs != 100 {
		t.Errorf("defaults = %d/%d, want 300/100", got.TxDelayMs, got.TxTailMs)
	}
}

// TestUpdatePttTiming_RoundTrips verifies PUT persists the global timing
// and a subsequent GET reflects it.
func TestUpdatePttTiming_RoundTrips(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body, _ := json.Marshal(dto.PttTimingRequest{TxDelayMs: 420, TxTailMs: 55})
	req := httptest.NewRequest(http.MethodPut, "/api/ptt-timing", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PUT expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var put dto.PttTimingResponse
	if err := json.NewDecoder(rec.Body).Decode(&put); err != nil {
		t.Fatal(err)
	}
	if put.TxDelayMs != 420 || put.TxTailMs != 55 {
		t.Errorf("PUT body = %d/%d, want 420/55", put.TxDelayMs, put.TxTailMs)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/ptt-timing", nil)
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}
	var got dto.PttTimingResponse
	if err := json.NewDecoder(getRec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.TxDelayMs != 420 || got.TxTailMs != 55 {
		t.Errorf("GET after PUT = %d/%d, want 420/55", got.TxDelayMs, got.TxTailMs)
	}
}
