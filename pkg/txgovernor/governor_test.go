package txgovernor

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

type captureSender struct {
	mu    sync.Mutex
	sent  []*pb.TransmitFrame
	count int32
}

func (c *captureSender) Send(tf *pb.TransmitFrame) error {
	c.mu.Lock()
	c.sent = append(c.sent, tf)
	c.mu.Unlock()
	atomic.AddInt32(&c.count, 1)
	return nil
}

func (c *captureSender) Count() int { return int(atomic.LoadInt32(&c.count)) }

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func makeFrame(t *testing.T, info string) *ax25.Frame {
	t.Helper()
	src, _ := ax25.ParseAddress("N0CALL-1")
	dst, _ := ax25.ParseAddress("APRS")
	f, err := ax25.NewUIFrame(src, dst, nil, []byte(info))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func newDeterministic(sender *captureSender) *Governor {
	return New(Config{
		Sender:     sender.Send,
		Logger:     silentLogger(),
		RandSource: rand.New(rand.NewSource(1)),
		Channels: map[uint32]ChannelTiming{
			1: {Persist: 255, SlotTime: 10 * time.Millisecond, FullDup: true},
		},
	})
}

func TestSubmitAndSend(t *testing.T) {
	sender := &captureSender{}
	g := newDeterministic(sender)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.Run(ctx)

	if err := g.Submit(ctx, 1, makeFrame(t, "one"), SubmitSource{Kind: "kiss", Priority: PriorityClient}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return sender.Count() == 1 }, "first send")

	st := g.Stats()
	if st.Sent != 1 || st.Enqueued != 1 {
		t.Errorf("stats: %+v", st)
	}
}

func TestDeduplication(t *testing.T) {
	sender := &captureSender{}
	g := newDeterministic(sender)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.Run(ctx)

	// Submit the same frame three times quickly.
	for i := 0; i < 3; i++ {
		_ = g.Submit(ctx, 1, makeFrame(t, "dupe"), SubmitSource{Kind: "kiss", Priority: PriorityClient})
	}
	waitFor(t, func() bool { return sender.Count() == 1 }, "one send only")
	time.Sleep(50 * time.Millisecond)
	if sender.Count() != 1 {
		t.Errorf("expected 1 send, got %d", sender.Count())
	}
	st := g.Stats()
	if st.Deduped != 2 {
		t.Errorf("deduped=%d, want 2", st.Deduped)
	}
}

func TestPriorityOrdering(t *testing.T) {
	// Gated sender lets us pre-load the queue before any send runs.
	gate := make(chan struct{})
	slow := &captureSender{}
	g := New(Config{
		Sender: func(tf *pb.TransmitFrame) error {
			<-gate
			return slow.Send(tf)
		},
		Logger:     silentLogger(),
		RandSource: rand.New(rand.NewSource(1)),
		Channels: map[uint32]ChannelTiming{
			1: {Persist: 255, FullDup: true},
		},
	})

	// Pre-load queue directly in order: low, high, mid.
	_ = g.Submit(context.Background(), 1, makeFrame(t, "low"), SubmitSource{Priority: PriorityBeacon})
	_ = g.Submit(context.Background(), 1, makeFrame(t, "high"), SubmitSource{Priority: PriorityIGateMsg})
	_ = g.Submit(context.Background(), 1, makeFrame(t, "mid"), SubmitSource{Priority: PriorityClient})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.Run(ctx)

	// Release one at a time, verifying order.
	close(gate)
	waitFor(t, func() bool { return slow.Count() == 3 }, "all three sent")

	slow.mu.Lock()
	defer slow.mu.Unlock()
	got := []string{string(decodeInfo(t, slow.sent[0])), string(decodeInfo(t, slow.sent[1])), string(decodeInfo(t, slow.sent[2]))}
	want := []string{"high", "mid", "low"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("order[%d]: got %q want %q", i, got[i], want[i])
		}
	}
}

func decodeInfo(t *testing.T, tf *pb.TransmitFrame) []byte {
	t.Helper()
	f, err := ax25.Decode(tf.Data)
	if err != nil {
		t.Fatal(err)
	}
	return f.Info
}

func TestRateLimit1Min(t *testing.T) {
	sender := &captureSender{}
	g := New(Config{
		Sender:        sender.Send,
		Logger:        silentLogger(),
		RandSource:    rand.New(rand.NewSource(1)),
		Rate1MinLimit: 2,
		Channels: map[uint32]ChannelTiming{
			1: {Persist: 255, FullDup: true},
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.Run(ctx)

	for i := 0; i < 5; i++ {
		_ = g.Submit(ctx, 1, makeFrame(t, string(rune('a'+i))), SubmitSource{Priority: PriorityClient})
	}
	// Give the worker plenty of time.
	time.Sleep(300 * time.Millisecond)
	if sender.Count() != 2 {
		t.Errorf("sent=%d, want 2", sender.Count())
	}
	st := g.Stats()
	if st.RateLimited == 0 {
		t.Errorf("expected RateLimited>0, got %d", st.RateLimited)
	}
}

func TestDcdBlocksTransmit(t *testing.T) {
	sender := &captureSender{}
	dcdCh := make(chan *pb.DcdChange, 4)
	g := New(Config{
		Sender:     sender.Send,
		Logger:     silentLogger(),
		RandSource: rand.New(rand.NewSource(1)),
		DcdEvents:  dcdCh,
		Channels: map[uint32]ChannelTiming{
			1: {Persist: 255, SlotTime: 10 * time.Millisecond},
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.Run(ctx)

	// Raise DCD before enqueuing.
	dcdCh <- &pb.DcdChange{Channel: 1, Detected: true}
	time.Sleep(50 * time.Millisecond)

	_ = g.Submit(ctx, 1, makeFrame(t, "blocked"), SubmitSource{Priority: PriorityClient})
	time.Sleep(200 * time.Millisecond)
	if sender.Count() != 0 {
		t.Errorf("expected 0 sends while DCD active, got %d", sender.Count())
	}

	// Drop DCD.
	dcdCh <- &pb.DcdChange{Channel: 1, Detected: false}
	waitFor(t, func() bool { return sender.Count() == 1 }, "send after dcd clear")
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for: %s", what)
}

func TestQueueCapacity(t *testing.T) {
	sender := &captureSender{}
	g := New(Config{
		Sender:        sender.Send,
		Logger:        silentLogger(),
		RandSource:    rand.New(rand.NewSource(1)),
		QueueCapacity: 2,
		Channels: map[uint32]ChannelTiming{
			1: {Persist: 255, FullDup: true},
		},
	})

	// Do NOT run the governor — we want the queue to stay full.
	_ = g.Submit(context.Background(), 1, makeFrame(t, "a"), SubmitSource{Priority: PriorityClient})
	_ = g.Submit(context.Background(), 1, makeFrame(t, "b"), SubmitSource{Priority: PriorityClient})
	err := g.Submit(context.Background(), 1, makeFrame(t, "c"), SubmitSource{Priority: PriorityClient})
	if !errors.Is(err, ErrQueueFull) {
		t.Errorf("expected ErrQueueFull, got %v", err)
	}
	if g.Stats().QueueDropped != 1 {
		t.Errorf("queue_dropped=%d", g.Stats().QueueDropped)
	}
}
