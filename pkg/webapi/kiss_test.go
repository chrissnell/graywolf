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
	"sync"
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

// TestSetKissEnabled exercises the focused enable/disable toggle. A
// freshly-created interface is enabled; PUT /enabled flips the flag,
// persists it, and reflects it in the response. A missing id is 404.
func TestSetKissEnabled(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	createBody, _ := json.Marshal(map[string]any{"type": "tcp", "tcp_port": 20001, "channel": 1})
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
	if !created.Enabled {
		t.Fatalf("newly created interface should be enabled, got %+v", created)
	}

	toggle := func(enabled bool) dto.KissResponse {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"enabled": enabled})
		req := httptest.NewRequest(http.MethodPut, "/api/kiss/"+itoa(created.ID)+"/enabled", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("toggle(%v) status = %d: %s", enabled, rec.Code, rec.Body.String())
		}
		var resp dto.KissResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		return resp
	}

	resp := toggle(false)
	if resp.Enabled {
		t.Errorf("after disable, response Enabled=true: %+v", resp)
	}
	if row, err := srv.store.GetKissInterface(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	} else if row.Enabled {
		t.Errorf("after disable, stored Enabled=true")
	}

	resp = toggle(true)
	if !resp.Enabled {
		t.Errorf("after enable, response Enabled=false: %+v", resp)
	}
	if row, err := srv.store.GetKissInterface(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	} else if !row.Enabled {
		t.Errorf("after enable, stored Enabled=false")
	}

	// Unknown id -> 404.
	body, _ := json.Marshal(map[string]any{"enabled": false})
	missReq := httptest.NewRequest(http.MethodPut, "/api/kiss/999999/enabled", bytes.NewReader(body))
	missReq.Header.Set("Content-Type", "application/json")
	missRec := httptest.NewRecorder()
	mux.ServeHTTP(missRec, missReq)
	if missRec.Code != http.StatusNotFound {
		t.Errorf("missing id status = %d, want 404", missRec.Code)
	}
}

// TestUpdateKissEnabledPreserved checks the full-resource PUT contract:
// an explicit enabled=false disables the row, while a PUT that omits the
// field defaults back to enabled (older-client backward compat).
func TestUpdateKissEnabledPreserved(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	createBody, _ := json.Marshal(map[string]any{"type": "tcp", "tcp_port": 20011, "channel": 1})
	createReq := httptest.NewRequest(http.MethodPost, "/api/kiss", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)
	var created dto.KissResponse
	_ = json.NewDecoder(createRec.Body).Decode(&created)

	put := func(body map[string]any) dto.KissResponse {
		t.Helper()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPut, "/api/kiss/"+itoa(created.ID), bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("put status = %d: %s", rec.Code, rec.Body.String())
		}
		var resp dto.KissResponse
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		return resp
	}

	if got := put(map[string]any{"type": "tcp", "tcp_port": 20011, "channel": 1, "enabled": false}); got.Enabled {
		t.Errorf("explicit enabled=false should disable, got %+v", got)
	}
	if got := put(map[string]any{"type": "tcp", "tcp_port": 20011, "channel": 1}); !got.Enabled {
		t.Errorf("omitted enabled should default to true, got %+v", got)
	}
}

