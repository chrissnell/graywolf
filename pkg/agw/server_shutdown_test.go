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

// TestAGWServerShutdownWithActiveClient verifies Shutdown returns within
// 200ms with a live client connected, closes the client so handleClient
// unblocks, and lets the port be rebound immediately.
func TestAGWServerShutdownWithActiveClient(t *testing.T) {
	srv := NewServer(ServerConfig{ListenAddr: "127.0.0.1:0"})

	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.ListenAndServe(context.Background()) }()

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

	client, err := net.Dial("tcp", addr.String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	// Wait for the handler to register the connection.
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		if srv.ActiveClients() >= 1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if srv.ActiveClients() != 1 {
		t.Fatalf("ActiveClients = %d, want 1", srv.ActiveClients())
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if elapsed := time.Since(start); elapsed >= 200*time.Millisecond {
		t.Fatalf("Shutdown took %s, want <200ms", elapsed)
	}

	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("ListenAndServe returned %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("ListenAndServe did not return after Shutdown")
	}

	ln, err := net.Listen("tcp", addr.String())
	if err != nil {
		t.Fatalf("rebind on %s failed: %v", addr.String(), err)
	}
	_ = ln.Close()
}
