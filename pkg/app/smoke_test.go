//go:build !windows

package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestAppRunSmoke exercises the full composition root: ParseFlags → New →
// Run against a fake modem binary and an in-process SQLite DB. It is the
// only test in the project that covers the real wireServices → Start →
// Stop path end-to-end, so it catches wiring regressions nothing else
// will (component ordering, the modem-version query, HTTP server
// actually binding, bridge handshake really completing, shutdown
// unblocking).
//
// The test does NOT assert anything about radio traffic, packet
// decoding, or iGate behavior — those live in their own unit tests. The
// acceptance criteria are:
//
//  1. App.Run returns cleanly (nil error) after context cancel.
//  2. The HTTP server was reachable before cancel.
//  3. The fake modem child process was reaped (no zombies).
//
// Guarded by !windows: the fake modem uses Unix domain sockets and
// POSIX signal semantics. The app package already exercises its
// Windows-specific code paths elsewhere; this is an integration test
// for the POSIX build.
func TestAppRunSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("slow test; skipped in -short mode")
	}

	bin := buildSmokeModem(t)

	// Short-pathed socket dir: on macOS sun_path is capped at 104 bytes,
	// and t.TempDir() under /var/folders/... routinely blows that.
	socketDir, err := os.MkdirTemp("/tmp", "gw-smoke-sock-")
	if err != nil {
		t.Fatalf("mkdir sock: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })

	dbPath := filepath.Join(t.TempDir(), "graywolf-smoke.db")
	httpPort := pickFreePort(t)
	httpAddr := fmt.Sprintf("127.0.0.1:%d", httpPort)

	cfg := Config{
		DBPath:          dbPath,
		ModemPath:       bin,
		HTTPAddr:        httpAddr,
		ShutdownTimeout: 5 * time.Second,
		Version:         "smoke",
		GitCommit:       "test",
	}

	// Override the socket dir the supervisor will pass to the modem
	// so the fake modem's listen path is /tmp/... and fits in sun_path.
	// This is done via GRAYWOLF_MODEM_SOCKET_DIR which the app doesn't
	// actually read today — the configuration path goes through
	// modembridge.Config.SocketDir, which is populated from a default
	// inside modembridge.applyDefaults when empty. The App doesn't
	// surface a way to override that path directly, so we set
	// TMPDIR for this test process instead: os.TempDir() honors
	// TMPDIR on Unix, and applyDefaults falls back to os.TempDir().
	t.Setenv("TMPDIR", socketDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))

	runErr := make(chan error, 1)
	go func() { runErr <- a.Run(ctx) }()

	// Poll the public /api/version endpoint until it answers. That
	// proves the HTTP server is serving and, transitively, that every
	// component wireServices installed came up without a wiring error.
	// Some endpoints are behind auth; /api/version is intentionally
	// public so the UI can read it before login.
	if err := waitForHTTPReady(t, httpAddr, 5*time.Second); err != nil {
		cancel()
		<-runErr
		t.Fatalf("http never became ready: %v", err)
	}

	// Trigger graceful shutdown.
	cancel()

	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("App.Run returned error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("App.Run did not return within 10s of cancel")
	}

	// Best-effort zombie check. The supervisor uses exec.CommandContext +
	// cmd.Wait so children should be reaped synchronously, but if the
	// fake modem hangs on SIGTERM and the supervisor timed out, we'd
	// still be holding a zombie. Probe via `ps` for any fake-modem
	// child whose parent is us. This is intentionally Linux/macOS
	// specific (gated by !windows) and best-effort: a failure of the
	// ps call is logged, not fatal.
	assertNoZombies(t, bin)
}

// pickFreePort asks the kernel for an unused TCP port by opening and
// immediately closing a listener on 127.0.0.1:0. The window between
// close and use is small enough in practice that collisions are rare
// under go test's default serialization; if this ever flakes, switch
// to handing an already-bound listener into the App rather than a
// string address.
func pickFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pick port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// waitForHTTPReady polls the public /api/version endpoint until it
// returns 200 or the deadline expires. Uses a fresh http.Client per
// call with a short per-request timeout so a stalled server doesn't
// hold the test open past the outer deadline.
func waitForHTTPReady(t *testing.T, addr string, deadline time.Duration) error {
	t.Helper()
	url := "http://" + addr + "/api/version"
	client := &http.Client{Timeout: 500 * time.Millisecond}
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s", url)
}

