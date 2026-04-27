package webapi

import (
	"bytes"
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
	"github.com/chrissnell/graywolf/pkg/kiss"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// TestCreateKissInterfaceMode pins the API contract for the new Mode
// field: the decoder accepts missing/valid values (rounding the empty
// case back to "modem") and returns 400 for anything else. The test
// drives the same request → DTO → store path the real CLI and UI
// exercise, so a regression in any layer trips here.
func TestCreateKissInterfaceMode(t *testing.T) {
	cases := []struct {
		name     string
		body     map[string]any
		wantCode int
		wantMode string
	}{
		{
			name:     "missing mode defaults to modem",
			body:     map[string]any{"type": "tcp", "tcp_port": 18001, "channel": 1},
			wantCode: http.StatusCreated,
			wantMode: configstore.KissModeModem,
		},
		{
			name:     "explicit modem round-trips",
			body:     map[string]any{"type": "tcp", "tcp_port": 18002, "channel": 1, "mode": "modem"},
			wantCode: http.StatusCreated,
			wantMode: configstore.KissModeModem,
		},
		{
			name:     "explicit tnc round-trips",
			body:     map[string]any{"type": "tcp", "tcp_port": 18003, "channel": 1, "mode": "tnc"},
			wantCode: http.StatusCreated,
			wantMode: configstore.KissModeTnc,
		},
		{
			name:     "invalid mode returns 400",
			body:     map[string]any{"type": "tcp", "tcp_port": 18004, "channel": 1, "mode": "bogus"},
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "case variant rejected",
			body:     map[string]any{"type": "tcp", "tcp_port": 18005, "channel": 1, "mode": "TNC"},
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "rate above cap returns 400",
			body:     map[string]any{"type": "tcp", "tcp_port": 18006, "channel": 1, "tnc_ingress_rate_hz": 99999},
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "burst above cap returns 400",
			body:     map[string]any{"type": "tcp", "tcp_port": 18007, "channel": 1, "tnc_ingress_burst": 9999999},
			wantCode: http.StatusBadRequest,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, _ := newTestServer(t)
			mux := http.NewServeMux()
			srv.RegisterRoutes(mux)

			raw, err := json.Marshal(c.body)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/kiss", bytes.NewReader(raw))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != c.wantCode {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, c.wantCode, rec.Body.String())
			}
			if c.wantCode != http.StatusCreated {
				return
			}
			var resp dto.KissResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if resp.Mode != c.wantMode {
				t.Errorf("Mode = %q, want %q", resp.Mode, c.wantMode)
			}
			// Store-boundary defaults must reach the response unchanged.
			if resp.TncIngressRateHz == 0 || resp.TncIngressBurst == 0 {
				t.Errorf("rate defaults not applied: %+v", resp)
			}
		})
	}
}

// TestUpdateKissInterfaceMode mirrors the create path for PUT.
func TestUpdateKissInterfaceMode(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	createBody, _ := json.Marshal(map[string]any{"type": "tcp", "tcp_port": 19001, "channel": 1})
	createReq := httptest.NewRequest(http.MethodPost, "/api/kiss", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", createRec.Code, createRec.Body.String())
	}
	var created dto.KissResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	// Flip to TNC mode with custom rate fields.
	upd, _ := json.Marshal(map[string]any{
		"type": "tcp", "tcp_port": 19001, "channel": 1,
		"mode": "tnc", "tnc_ingress_rate_hz": 40, "tnc_ingress_burst": 80,
	})
	updReq := httptest.NewRequest(http.MethodPut, "/api/kiss/"+itoa(created.ID), bytes.NewReader(upd))
	updReq.Header.Set("Content-Type", "application/json")
	updRec := httptest.NewRecorder()
	mux.ServeHTTP(updRec, updReq)
	if updRec.Code != http.StatusOK {
		t.Fatalf("update status = %d: %s", updRec.Code, updRec.Body.String())
	}
	var got dto.KissResponse
	if err := json.NewDecoder(updRec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Mode != configstore.KissModeTnc || got.TncIngressRateHz != 40 || got.TncIngressBurst != 80 {
		t.Errorf("update did not persist fields: %+v", got)
	}

	// Invalid mode on update must also be rejected.
	bad, _ := json.Marshal(map[string]any{"type": "tcp", "tcp_port": 19001, "channel": 1, "mode": "junk"})
	badReq := httptest.NewRequest(http.MethodPut, "/api/kiss/"+itoa(created.ID), bytes.NewReader(bad))
	badReq.Header.Set("Content-Type", "application/json")
	badRec := httptest.NewRecorder()
	mux.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid mode, got %d: %s", badRec.Code, badRec.Body.String())
	}
	if !strings.Contains(badRec.Body.String(), "mode") {
		t.Errorf("error body does not mention mode: %s", badRec.Body.String())
	}
}

