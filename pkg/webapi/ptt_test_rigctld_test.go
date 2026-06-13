package webapi

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// fakeRigctld listens on a loopback port and speaks the real rigctld
// line protocol for the handful of commands the probe exercises:
//   - `t`   (get_ptt) → a single value line, e.g. "0\n", with NO
//     trailing "RPRT 0" (matching a healthy daemon).
//   - `T 1`/`T 0`     → "RPRT 0\n".
//
// rprtErr, when non-zero, makes `t` reply with "RPRT <n>" instead of a
// value, simulating rigctld-up-radio-down.
func fakeRigctld(t *testing.T, rprtErr int) (host string, port uint16) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					cmd := strings.TrimSpace(line)
					switch {
					case cmd == "t" && rprtErr != 0:
						_, _ = c.Write([]byte("RPRT " + strconv.Itoa(rprtErr) + "\n"))
					case cmd == "t":
						_, _ = c.Write([]byte("0\n"))
					case strings.HasPrefix(cmd, "T "):
						_, _ = c.Write([]byte("RPRT 0\n"))
					default:
						_, _ = c.Write([]byte("RPRT -1\n"))
					}
				}
			}(conn)
		}
	}()

	h, p, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	pn, err := strconv.Atoi(p)
	if err != nil {
		t.Fatalf("atoi port: %v", err)
	}
	return h, uint16(pn)
}

func postTestRigctld(t *testing.T, host string, port uint16) dto.TestRigctldResponse {
	t.Helper()
	s := &Server{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	body, _ := json.Marshal(dto.TestRigctldRequest{Host: host, Port: port})
	req := httptest.NewRequest(http.MethodPost, "/api/ptt/test-rigctld", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.handleTestRigctld(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp dto.TestRigctldResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	return resp
}

// GRA-73 regression: a healthy rigctld answers `t` with only the state
// value (no trailing "RPRT 0"). The probe must report success rather
// than blocking on a second line and timing out.
func TestHandleTestRigctld_SingleLineGetPttSucceeds(t *testing.T) {
	host, port := fakeRigctld(t, 0)
	resp := postTestRigctld(t, host, port)
	if !resp.OK {
		t.Fatalf("expected OK against single-line get_ptt, got OK=false message=%q", resp.Message)
	}
}

func TestHandleTestRigctld_HamlibErrorReported(t *testing.T) {
	host, port := fakeRigctld(t, -6)
	resp := postTestRigctld(t, host, port)
	if resp.OK {
		t.Fatalf("expected failure on RPRT -6, got OK=true")
	}
	if !strings.Contains(resp.Message, "-6") {
		t.Fatalf("expected message to surface code -6, got %q", resp.Message)
	}
}

// A reply that is neither a state value nor a parseable RPRT must be
// reported as a protocol mismatch (the sanitized snippet), not silently
// accepted. Guards the single-line parse's fall-through branch.
func TestHandleTestRigctld_UnexpectedResponse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				if _, err := r.ReadString('\n'); err != nil {
					return
				}
				_, _ = c.Write([]byte("BOGUS\n"))
			}(conn)
		}
	}()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	pn, _ := strconv.Atoi(p)

	resp := postTestRigctld(t, h, uint16(pn))
	if resp.OK {
		t.Fatalf("expected failure on garbage reply, got OK=true")
	}
	if !strings.Contains(resp.Message, "unexpected response") || !strings.Contains(resp.Message, "BOGUS") {
		t.Fatalf("expected protocol-mismatch message, got %q", resp.Message)
	}
}

func TestHandleTestRigctld_ConnectionRefused(t *testing.T) {
	// Bind then immediately close to obtain a port nothing is listening on.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_, p, _ := net.SplitHostPort(ln.Addr().String())
	_ = ln.Close()
	pn, _ := strconv.Atoi(p)

	resp := postTestRigctld(t, "127.0.0.1", uint16(pn))
	if resp.OK {
		t.Fatalf("expected failure against closed port, got OK=true")
	}
	if !strings.Contains(resp.Message, "connection failed") {
		t.Fatalf("expected connection-failed message, got %q", resp.Message)
	}
}
