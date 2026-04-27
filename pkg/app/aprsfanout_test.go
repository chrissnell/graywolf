package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

// fakeCounter is a minimal aprsDropCounter used to assert the fan-out
// submitter increments exactly once per dropped packet.
type fakeCounter struct {
	mu sync.Mutex
	n  int
}

func (c *fakeCounter) Inc() {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

func (c *fakeCounter) get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// recordingOutput captures packets received via SendPacket.
type recordingOutput struct {
	mu   sync.Mutex
	pkts []*aprs.DecodedAPRSPacket
}

func (r *recordingOutput) SendPacket(_ context.Context, pkt *aprs.DecodedAPRSPacket) error {
	r.mu.Lock()
	r.pkts = append(r.pkts, pkt)
	r.mu.Unlock()
	return nil
}

func (r *recordingOutput) Close() error { return nil }

func (r *recordingOutput) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pkts)
}

func TestAPRSFanOut_NormalFlow(t *testing.T) {
	queue := make(chan *aprs.DecodedAPRSPacket, 4)
	counter := &fakeCounter{}
	out := &recordingOutput{}

	submit := newAPRSSubmitter(queue, counter, quietLogger())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runAPRSFanOut(context.Background(), queue, out)
	}()

	for i := 0; i < 3; i++ {
		submit.submit(&aprs.DecodedAPRSPacket{Source: "TEST"})
	}
	close(queue)
	wg.Wait()

	if got := out.count(); got != 3 {
		t.Fatalf("output got %d packets, want 3", got)
	}
	if got := counter.get(); got != 0 {
		t.Fatalf("drop counter = %d, want 0", got)
	}
}

func TestAPRSFanOut_OverflowCountedExactlyOnce(t *testing.T) {
	// Tiny queue, no consumer running, so after the capacity is filled
	// every subsequent submit must drop and increment the counter exactly
	// once per call (not twice, which the old double-drain drop did).
	const cap = 2
	queue := make(chan *aprs.DecodedAPRSPacket, cap)
	counter := &fakeCounter{}
	submit := newAPRSSubmitter(queue, counter, quietLogger())

	for i := 0; i < cap; i++ {
		submit.submit(&aprs.DecodedAPRSPacket{})
	}
	if got := counter.get(); got != 0 {
		t.Fatalf("pre-overflow drops = %d, want 0", got)
	}

	const overflow = 5
	for i := 0; i < overflow; i++ {
		submit.submit(&aprs.DecodedAPRSPacket{})
	}
	if got := counter.get(); got != overflow {
		t.Fatalf("drops = %d, want %d (one per overflow submit)", got, overflow)
	}
}

func TestAPRSFanOut_ShutdownDrains(t *testing.T) {
	queue := make(chan *aprs.DecodedAPRSPacket, 8)
	counter := &fakeCounter{}
	out := &recordingOutput{}
	submit := newAPRSSubmitter(queue, counter, quietLogger())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runAPRSFanOut(context.Background(), queue, out)
	}()

	// Enqueue some work and close, simulating shutdown.
	for i := 0; i < 4; i++ {
		submit.submit(&aprs.DecodedAPRSPacket{})
	}
	close(queue)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("fan-out goroutine did not exit within 100ms after queue close")
	}

	if got := out.count(); got != 4 {
		t.Fatalf("output drained %d packets, want 4", got)
	}
}

// TestAPRSFanOut_NilOutputSkipped confirms the variadic nil-guard so
// callers can pass a typed-nil-free interface without a special case.
func TestAPRSFanOut_NilOutputSkipped(t *testing.T) {
	queue := make(chan *aprs.DecodedAPRSPacket, 1)
	out := &recordingOutput{}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runAPRSFanOut(context.Background(), queue, out, nil)
	}()

	queue <- &aprs.DecodedAPRSPacket{}
	close(queue)
	wg.Wait()

	if got := out.count(); got != 1 {
		t.Fatalf("output got %d, want 1", got)
	}
}
