package modembridge

import (
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
	"github.com/chrissnell/graywolf/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// counterValue reads a Prometheus counter's current value without
// pulling in the testutil subpackage (which would promote several
// indirect deps to direct).
func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	m := &dto.Metric{}
	if err := c.Write(m); err != nil {
		t.Fatalf("counter write: %v", err)
	}
	return m.GetCounter().GetValue()
}

// TestDcdPublisherPublishesToAllSubscribers verifies that every Publish
// reaches every Subscribe'd channel.
func TestDcdPublisherPublishesToAllSubscribers(t *testing.T) {
	p := newDcdPublisher(testLogger(), nil)
	defer p.Close()

	a := p.Subscribe()
	b := p.Subscribe()

	for i := 0; i < 3; i++ {
		p.Publish(&pb.DcdChange{Channel: uint32(i)})
	}

	drain := func(ch <-chan *pb.DcdChange) int {
		got := 0
		deadline := time.After(200 * time.Millisecond)
		for got < 3 {
			select {
			case <-ch:
				got++
			case <-deadline:
				return got
			}
		}
		return got
	}
	if n := drain(a); n != 3 {
		t.Errorf("subscriber a got %d, want 3", n)
	}
	if n := drain(b); n != 3 {
		t.Errorf("subscriber b got %d, want 3", n)
	}
}

// TestDcdPublisherSlowSubscriberDrops verifies that a full subscriber
// channel drops events rather than stalling other subscribers, and that
// the drop is counted via incDropped. The test is deterministic: it
// fills both subscribers' buffers in phase 1 (no drops expected), then
// synchronously drains fast only in phase 2 and publishes 5 more events
// — slow is still full so it must drop all 5 while fast receives all 5.
func TestDcdPublisherSlowSubscriberDrops(t *testing.T) {
	var dropCount atomic.Int64
	p := newDcdPublisher(testLogger(), func() { dropCount.Add(1) })
	defer p.Close()

	slow := p.Subscribe()
	fast := p.Subscribe()

	// Phase 1: fill both subscribers' buffers exactly to capacity.
	for i := 0; i < dcdPublisherBufferSize; i++ {
		p.Publish(&pb.DcdChange{Channel: uint32(i)})
	}
	if got := dropCount.Load(); got != 0 {
		t.Fatalf("phase 1 drop count = %d, want 0", got)
	}

	// Drain fast completely; slow remains full.
	for i := 0; i < dcdPublisherBufferSize; i++ {
		select {
		case <-fast:
		case <-time.After(time.Second):
			t.Fatalf("phase 1 drain stalled at %d", i)
		}
	}

	// Phase 2: publish 5 more. slow drops all 5; fast queues all 5.
	const extra = 5
	for i := 0; i < extra; i++ {
		p.Publish(&pb.DcdChange{Channel: uint32(dcdPublisherBufferSize + i)})
	}
	if got := dropCount.Load(); got != extra {
		t.Errorf("phase 2 drop count = %d, want %d", got, extra)
	}

	// fast should have exactly extra new events queued.
	queued := 0
	for i := 0; i < extra; i++ {
		select {
		case <-fast:
			queued++
		case <-time.After(time.Second):
			t.Fatalf("fast did not receive phase-2 event %d", i)
		}
	}
	if queued != extra {
		t.Errorf("fast received %d, want %d", queued, extra)
	}

	// slow should still have exactly dcdPublisherBufferSize events queued
	// (its phase-1 fill was never drained and every phase-2 publish
	// dropped on it).
	slowQueued := 0
DRAIN:
	for {
		select {
		case <-slow:
			slowQueued++
		default:
			break DRAIN
		}
	}
	if slowQueued != dcdPublisherBufferSize {
		t.Errorf("slow subscriber queued = %d, want %d", slowQueued, dcdPublisherBufferSize)
	}
}

// TestDcdPublisherUnsubscribeStopsDelivery verifies Unsubscribe removes
// the channel from future Publish fan-outs and closes the channel so a
// range consumer exits.
func TestDcdPublisherUnsubscribeStopsDelivery(t *testing.T) {
	p := newDcdPublisher(testLogger(), nil)
	defer p.Close()

	a := p.Subscribe()
	b := p.Subscribe()

	p.Publish(&pb.DcdChange{Channel: 1})

	// Both receive the first event.
	if _, ok := <-a; !ok {
		t.Fatal("a did not receive first event")
	}
	if _, ok := <-b; !ok {
		t.Fatal("b did not receive first event")
	}

	// Unsubscribe a.
	p.Unsubscribe(a)

	// a's channel should be closed.
	select {
	case _, ok := <-a:
		if ok {
			t.Error("a should be closed after Unsubscribe")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("a did not close after Unsubscribe")
	}

	// b still receives the next event.
	p.Publish(&pb.DcdChange{Channel: 2})
	select {
	case ev := <-b:
		if ev.Channel != 2 {
			t.Errorf("b got channel %d, want 2", ev.Channel)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("b did not receive second event after Unsubscribe(a)")
	}
}

// TestDcdPublisherCloseUnblocksRangeConsumers verifies Close closes every
// subscriber channel so a `for range` consumer exits cleanly.
func TestDcdPublisherCloseUnblocksRangeConsumers(t *testing.T) {
	p := newDcdPublisher(testLogger(), nil)
	ch := p.Subscribe()

	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()

	p.Close()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("range consumer did not exit after Close")
	}
}

// TestDcdPublisherWiresMetricsCounter verifies that a drop hook sourced
// from *metrics.Metrics.DcdDropped increments the shared
// graywolf_modembridge_dcd_dropped_total counter on slow-subscriber
// drops. This is the wiring contract Bridge.New relies on: if the
// counter type changes or the hook installation drifts, this test
// catches it without needing to spin up the full bridge supervisor.
func TestDcdPublisherWiresMetricsCounter(t *testing.T) {
	m := metrics.New()
	p := newDcdPublisher(testLogger(), m.DcdDropped.Inc)
	defer p.Close()

	// Fill the buffer of the only subscriber so every further publish drops.
	slow := p.Subscribe()
	_ = slow
	for i := 0; i < dcdPublisherBufferSize; i++ {
		p.Publish(&pb.DcdChange{Channel: uint32(i)})
	}
	if got := counterValue(t, m.DcdDropped); got != 0 {
		t.Fatalf("pre-drop counter = %v, want 0 (buffer should have absorbed)", got)
	}
	const extra = 5
	for i := 0; i < extra; i++ {
		p.Publish(&pb.DcdChange{Channel: uint32(dcdPublisherBufferSize + i)})
	}
	if got := counterValue(t, m.DcdDropped); got != float64(extra) {
		t.Errorf("DcdDropped counter = %v, want %d", got, extra)
	}
}

// TestDcdPublisherSubscribeAfterCloseReturnsClosedChannel verifies that a
// Subscribe racing past a concurrent Close gets a closed channel instead
// of leaking.
func TestDcdPublisherSubscribeAfterCloseReturnsClosedChannel(t *testing.T) {
	p := newDcdPublisher(testLogger(), nil)
	p.Close()

	ch := p.Subscribe()
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected closed channel, got an event")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Subscribe did not return a closed channel after Close")
	}
}