// assertNoZombies pgreps for our fake-modem binary with ppid==self.
// If anything is still running, the supervisor didn't reap it and
// we log a fail. Pure best-effort — if ps isn't available or the
// output format is something we don't understand, this is a no-op
// with a log line rather than a failure.
func assertNoZombies(t *testing.T, bin string) {
	t.Helper()
	pgrep, err := exec.LookPath("pgrep")
	if err != nil {
		t.Logf("pgrep not on PATH; skipping zombie check")
		return
	}
	// pgrep -P <ppid> -f <binary>: children of the test process whose
	// command line matches the fake-modem binary path.
	out, err := exec.Command(pgrep, "-P", fmt.Sprintf("%d", os.Getpid()), "-f", bin).Output()
	if err != nil {
		// exit status 1 is "no matches" which is the happy path.
		return
	}
	if len(out) > 0 {
		t.Errorf("fake modem child(ren) still alive after App.Run returned:\n%s", out)
	}
}

// buildSmokeModem compiles a Go program that impersonates graywolf-modem
// well enough to let the App's full startup path succeed. It differs
// from the bridge_restart_leak_test fake modem in two ways:
//
//  1. It implements --version. ResolveModemPath runs `<bin> --version`
//     early in wireServices and would otherwise fall through to the
//     non-fatal "unknown" branch. Emitting the matching version keeps
//     the startup banner clean and catches any future code that makes
//     version-query failure fatal.
//
//  2. It handles multiple IPC sessions. The supervisor restarts the
//     modem child on any session error; for a short-lived smoke test
//     one session is enough, but the fake modem must not exit early
//     in a way that would race the HTTP readiness check.
func buildSmokeModem(t *testing.T) string {
	t.Helper()
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "main.go")
	if err := os.WriteFile(srcPath, []byte(smokeModemSource), 0o644); err != nil {
		t.Fatalf("write fake modem source: %v", err)
	}
	binPath := filepath.Join(srcDir, "fake-modem")
	cmd := exec.Command("go", "build", "-o", binPath, srcPath)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build fake modem: %v\n%s", err, out)
	}
	return binPath
}

// smokeModemSource is the fake graywolf-modem used by TestAppRunSmoke.
// Compiled at test time so it always matches the current ipcproto
// contract without needing an out-of-tree testdata binary.
//
// Keep it minimal: speak enough of the IPC protocol that runSession
// advances past its handshake and configure phases, then block on
// frame reads until Shutdown or the connection closes.
const smokeModemSource = `package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/protobuf/proto"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

func writeFrame(w io.Writer, msg *pb.IpcMessage) error {
	buf, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(buf)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(buf)
	return err
}

func readFrame(r io.Reader) (*pb.IpcMessage, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	m := &pb.IpcMessage{}
	if err := proto.Unmarshal(buf, m); err != nil {
		return nil, err
	}
	return m, nil
}

func main() {
	// --version: the app queries this during early startup. Answer
	// with the smoke-test version string so the banner's equality
	// check passes and we don't get a "versions disagree" warning.
	if len(os.Args) >= 2 && os.Args[1] == "--version" {
		fmt.Println("vsmoke-test")
		return
	}
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "fake-modem: missing socket path")
		os.Exit(2)
	}
	addr := os.Args[1]

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigs
		os.Exit(0)
	}()

	l, err := net.Listen("unix", addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake-modem: listen:", err)
		os.Exit(1)
	}

	// Readiness byte.
	fmt.Println()
	os.Stdout.Sync()

	conn, err := l.Accept()
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake-modem: accept:", err)
		os.Exit(1)
	}
	defer conn.Close()

	if err := writeFrame(conn, &pb.IpcMessage{Payload: &pb.IpcMessage_ModemReady{
		ModemReady: &pb.ModemReady{Version: "vsmoke-test", Pid: uint64(os.Getpid())},
	}}); err != nil {
		return
	}

	// Drain frames until shutdown or EOF. The bridge might send
	// ConfigureAudio/ConfigureChannel/ConfigurePtt/StartAudio; we
	// happily accept them all.
	for {
		m, err := readFrame(conn)
		if err != nil {
			return
		}
		if m.GetShutdown() != nil {
			// Acknowledge shutdown with a final StatusUpdate so the
			// session read loop observes a clean EOF after the next
			// read. Then close.
			_ = writeFrame(conn, &pb.IpcMessage{Payload: &pb.IpcMessage_StatusUpdate{
				StatusUpdate: &pb.StatusUpdate{ShutdownComplete: true},
			}})
			return
		}
	}
}
`
