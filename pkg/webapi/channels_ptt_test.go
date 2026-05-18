package webapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

// TestManualPttREST_ForwardsKeyedToBridge verifies that
// POST /api/channels/{id}/ptt {"keyed":true} dispatches a ManualPtt IPC
// message with the correct channel and keyed=true.
func TestManualPttREST_ForwardsKeyedToBridge(t *testing.T) {
	srv, bridge := newTestServer(t)

	var captured atomic.Pointer[pb.ManualPtt]
	restore := bridge.InjectSendFnForTest(func(msg *pb.IpcMessage) error {
		if mp, ok := msg.Payload.(*pb.IpcMessage_ManualPtt); ok {
			captured.Store(mp.ManualPtt)
		}
		return nil
	})
	defer restore()

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/channels/1/ptt", strings.NewReader(`{"keyed":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	got := captured.Load()
	if got == nil {
		t.Fatal("no ManualPtt IPC message was sent")
	}
	if got.Channel != 1 {
		t.Errorf("expected channel=1, got %d", got.Channel)
	}
	if !got.Keyed {
		t.Errorf("expected keyed=true, got false")
	}
}

// TestManualPttREST_NilBridgeReturns503 ensures the handler returns 503 when
// the server has no bridge (e.g. early startup or test without bridge wiring).
func TestManualPttREST_NilBridgeReturns503(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.bridge = nil // detach bridge

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/channels/1/ptt", strings.NewReader(`{"keyed":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestManualPttREST_BadIDReturns400 ensures invalid path segments are rejected.
func TestManualPttREST_BadIDReturns400(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/channels/abc/ptt", strings.NewReader(`{"keyed":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
