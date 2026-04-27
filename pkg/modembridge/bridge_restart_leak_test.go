//go:build !windows

package modembridge

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// TestBridgeRestartNoLeaks verifies that repeatedly starting and stopping the
// bridge does not accumulate goroutines. This is a regression test for the
// pre-work-order-01 leaks in the pending-reply dispatchers, the stdout drain
// goroutine, and the readDialAddr readiness handshake — three paths whose
// bugs only manifest when the full supervisor lifecycle actually spawns and
// reaps a real child process.
//
// The test drives a tiny Go-compiled fake modem binary (built once per test)
// that speaks just enough of the IPC framing protocol to get each session
// through ModemReady → empty configure → Shutdown → close. Each iteration
// is a fresh Bridge.New + Start + Stop; goroutine count after 20 cycles
// must not have drifted appreciably from the baseline captured after one
// warm-up run.
//
// Skipped in -short mode and on Windows (the fake-modem binary spawn and
// SIGTERM semantics used here are POSIX-specific; the modem IPC has its
// own Windows path elsewhere).
func TestBridgeRestartNoLeaks(t *testing.T) {
	if testing.Short() {
		t.Skip("slow test; skipped in -short mode")
	}

	bin := buildFakeModem(t)
	socketDir := shortTempDir(t)

	// Warm-up run lets any one-time goroutines spin up (metrics registry,
	// test-infra scheduler goroutines, etc.) so the baseline is honest.
	runBridgeOnce(t, bin, socketDir)
	settleGoroutines()
	baseline := runtime.NumGoroutine()

	const iterations = 20
	for i := 0; i < iterations; i++ {
		runBridgeOnce(t, bin, socketDir)
	}

	// Give any post-Stop cleanup a moment to complete. Goroutine exit is
	// eventually-consistent; poll rather than assume a fixed sleep suffices.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= baseline+5 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	got := runtime.NumGoroutine()
	// +5 tolerates runtime scheduling noise, the slog background flushers,
	// and any transient goroutines the runtime hasn't yet reaped. A real
	// leak is at least one new goroutine per iteration (20 on 20 restarts).
	if got > baseline+5 {
		dumpGoroutines(t)
		t.Fatalf("goroutine leak: baseline=%d after %d restarts=%d", baseline, iterations, got)
	}
}

// runBridgeOnce runs a single Start/Stop cycle with an empty configstore.
// The fake modem answers the handshake, the bridge settles briefly into
// StateRunning, and Stop drives a graceful shutdown.
func runBridgeOnce(t *testing.T, bin, socketDir string) {
	t.Helper()

	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer store.Close()

	b := New(Config{
		BinaryPath:       bin,
		SocketDir:        socketDir,
		Store:            store,
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		ReadinessTimeout: 3 * time.Second,
		ShutdownTimeout:  200 * time.Millisecond,
		FrameBufferSize:  16,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Give the session time to hand-shake and reach StateRunning. A tight
	// loop on b.State() would also work but is more fragile; 150ms is
	// comfortable for the in-process fake modem.
	time.Sleep(150 * time.Millisecond)
	b.Stop()
}

// shortTempDir returns a short-pathed tempdir suitable for holding a
// Unix domain socket. On macOS the sun_path field tops out at 104 bytes,
// and t.TempDir() produces paths well over that limit under
// /var/folders/... — so we mkdir under /tmp instead and register a
// cleanup hook.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "gw-bridge-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// settleGoroutines gives the runtime a beat to reap anything that just
// exited so the baseline snapshot is as quiet as possible.
func settleGoroutines() {
	for i := 0; i < 4; i++ {
		runtime.Gosched()
		time.Sleep(20 * time.Millisecond)
	}
}

// dumpGoroutines prints a goroutine profile to the test log on failure.
// Useful for pinpointing which stacks are leaking.
func dumpGoroutines(t *testing.T) {
	t.Helper()
	buf := make([]byte, 1<<16)
	n := runtime.Stack(buf, true)
	t.Logf("goroutine dump:\n%s", buf[:n])
}

// buildFakeModem compiles a single-file Go program that impersonates
// graywolf-modem to the extent this test needs: listen on the supplied
// Unix socket, write the '\n' readiness byte to stdout, accept one
// connection, send a ModemReady frame, read frames until it sees a
// Shutdown message (or EOF), and exit. Also exits cleanly on SIGTERM
// so terminateChild never blocks in cmd.Wait.
//
// The binary is compiled into t.TempDir() so the filesystem is cleaned up
// automatically. Building is the dominant setup cost (~1s on a cold
// cache); running the bridge cycle is milliseconds per iteration.
func buildFakeModem(t *testing.T) string {
	t.Helper()

	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "main.go")
	if err := os.WriteFile(srcPath, []byte(fakeModemSource), 0o644); err != nil {
		t.Fatalf("write fake modem source: %v", err)
	}

	binPath := filepath.Join(srcDir, "fake-modem")
	// Build with the current module context so the fake modem can import
	// pkg/ipcproto for the IpcMessage type. Running `go build` from the
	// test's working directory (inside pkg/modembridge) picks up the
	// module root automatically.
	cmd := exec.Command("go", "build", "-o", binPath, srcPath)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build fake modem: %v\n%s", err, out)
	}
	return binPath
}

// fakeModemSource is the Go program compiled on-the-fly by buildFakeModem.
// Kept as a string literal so it travels with the test and requires no
// separate testdata directory or build-tag gymnastics.
const fakeModemSource = `package main

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

// writeFrame writes one length-prefixed IPC frame. Matches the Rust/Go
// framing on the wire: 4-byte big-endian length, then the marshalled
// protobuf message.
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
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "fake-modem: missing socket path")
		os.Exit(2)
	}
	addr := os.Args[1]

	// SIGTERM from the supervisor's terminateChild exits cleanly so
	// cmd.Wait returns in the test promptly.
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

	// Readiness byte. The Go supervisor waits for exactly one '\n' before
	// dialing the socket.
	fmt.Println()
	os.Stdout.Sync()

	conn, err := l.Accept()
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake-modem: accept:", err)
		os.Exit(1)
	}
	defer conn.Close()

	// Send ModemReady so the Go session advances past its first read.
	if err := writeFrame(conn, &pb.IpcMessage{Payload: &pb.IpcMessage_ModemReady{
		ModemReady: &pb.ModemReady{Version: "fake", Pid: uint64(os.Getpid())},
	}}); err != nil {
		return
	}

	// Read frames until Shutdown or EOF. Closing the conn on Shutdown
	// lets the Go side's session loop return without waiting for the
	// full ShutdownTimeout.
	for {
		m, err := readFrame(conn)
		if err != nil {
			return
		}
		if m.GetShutdown() != nil {
			return
		}
	}
}
`