// TestCreateKissDisabled pins the create-path contract for the gorm
// `default:true` footgun: POST with enabled=false must persist a disabled
// row, not silently flip it to enabled. (db.Create applies the column
// default for a Go zero-value bool, so the store re-asserts the false.)
func TestCreateKissDisabled(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]any{"type": "tcp", "tcp_port": 20021, "channel": 1, "enabled": false})
	req := httptest.NewRequest(http.MethodPost, "/api/kiss", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.KissResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Enabled {
		t.Errorf("POST enabled=false returned Enabled=true: %+v", resp)
	}
	row, err := srv.store.GetKissInterface(context.Background(), resp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Enabled {
		t.Errorf("POST enabled=false persisted Enabled=true (gorm default trap)")
	}
}

// blockingRWC is an in-memory io.ReadWriteCloser whose Read blocks until
// Close so a SerialSupervisor started over it stays in StateConnected
// until ctx cancel / explicit close — mirrors pkg/kiss's fakeRWC.
type blockingRWC struct {
	closed chan struct{}
	once   sync.Once
}

func newBlockingRWC() *blockingRWC { return &blockingRWC{closed: make(chan struct{})} }

func (b *blockingRWC) Read([]byte) (int, error)    { <-b.closed; return 0, io.EOF }
func (b *blockingRWC) Write(p []byte) (int, error) { return len(p), nil }
func (b *blockingRWC) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

// TestSetKissEnabledStartsSerialFamily is the regression guard for the
// notifyKissManager dispatch fix: enabling a disabled serial / bluetooth
// / usbserial interface must actually (re)start its supervisor — not fall
// into the default branch that only calls Stop — and disabling must
// release it. The bug it guards against silently left Bluetooth/USB
// interfaces down after a re-enable (the manager's switch lacked those
// cases, so they hit `default: Stop`). A fake OpenFunc stands in for the
// platform serial opener so no real device is touched.
func TestSetKissEnabledStartsSerialFamily(t *testing.T) {
	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	mgr := kiss.NewManager(kiss.ManagerConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(mgr.StopAll)

	openFn := func(string, uint32) (io.ReadWriteCloser, error) { return newBlockingRWC(), nil }

	srv, err := NewServer(Config{
		Store:              store,
		KissManager:        mgr,
		KissSerialOpenFunc: openFn,
		Logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	cases := []configstore.KissInterface{
		{Name: "k-serial", InterfaceType: configstore.KissTypeSerial, Device: "/dev/ttyUSB0", BaudRate: 9600, Channel: 1, Mode: configstore.KissModeModem, Enabled: false},
		{Name: "k-bt", InterfaceType: configstore.KissTypeBluetooth, Device: "AA:BB:CC:DD:EE:FF", Channel: 1, Mode: configstore.KissModeTnc, Enabled: false},
		{Name: "k-usb", InterfaceType: configstore.KissTypeUsbSerial, Device: "2341:0043", BaudRate: 9600, Channel: 1, Mode: configstore.KissModeModem, Enabled: false},
	}
	setEnabled := func(t *testing.T, id uint32, enabled bool) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"enabled": enabled})
		req := httptest.NewRequest(http.MethodPut, "/api/kiss/"+itoa(id)+"/enabled", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("set enabled=%v status = %d: %s", enabled, rec.Code, rec.Body.String())
		}
	}
	for _, ki := range cases {
		ki := ki
		t.Run(ki.InterfaceType, func(t *testing.T) {
			if err := store.CreateKissInterface(context.Background(), &ki); err != nil {
				t.Fatal(err)
			}

			// Enable: the supervisor must come up (StateConnected via the
			// fake opener). Before the fix, bluetooth/usbserial hit
			// `default: Stop` and never appeared in Status().
			setEnabled(t, ki.ID, true)
			deadline := time.Now().Add(2 * time.Second)
			var got string
			for time.Now().Before(deadline) {
				if st, ok := mgr.Status()[ki.ID]; ok {
					got = st.State
					if got == kiss.StateConnected {
						break
					}
				}
				time.Sleep(2 * time.Millisecond)
			}
			if got != kiss.StateConnected {
				t.Fatalf("after enable, manager state = %q, want %q", got, kiss.StateConnected)
			}

			// Disable: the interface must be torn down and removed from the
			// manager (device released).
			setEnabled(t, ki.ID, false)
			deadline = time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				if _, ok := mgr.Status()[ki.ID]; !ok {
					return
				}
				time.Sleep(2 * time.Millisecond)
			}
			t.Fatalf("after disable, interface %d still present in manager", ki.ID)
		})
	}
}

