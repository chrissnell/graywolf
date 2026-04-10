package modembridge

import (
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
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
// the drop is counted via incDropped. The fast subscriber drains in a
// helper goroutine so its buffer never fills; the slow one never drains
// until after all Publish calls, so exactly (total - buffer) events
// drop for it.
func TestDcdPublisherSlowSubscriberDrops(t *testing.T) {
	var dropCount atomic.Int64
	p := newDcdPublisher(testLogger(), func() { dropCount.Add(1) })
	defer p.Close()

	slow := p.Subscribe()
	fast := p.Subscribe()

	// Drain fast concurrently so its buffer never fills.
	fastDone := make(chan int, 1)
	go func() {
		received := 0
		for range fast {
			received++
		}
		fastDone <- received
	}()

	const total = dcdPublisherBufferSize + 5
	for i := 0; i < total; i++ {
		p.Publish(&pb.DcdChange{Channel: uint32(i)})
	}

	if got := dropCount.Load(); got != 5 {
		t.Errorf("drop count = %d, want 5", got)
	}

	// slow should have exactly dcdPublisherBufferSize events queued.
	queued := 0
DRAIN:
	for {
		select {
		case <-slow:
			queued++
		default:
			break DRAIN
		}
	}
	if queued != dcdPublisherBufferSize {
		t.Errorf("slow subscriber queued = %d, want %d", queued, dcdPublisherBufferSize)
	}

	// Closing the publisher unblocks the fast-drain goroutine so the
	// test doesn't leak it.
	p.Close()
	select {
	case n := <-fastDone:
		if n != total {
			t.Errorf("fast subscriber received %d, want %d", n, total)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("fast drainer did not exit after Close")
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
