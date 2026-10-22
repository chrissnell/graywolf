//go:build android

package platformsvc

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/platformproto"
)

// TestRoundTripQueuedBehindWedgedRequestTimesOut: a round-trip whose reply
// never arrives holds the request line. A second caller queued behind it
// must give up when its own ctx expires (and name the holder) rather than
// block forever -- the KISS bonded-device picker's "Loading" hang (GH #573).
func TestRoundTripQueuedBehindWedgedRequestTimesOut(t *testing.T) {
	c := newClient("unused").(*clientImpl)
	client, server := net.Pipe()
	defer server.Close()
	c.injectConn(client)
	// Swallow every request and never reply, so the first one wedges.
	go func() { _, _ = io.Copy(io.Discard, server) }()

	wedged := make(chan error, 1)
	go func() {
		_, err := c.ListUsbDevices(context.Background(), pb.UsbClass_USB_CLASS_UNKNOWN)
		wedged <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for c.inflight.Load() == nil {
		if time.Now().After(deadline) {
			t.Fatal("first request never took the request line")
		}
		time.Sleep(5 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.BondedBtDevices(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued request: got %v, want context.DeadlineExceeded", err)
	}
	if !strings.Contains(err.Error(), "UsbListReq") {
		t.Fatalf("error should name the wedged holder, got %q", err)
	}
	if waited := time.Since(start); waited > 2*time.Second {
		t.Fatalf("queued request waited %s; ctx should have bounded it", waited)
	}

	_ = c.Close()
	select {
	case <-wedged:
	case <-time.After(2 * time.Second):
		t.Fatal("wedged request did not unblock on Close")
	}
}
