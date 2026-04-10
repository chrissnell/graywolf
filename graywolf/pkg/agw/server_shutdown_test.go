package agw

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestAGWServerPortFreeAfterCancel verifies that once ListenAndServe
// returns, the bound port is immediately available for a new listener —
// i.e. the cancel-watcher goroutine that closes the listener is waited on
// before return, not left racing against the next bind.
func TestAGWServerPortFreeAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	srv := NewServer(ServerConfig{ListenAddr: "127.0.0.1:0"})

	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.ListenAndServe(ctx) }()

	var addr net.Addr
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		if addr = srv.LocalAddr(); addr != nil {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if addr == nil {
		t.Fatal("server never bound its listener")
	}

	cancel()

	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("ListenAndServe returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ListenAndServe did not return within 1s of context cancel")
	}

	// The port must be immediately bindable — no retry loop.
	ln, err := net.Listen("tcp", addr.String())
	if err != nil {
		t.Fatalf("rebind on %s failed: %v", addr.String(), err)
	}
	_ = ln.Close()
}
