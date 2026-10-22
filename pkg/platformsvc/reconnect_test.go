//go:build android

package platformsvc

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/platformproto"
)

// TestReconnectAfterEOF: client connects to a UDS, server closes the
// connection mid-life, client transparently reconnects. We assert that
// after the server re-accepts, a fresh Hello round-trip succeeds.
func TestReconnectAfterEOF(t *testing.T) {
	// Linux abstract-namespace UDS path (leading "@") — Go translates the
	// "@" to a NUL byte for sun_path. Avoids filesystem-level SELinux
	// denials on Android's /data/local/tmp where on-disk UDS bind is
	// blocked under shell_data_file. The address is unique-per-test via a
	// nanosecond suffix so concurrent runs don't collide.
	sockPath := fmt.Sprintf("@platformsvc-test-%d.sock", time.Now().UnixNano())

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	var (
		acceptCount int
		mu          sync.Mutex
	)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			acceptCount++
			isFirst := acceptCount == 1
			mu.Unlock()
			go func(c net.Conn, first bool) {
				defer c.Close()
				if first {
					// Drop after a brief moment to force reconnect.
					time.Sleep(50 * time.Millisecond)
					return
				}
				// Permanent server: reply to one Hello.
				msg, err := readFrame(c)
				if err != nil {
					return
				}
				if msg.GetHello() == nil {
					return
				}
				_ = writeFrame(c, helloOK(msg.GetHello()))
			}(conn, isFirst)
		}
	}()

	c := newClient(sockPath).(*clientImpl)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.ConnectWithReconnect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	// The first connection drops; ConnectWithReconnect should re-dial.
	// Wait for accept #2 to land.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		ok := acceptCount >= 2
		mu.Unlock()
		if ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	got := acceptCount
	mu.Unlock()
	if got < 2 {
		t.Fatalf("expected >=2 accepts (initial + reconnect), got %d", got)
	}

	// Now verify Hello round-trips on the reconnected session.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	if _, err := c.Hello(ctx2, SchemaVersion); err != nil {
		t.Fatalf("Hello after reconnect: %v", err)
	}
}

// TestReconnectReHandshakes verifies the client re-sends Hello on its own
// after a reconnect (no caller intervention). The server registers a
// connection for server→client broadcasts only after a Hello, so a
// reconnect that skipped the handshake would silently drop every broadcast
// (GPS/GNSS/bonded-device/serial) — the root cause of the KISS
// bonded-device picker hanging on "Loading" (GH #573).
func TestReconnectReHandshakes(t *testing.T) {
	sockPath := fmt.Sprintf("@platformsvc-rehello-%d.sock", time.Now().UnixNano())

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	secondHello := make(chan struct{}, 1)
	var (
		acceptCount int
		mu          sync.Mutex
	)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			acceptCount++
			isFirst := acceptCount == 1
			mu.Unlock()
			go func(c net.Conn, first bool) {
				defer c.Close()
				// Reply to the Hello on this connection, then (first only)
				// drop to force a reconnect.
				msg, err := readFrame(c)
				if err != nil || msg.GetHello() == nil {
					return
				}
				_ = writeFrame(c, helloOK(msg.GetHello()))
				if first {
					time.Sleep(50 * time.Millisecond)
					return
				}
				// The reconnected session must have Hello'd on its own.
				select {
				case secondHello <- struct{}{}:
				default:
				}
				// Keep the connection open so the client doesn't loop.
				_, _ = readFrame(c)
			}(conn, isFirst)
		}
	}()

	c := newClient(sockPath).(*clientImpl)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.ConnectWithReconnect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	// Initial Hello (as main_android does) — this is what records the
	// schema version the reconnect loop replays.
	helloCtx, helloCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer helloCancel()
	if _, err := c.Hello(helloCtx, SchemaVersion); err != nil {
		t.Fatalf("initial Hello: %v", err)
	}

	// The first connection drops; the reconnect loop must re-dial AND
	// re-Hello without the caller touching Hello again.
	select {
	case <-secondHello:
	case <-time.After(3 * time.Second):
		t.Fatal("reconnected session never received an auto Hello")
	}
}

// TestBackoffSchedule asserts the backoff sequence used by the reconnect
// loop is the documented one (Decisions table). Test reads the package-
// private backoffSchedule slice.
func TestBackoffSchedule(t *testing.T) {
	want := []time.Duration{
		100 * time.Millisecond, 200 * time.Millisecond,
		400 * time.Millisecond, 800 * time.Millisecond,
		1600 * time.Millisecond, 5 * time.Second,
	}
	if len(backoffSchedule) != len(want) {
		t.Fatalf("len: got %d, want %d", len(backoffSchedule), len(want))
	}
	for i, d := range want {
		if backoffSchedule[i] != d {
			t.Errorf("step %d: got %v, want %v", i, backoffSchedule[i], d)
		}
	}
}

func helloOK(req *pbHello) *pb.PlatformMessage {
	return &pb.PlatformMessage{Body: &pb.PlatformMessage_Hello{
		Hello: &pb.Hello{
			SchemaVersion: req.SchemaVersion,
			ClientVersion: req.ClientVersion,
			ServerVersion: "v0.0.0-test-server",
		},
	}}
}

type pbHello = pb.Hello
