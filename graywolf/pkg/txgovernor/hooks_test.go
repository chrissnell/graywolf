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
			g.SetChannelTiming(uint32(i%4), ChannelTiming{TxDelayMs: uint32(i), Persist: 63, SlotTime: 10 * time.Millisecond})
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
	g.SetTxHook(func(channel uint32, frame *ax25.Frame, src SubmitSource) {
		atomic.AddInt32(&hits, 1)
	})
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
