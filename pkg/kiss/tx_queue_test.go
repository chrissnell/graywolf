package kiss

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestInstanceTxQueue_EnqueueAndDrain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var count atomic.Int32
	broadcast := func(_ []byte) {
		count.Add(1)
	}
	q := newInstanceTxQueue(ctx, broadcast)
	defer q.Close()

	for i := 0; i < 5; i++ {
		if err := q.Enqueue([]byte{byte(i)}, uint64(i+1)); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	deadline := time.After(time.Second)
	for count.Load() < 5 {
		select {
		case <-deadline:
			t.Fatalf("drain timed out; got %d", count.Load())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestInstanceTxQueue_FullReturnsBusy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Broadcast blocks until release is closed so the drainer pins on
	// the in-flight frame. Release is closed BEFORE q.Close so the
	// drainer goroutine can exit — otherwise q.Close's WaitGroup would
	// deadlock waiting on a broadcast that never returns.
	release := make(chan struct{})
	broadcast := func(_ []byte) {
		<-release
	}
	q := newInstanceTxQueue(ctx, broadcast)
	defer func() {
		close(release)
		q.Close()
	}()

	// First enqueue + small yield so the drainer pulls it off q.ch and
	// blocks inside broadcast(<-release). After that, q.ch has capacity
	// instanceTxQueueDepth and the drainer will stay pinned.
	if err := q.Enqueue([]byte{0xAA}, 1); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	// Now fill the buffer — exactly instanceTxQueueDepth more frames fit.
	for i := 0; i < instanceTxQueueDepth; i++ {
		if err := q.Enqueue([]byte{0xAA}, uint64(i+2)); err != nil {
			t.Fatalf("fill enqueue %d: %v", i, err)
		}
	}

	// The next enqueue must return ErrBackendBusy — buffer full, drainer pinned.
	err := q.Enqueue([]byte{0xFF}, 99)
	if !errors.Is(err, ErrBackendBusy) {
		t.Fatalf("enqueue after full: got %v, want ErrBackendBusy", err)
	}
}

func TestInstanceTxQueue_ClosedReturnsDown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := newInstanceTxQueue(ctx, func([]byte) {})
	q.Close()

	err := q.Enqueue([]byte{0x01}, 1)
	if !errors.Is(err, ErrBackendDown) {
		t.Fatalf("err=%v, want ErrBackendDown", err)
	}
}

func TestInstanceTxQueue_ObserversFire(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var enqueueHits, depthHits atomic.Int32
	q := newInstanceTxQueue(ctx, func([]byte) {})
	defer q.Close()
	q.SetObservers(
		func() { enqueueHits.Add(1) },
		func(string) {},
		func(int32) { depthHits.Add(1) },
	)

	if err := q.Enqueue([]byte{0x01}, 1); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	// Give the drainer a moment to call onDepth on drain too.
	time.Sleep(50 * time.Millisecond)
	if enqueueHits.Load() == 0 {
		t.Error("onEnqueue never fired")
	}
	if depthHits.Load() < 2 {
		// One for enqueue (depth=1), one for drain (depth=0).
		t.Errorf("onDepth fired %d times, want >=2", depthHits.Load())
	}
}
