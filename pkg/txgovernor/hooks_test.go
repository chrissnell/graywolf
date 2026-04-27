package txgovernor

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
)

func TestSetChannelTimingUnderConcurrentSubmits(t *testing.T) {
	cap := &captureSender{}
	g := New(Config{
		Sender: cap.Send,
		Logger: silentLogger(),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.Run(ctx)

	var wg sync.WaitGroup
	// Writers updating timing.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			g.SetChannelTiming(uint32(i%4), ChannelTiming{Persist: 63, SlotTime: 10 * time.Millisecond})
		}
	}()
	// Submitters.
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				f := makeFrame(t, "race")
				_ = g.Submit(ctx, uint32(i%4), f, SubmitSource{Kind: "kiss", Priority: ax25.PriorityClient})
			}
		}()
	}
	wg.Wait()
}

func TestTxHookInvoked(t *testing.T) {
	cap := &captureSender{}
	// Use a full-duplex channel so the CSMA p-persistence roll is
	// skipped entirely — without this the default Persist=63 defers
	// ~75% of attempts by one 100ms slot and the 1s deadline can
	// expire after a run of unlucky rolls.
	g := New(Config{
		Sender: cap.Send,
		Logger: silentLogger(),
		Channels: map[uint32]ChannelTiming{
			1: {FullDup: true},
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.Run(ctx)

	var hits int32
	_, unregister := g.AddTxHook(func(channel uint32, frame *ax25.Frame, src SubmitSource) {
		atomic.AddInt32(&hits, 1)
	})
	defer unregister()
	f := makeFrame(t, "hook-test")
	if err := g.Submit(ctx, 1, f, SubmitSource{Kind: "digipeater", Priority: PriorityDigipeated}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&hits) > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("hook never fired")
}

// newHookGov returns a governor configured with full-duplex timing on
// channel 1 so the CSMA p-persistence roll cannot defer sends in tests.
func newHookGov() *Governor {
	return New(Config{
		Sender: (&captureSender{}).Send,
		Logger: silentLogger(),
		Channels: map[uint32]ChannelTiming{
			1: {FullDup: true},
		},
	})
}

// TestAddTxHookMultipleInvokedInOrder verifies that two hooks both fire
// on every send, preserving registration order.
func TestAddTxHookMultipleInvokedInOrder(t *testing.T) {
	g := newHookGov()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.Run(ctx)

	var mu sync.Mutex
	var order []int
	_, unA := g.AddTxHook(func(uint32, *ax25.Frame, SubmitSource) {
		mu.Lock()
		order = append(order, 1)
		mu.Unlock()
	})
	defer unA()
	_, unB := g.AddTxHook(func(uint32, *ax25.Frame, SubmitSource) {
		mu.Lock()
		order = append(order, 2)
		mu.Unlock()
	})
	defer unB()

	if err := g.Submit(ctx, 1, makeFrame(t, "multi-hook"), SubmitSource{Priority: PriorityDigipeated}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(order) >= 2
	}, "both hooks fire")
	mu.Lock()
	got := append([]int{}, order...)
	mu.Unlock()
	if got[0] != 1 || got[1] != 2 {
		t.Fatalf("hooks fired out of registration order: %v", got)
	}
}

// TestAddTxHookUnregister verifies: register → fire → unregister →
// fire → confirm the second call is a no-op for the unregistered hook.
// Also confirms unregister is idempotent (calling twice is a no-op).
func TestAddTxHookUnregister(t *testing.T) {
	g := newHookGov()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.Run(ctx)

	var hits int32
	id, unregister := g.AddTxHook(func(uint32, *ax25.Frame, SubmitSource) {
		atomic.AddInt32(&hits, 1)
	})
	if id == 0 {
		t.Fatalf("AddTxHook returned id 0 for non-nil hook")
	}

	// First submit: hook should fire.
	if err := g.Submit(ctx, 1, makeFrame(t, "first"), SubmitSource{Priority: PriorityDigipeated}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return atomic.LoadInt32(&hits) == 1 }, "first submit fires hook")

	// Unregister and submit again; the hook must not fire.
	unregister()
	// Idempotency: unregister again is a no-op.
	unregister()

	if err := g.Submit(ctx, 1, makeFrame(t, "second"), SubmitSource{Priority: PriorityDigipeated}); err != nil {
		t.Fatal(err)
	}
	// Wait long enough for the worker to have sent the second frame.
	// The governor's 50 ms ticker plus a small margin is sufficient.
	time.Sleep(150 * time.Millisecond)
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("after unregister hits=%d want 1 (hook still firing)", got)
	}
}

// TestAddTxHookNilNoPanic verifies that registering a nil hook is a
// no-op (returns id 0 and a no-op unregister).
func TestAddTxHookNilNoPanic(t *testing.T) {
	g := newHookGov()
	id, unregister := g.AddTxHook(nil)
	if id != 0 {
		t.Fatalf("nil hook got id %d want 0", id)
	}
	// Must not panic.
	unregister()
}

// TestAddTxHookConcurrentRegistrationDuringFire stresses the registry:
// one goroutine submits frames continuously while others register and
// unregister hooks. The test passes if it completes without a data race
// under `go test -race` and without a panic.
func TestAddTxHookConcurrentRegistrationDuringFire(t *testing.T) {
	g := newHookGov()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.Run(ctx)

	var fired int64
	// A long-lived hook that simply records invocations. Registered
	// once up front so there is always at least one hook to fire.
	_, baseUnreg := g.AddTxHook(func(uint32, *ax25.Frame, SubmitSource) {
		atomic.AddInt64(&fired, 1)
	})
	defer baseUnreg()

	// Submitter goroutine: feed frames at a steady rate.
	submitDone := make(chan struct{})
	go func() {
		defer close(submitDone)
		for i := 0; i < 200; i++ {
			f := makeFrame(t, "concurrent")
			// Frames with identical content would dedup; skip dedup to
			// actually exercise the hook fan-out on every send.
			_ = g.Submit(ctx, 1, f, SubmitSource{Priority: PriorityDigipeated, SkipDedup: true})
			time.Sleep(500 * time.Microsecond)
		}
	}()

	// Registration/unregistration churn goroutines.
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_, un := g.AddTxHook(func(uint32, *ax25.Frame, SubmitSource) {
					atomic.AddInt64(&fired, 1)
				})
				// Sometimes hold briefly, sometimes unregister
				// immediately — both orderings should be safe.
				if i%2 == 0 {
					time.Sleep(100 * time.Microsecond)
				}
				un()
			}
		}()
	}

	<-submitDone
	wg.Wait()

	// Sanity: at least the base hook should have fired. A precise
	// count isn't meaningful because concurrent churn means some
	// sends may observe different hook-set snapshots — the invariant
	// is "no race, no panic, base hook sees sends".
	if atomic.LoadInt64(&fired) == 0 {
		t.Fatalf("no hook invocations observed — submitter never made progress")
	}
}