// TestCreateKissTcpClient verifies the create path accepts a well-formed
// tcp-client payload and rejects missing remote_host / remote_port.
func TestCreateKissTcpClient(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// Valid tcp-client. Channel 1 is the modem-backed seed channel, so
	// pin mode=modem explicitly: a bare tcp-client now defaults to a
	// TX-capable TNC link (issue #128), which the dual-backend
	// invariant correctly refuses on a modem channel. This test only
	// exercises tcp-client field plumbing; the TX default is covered by
	// the dto/store/migration unit tests.
	body, _ := json.Marshal(map[string]any{
		"type":        "tcp-client",
		"remote_host": "lora.example.com",
		"remote_port": 8001,
		"channel":     1,
		"mode":        "modem",
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
	// Pin mode=modem: channel 1 is modem-backed and a bare tcp-client
	// now defaults to a TX-capable TNC link (issue #128), which the
	// dual-backend invariant refuses on a modem channel. This test
	// exercises the reconnect supervisor, not the TX default.
	clientBody, _ := json.Marshal(map[string]any{
		"type":              "tcp-client",
		"remote_host":       "127.0.0.1",
		"remote_port":       1,
		"channel":           1,
		"mode":              "modem",
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

// newSerialTestServer returns a *Server wired with a real kiss.Manager and the
// bogus device path used by serial KISS tests. The manager context and cleanup
// are registered on t. Callers receive the server and a ready-to-use ServeMux.
func newSerialTestServer(t *testing.T) (*Server, *kiss.Manager, *http.ServeMux) {
	t.Helper()
	srv, _ := newTestServer(t)
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
	return srv, mgr, mux
}

// pollRegistered polls mgr.Status() until id is present or deadline expires.
// Returns true when the id appears before the deadline.
func pollRegistered(mgr *kiss.Manager, id uint32, deadline time.Time) bool {
	for time.Now().Before(deadline) {
		if _, ok := mgr.Status()[id]; ok {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// pollAbsent polls mgr.Status() until id is absent or deadline expires.
// Returns true when the id is gone before the deadline.
func pollAbsent(mgr *kiss.Manager, id uint32, deadline time.Time) bool {
	for time.Now().Before(deadline) {
		if _, ok := mgr.Status()[id]; !ok {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// TestNotifyKissManager_Serial verifies the initial dispatch path: a valid
// serial KISS config causes the manager to register the supervisor (step A).
// This is the load-bearing discriminator for the case configstore.KissTypeSerial
// arm — without it, notifyKissManager falls through to the default: arm which
// calls Stop(), so the registration can never succeed.
//
// Mirrors the tcp-client pattern in TestKissReconnect_Endpoint: a real
// kiss.Manager is wired in; the supervisor tries to open the bogus device path
// and enters reconnect backoff — that is fine; we only assert the row appears
// in manager Status(). No real serial hardware is required.
func TestNotifyKissManager_Serial(t *testing.T) {
	_, mgr, mux := newSerialTestServer(t)

	// POST a valid serial interface; device is bogus so the supervisor
	// enters backoff, but the manager row is registered immediately.
	body, _ := json.Marshal(map[string]any{
		"type":              "serial",
		"serial_device":     "/dev/ttyACM-graywolf-test-nonexistent",
		"baud_rate":         9600,
		"channel":           1,
		"mode":              "modem",
		"reconnect_init_ms": 60000,
		"reconnect_max_ms":  60000,
	})
	rr := doPost(mux, "/api/kiss", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create serial: %d %s", rr.Code, rr.Body.String())
	}
	var created dto.KissResponse
	if err := json.NewDecoder(rr.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Type != "serial" {
		t.Fatalf("created type = %q, want serial", created.Type)
	}

	// A: valid first dispatch → id must appear in Status().
	// Without the case KissTypeSerial arm, this assertion fails because
	// the default: arm calls Stop(), which is a no-op for a fresh id and
	// never registers it.
	if !pollRegistered(mgr, created.ID, time.Now().Add(2*time.Second)) {
		t.Errorf("serial interface %d not registered in manager after notifyKissManager (A); Status=%v",
			created.ID, mgr.Status())
	}
}

// TestNotifyKissManager_SerialReload verifies the two hot-reload semantics that
// are the whole point of the case configstore.KissTypeSerial arm:
//
//	B. Blank-device reload on a RUNNING serial row → manager must Stop it
//	   (the arm's invalid-config guard fires and removes the supervisor).
//	C. Edit of a RUNNING serial row with a still-valid config (e.g. BaudRate
//	   change) → manager must keep the row registered (StartSerial
//	   stops-then-restarts under the same id, so the row is never dropped).
//
// Step A (valid registration) is a prerequisite for both B and C: it is the
// discriminating assertion — without the arm the registration never happens,
// so the test fails at A before B or C can run. That sequencing means the
// full test fails when the arm is absent, even if B's blank-device Stop would
// otherwise be reachable via the default: arm.
func TestNotifyKissManager_SerialReload(t *testing.T) {
	srv, mgr, mux := newSerialTestServer(t)

	const bogusDev = "/dev/ttyACM-graywolf-test-nonexistent"

	// — A: bring up a valid serial row —
	body, _ := json.Marshal(map[string]any{
		"type":              "serial",
		"serial_device":     bogusDev,
		"baud_rate":         9600,
		"channel":           1,
		"mode":              "modem",
		"reconnect_init_ms": 60000,
		"reconnect_max_ms":  60000,
	})
	rr := doPost(mux, "/api/kiss", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create serial: %d %s", rr.Code, rr.Body.String())
	}
	var created dto.KissResponse
	if err := json.NewDecoder(rr.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	// A: valid dispatch → must be registered (prerequisite for B and C;
	// fails without the arm — see function-level comment).
	if !pollRegistered(mgr, created.ID, time.Now().Add(2*time.Second)) {
		t.Fatalf("serial %d not registered after initial dispatch (A); Status=%v",
			created.ID, mgr.Status())
	}

	// — B: blank-device hot-reload on a RUNNING row → must be removed —
	// The HTTP layer rejects an empty device, so call notifyKissManager
	// directly to simulate an operator clearing the device field via a
	// path that bypasses HTTP validation (e.g. direct store write or a
	// future batch-edit API). The arm's guard ( if ki.Device==""... )
	// fires Stop(id), which synchronously removes the entry from running.
	blankDevice := configstore.KissInterface{
		ID:            created.ID,
		InterfaceType: configstore.KissTypeSerial,
		Name:          "serial-reload-test",
		Device:        "", // invalid — triggers stop
		BaudRate:      9600,
		Channel:       1,
		Enabled:       true,
	}
	srv.notifyKissManager(blankDevice)

	// Stop() removes the entry synchronously; poll confirms removal.
	if !pollAbsent(mgr, created.ID, time.Now().Add(2*time.Second)) {
		t.Errorf("serial %d still registered after blank-device reload (B); Status=%v",
			created.ID, mgr.Status())
	}

	// — Re-register for step C —
	// Call notifyKissManager directly with the original valid config so we
	// can exercise the edit path without a second HTTP round-trip.
	valid := configstore.KissInterface{
		ID:            created.ID,
		InterfaceType: configstore.KissTypeSerial,
		Name:          "serial-reload-test",
		Device:        bogusDev,
		BaudRate:      9600,
		Channel:       1,
		Enabled:       true,
	}
	srv.notifyKissManager(valid)
	if !pollRegistered(mgr, created.ID, time.Now().Add(2*time.Second)) {
		t.Fatalf("serial %d not re-registered after restore (C-pre); Status=%v",
			created.ID, mgr.Status())
	}

	// — C: valid edit (BaudRate change) on a RUNNING row → must stay registered —
	// StartSerial stops-then-restarts under the same id, so the manager
	// row is never dropped. Without the arm, notifyKissManager hits
	// default: → Stop(), and the row would be absent after the call.
	edited := configstore.KissInterface{
		ID:            created.ID,
		InterfaceType: configstore.KissTypeSerial,
		Name:          "serial-reload-test",
		Device:        bogusDev,
		BaudRate:      19200, // changed field
		Channel:       1,
		Enabled:       true,
	}
	srv.notifyKissManager(edited)

	if !pollRegistered(mgr, created.ID, time.Now().Add(2*time.Second)) {
		t.Errorf("serial %d not registered after valid-edit hot-reload (C); Status=%v",
			created.ID, mgr.Status())
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
