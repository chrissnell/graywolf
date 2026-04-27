package webapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// TestUpdateAgwSignalsReload verifies that a successful PUT /api/agw
// sends on the reload channel that wiring.go drains to restart the
// live AGW server.
func TestUpdateAgwSignalsReload(t *testing.T) {
	srv, _ := newTestServer(t)

	reload := make(chan struct{}, 1)
	srv.SetAgwReload(reload)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body, _ := json.Marshal(dto.AgwRequest{
		ListenAddr: "127.0.0.1:0",
		Callsigns:  "N0CALL",
		Enabled:    true,
	})
	req := httptest.NewRequest(http.MethodPut, "/api/agw", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	select {
	case <-reload:
		// ok
	default:
		t.Fatal("expected reload signal after PUT /api/agw")
	}
}

// TestUpdateAgwReloadCoalesces verifies the non-blocking send pattern:
// a second update with a signal already buffered must not block.
func TestUpdateAgwReloadCoalesces(t *testing.T) {
	srv, _ := newTestServer(t)

	// Buffered, pre-filled so the next signal would have to block
	// if signalAgwReload were not non-blocking.
	reload := make(chan struct{}, 1)
	reload <- struct{}{}
	srv.SetAgwReload(reload)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body, _ := json.Marshal(dto.AgwRequest{
		ListenAddr: "127.0.0.1:0",
		Callsigns:  "N0CALL",
		Enabled:    false,
	})
	req := httptest.NewRequest(http.MethodPut, "/api/agw", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