// itoa avoids pulling strconv into every call site for a single decimal
// format. uint32 values are bounded so the manual conversion is safe.
func itoa(n uint32) string {
	if n == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// TestCreateKissTcpClient verifies the create path accepts a well-formed
// tcp-client payload and rejects missing remote_host / remote_port.
func TestCreateKissTcpClient(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// Valid tcp-client.
	body, _ := json.Marshal(map[string]any{
		"type":        "tcp-client",
		"remote_host": "lora.example.com",
		"remote_port": 8001,
		"channel":     1,
	})
	rec := doPost(mux, "/api/kiss", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create tcp-client status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp dto.KissResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Type != "tcp-client" || resp.RemoteHost != "lora.example.com" || resp.RemotePort != 8001 {
		t.Errorf("unexpected response fields: %+v", resp)
	}

	// Missing remote_host → 400.
	bad, _ := json.Marshal(map[string]any{
		"type":        "tcp-client",
		"remote_port": 8001,
		"channel":     1,
	})
	rec = doPost(mux, "/api/kiss", bad)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing remote_host, got %d", rec.Code)
	}

	// Missing remote_port → 400.
	bad, _ = json.Marshal(map[string]any{
		"type":        "tcp-client",
		"remote_host": "h",
		"channel":     1,
	})
	rec = doPost(mux, "/api/kiss", bad)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing remote_port, got %d", rec.Code)
	}
}

// TestKissReconnect_Endpoint verifies the POST /api/kiss/{id}/reconnect
// endpoint's response matrix: 404 for unknown id, 409 for non-tcp-client
// row, 409 for configured-but-not-running (manager-level), 200 when a
// tcp-client supervisor is registered.
func TestKissReconnect_Endpoint(t *testing.T) {
	srv, _ := newTestServer(t)
	// Wire a kiss.Manager so the reconnect handler has something to
	// dispatch against.
	mgr := kiss.NewManager(kiss.ManagerConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	srv.kissManager = mgr
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		mgr.StopAll()
	})
	srv.kissCtx = ctx

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// 404: unknown id.
	rec := doPost(mux, "/api/kiss/999/reconnect", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown id: want 404, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Create a server-listen tcp row; Reconnect should 409 it.
	body, _ := json.Marshal(map[string]any{
		"type": "tcp", "tcp_port": 28001, "channel": 1,
	})
	if rr := doPost(mux, "/api/kiss", body); rr.Code != http.StatusCreated {
		t.Fatalf("create tcp: %d %s", rr.Code, rr.Body.String())
	}
	list := fetchList(t, mux)
	var tcpID uint32
	for _, r := range list {
		if r.Type == "tcp" {
			tcpID = r.ID
			break
		}
	}
	if tcpID == 0 {
		t.Fatalf("tcp row not found")
	}
	rec = doPost(mux, "/api/kiss/"+itoa(tcpID)+"/reconnect", nil)
	if rec.Code != http.StatusConflict {
		t.Errorf("non-tcp-client row reconnect: want 409, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Create a tcp-client row. notifyKissManager with a nil
	// kissManager would be a no-op, but we set it above — so this
	// actually starts a supervisor that will try to dial the bogus
	// address and enter backoff. The reconnect handler should return
	// 200 once the manager has the id registered.
	clientBody, _ := json.Marshal(map[string]any{
		"type":             "tcp-client",
		"remote_host":      "127.0.0.1",
		"remote_port":      1,
		"channel":          1,
		"reconnect_init_ms": 60000,
		"reconnect_max_ms":  60000,
	})
	if rr := doPost(mux, "/api/kiss", clientBody); rr.Code != http.StatusCreated {
		t.Fatalf("create tcp-client: %d %s", rr.Code, rr.Body.String())
	}
	list = fetchList(t, mux)
	var clientID uint32
	for _, r := range list {
		if r.Type == "tcp-client" {
			clientID = r.ID
			break
		}
	}
	if clientID == 0 {
		t.Fatalf("tcp-client row not found in list")
	}
	// Wait for manager to register the client (StartClient is async
	// from notifyKissManager).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := mgr.Status()[clientID]; ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	rec = doPost(mux, "/api/kiss/"+itoa(clientID)+"/reconnect", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("tcp-client reconnect with manager: want 200, got %d body=%s",
			rec.Code, rec.Body.String())
	}
}

// doPost is a small request helper so each test case stays readable.
func doPost(mux *http.ServeMux, path string, body []byte) *httptest.ResponseRecorder {
	var r *http.Request
	if body == nil {
		r = httptest.NewRequest(http.MethodPost, path, nil)
	} else {
		r = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

// fetchList pulls the full /api/kiss list for assertion access.
func fetchList(t *testing.T, mux *http.ServeMux) []dto.KissResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/kiss", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status %d", rec.Code)
	}
	var list []dto.KissResponse
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return list
}
