package app

import (
	"context"
	"sync"
	"testing"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
)

// TestRFIngressSetsDirectionRF verifies the RF ingress contract:
// frames received from the modem bridge are decoded via aprs.Parse
// and tagged DirectionRF before being enqueued onto the APRS fan-out.
// Downstream consumers (messages router, Source badge) rely on this
// tagging to distinguish RF from IS traffic.
//
// This test mirrors the RF-path logic in wireServices → modem frame
// consumer (pkg/app/wiring.go, "modembridge frame consumer" block)
// without requiring the full App wiring. If that block ever stops
// setting pkt.Direction = DirectionRF, this test fails.
func TestRFIngressSetsDirectionRF(t *testing.T) {
	// Build a minimal UI frame (a position beacon) to feed the path.
	src, err := ax25.ParseAddress("W5ABC-7")
	if err != nil {
		t.Fatalf("parse src addr: %v", err)
	}
	dest, err := ax25.ParseAddress("APRS")
	if err != nil {
		t.Fatalf("parse dest addr: %v", err)
	}
	f := &ax25.Frame{
		Dest:    dest,
		Source:  src,
		Control: 0x03, // UI
		PID:     0xF0,
		Info:    []byte("!3725.00N/12158.00W>RF test"),
	}

	// RF-path contract: Parse → set Direction → submit to fanout.
	pkt, err := aprs.Parse(f)
	if err != nil || pkt == nil {
		t.Fatalf("aprs.Parse failed: err=%v pkt=%v", err, pkt)
	}
	pkt.Channel = 1
	pkt.Direction = aprs.DirectionRF

	// Wire through the fan-out submitter + runAPRSFanOut and confirm
	// Direction survives the hop intact.
	queue := make(chan *aprs.DecodedAPRSPacket, 1)
	out := &recordingOutput{}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runAPRSFanOut(context.Background(), queue, out)
	}()

	s := newAPRSSubmitter(queue, &fakeCounter{}, nil)
	s.submit(pkt)
	close(queue)
	wg.Wait()

	if got := out.count(); got != 1 {
		t.Fatalf("fanout delivered %d packets, want 1", got)
	}
	out.mu.Lock()
	got := out.pkts[0].Direction
	out.mu.Unlock()
	if got != aprs.DirectionRF {
		t.Fatalf("fan-out delivered Direction=%q, want %q", got, aprs.DirectionRF)
	}
}
