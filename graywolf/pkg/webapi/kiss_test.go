package webapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
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
