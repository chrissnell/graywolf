package webapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/kiss"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// recordingSink stands in for the TX governor. Submit always succeeds so
// the server reaches the gate-hook branch of dispatchDataFrame.
type recordingSink struct{ submits atomic.Int32 }

func (s *recordingSink) Submit(context.Context, uint32, *ax25.Frame, txgovernor.SubmitSource) error {
	s.submits.Add(1)
	return nil
}

// freeTCPPort returns a port that was bindable a moment ago. Good enough
// for a test that immediately rebinds it via the KISS manager.
func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe for free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// TestNotifyKissManager_TCPThreadsGateTxToIs is the regression test for
// the tcp (server-listen) arm of notifyKissManager dropping GateTxToIs.
//
// The flag only ever reached a running server through the boot path in
// pkg/app/wiring.go, so an operator who checked "also forward
// transmissions from connected clients to APRS-IS" and saved got a
// server restarted with the flag at its zero value. RF TX kept working
// (that leg goes through Sink.Submit, which does not consult the flag),
// so the failure was invisible except as packets silently never reaching
// APRS-IS until the next graywolf restart re-ran the boot path.
//
// End-to-end on purpose: a struct-literal omission is exactly the kind of
// bug a field-by-field mock assertion can be written to miss, so this
// drives a real socket and asserts the observable outcome — the gate hook
// fires — instead of inspecting the config that was passed.
func TestNotifyKissManager_TCPThreadsGateTxToIs(t *testing.T) {
	srv, _ := newTestServer(t)

	sink := &recordingSink{}
	var gated atomic.Int32
	mgr := kiss.NewManager(kiss.ManagerConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sink:   sink,
		OnClientTxAccepted: func(context.Context, uint32, uint32, *ax25.Frame) {
			gated.Add(1)
		},
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

	// Create a modem-mode tcp server-listen interface with the gate on.
	// createKiss dispatches through notifyKissManager, the same path a
	// later PUT takes, so the create alone reproduces the bug.
	port := freeTCPPort(t)
	body, _ := json.Marshal(map[string]any{
		"type":          "tcp",
		"tcp_port":      port,
		"local_only":    true,
		"channel":       1,
		"mode":          "modem",
		"gate_tx_to_is": true,
	})
	rr := doPost(mux, "/api/kiss", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create tcp interface: %d %s", rr.Code, rr.Body.String())
	}
	var created dto.KissResponse
	if err := json.NewDecoder(rr.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if !created.GateTxToIs {
		t.Fatalf("persisted gate_tx_to_is = false, want true (store/DTO regression, not the manager)")
	}

	// Dial the listener the manager just brought up. It binds on its own
	// goroutine, so retry until the connect succeeds.
	var conn net.Conn
	addr := "127.0.0.1:" + strconv.Itoa(port)
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		c, err := net.Dial("tcp", addr)
		if err == nil {
			conn = c
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if conn == nil {
		t.Fatalf("kiss server never became connectable on %s", addr)
	}
	defer conn.Close()

	// Send one APRS message frame, the traffic shape from the bug report.
	src, err := ax25.ParseAddress("N0CALL-5")
	if err != nil {
		t.Fatal(err)
	}
	dst, err := ax25.ParseAddress("APRS")
	if err != nil {
		t.Fatal(err)
	}
	f, err := ax25.NewUIFrame(src, dst, nil, []byte(":TEST     :ping{01"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(kiss.Encode(0, raw)); err != nil {
		t.Fatalf("write kiss frame: %v", err)
	}

	// The RF leg is independent of the flag; assert it first so a failure
	// here points at the TX path rather than at the gate.
	if !pollCount(&sink.submits, 1, time.Now().Add(2*time.Second)) {
		t.Fatalf("frame never reached the TX sink (submits=%d)", sink.submits.Load())
	}
	if !pollCount(&gated, 1, time.Now().Add(2*time.Second)) {
		t.Errorf("gate hook never fired: notifyKissManager dropped GateTxToIs " +
			"for the tcp arm, so APRS-IS forwarding is off until the next restart")
	}
}

// pollCount waits for an atomic counter to reach want before deadline.
func pollCount(c *atomic.Int32, want int32, deadline time.Time) bool {
	for time.Now().Before(deadline) {
		if c.Load() >= want {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}
