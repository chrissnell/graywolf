package txgovernor

import (
	"context"
	"errors"
	"testing"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

func TestSourceOutcomesSeparateSuppressionAndBackendFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "forwarded", true: "backend_failure"}[fail], func(t *testing.T) {
			counts := map[string]int{}
			sends := 0
			var g *Governor
			g = New(Config{
				Logger: silentLogger(),
				Sender: func(*pb.TransmitFrame) error {
					sends++
					if fail {
						return errors.New("backend unavailable")
					}
					return nil
				},
				Channels: map[uint32]ChannelTiming{1: {FullDup: true}},
				OnOutcome: func(ch uint32, source SubmitSource, outcome Outcome) {
					// Observers may read governor state; this would deadlock
					// if any decision callback ran under g.mu.
					_ = g.QueueLen()
					if ch != 1 {
						t.Errorf("channel = %d", ch)
					}
					counts[source.Kind+"/"+string(outcome)]++
				},
			})
			ctx := context.Background()
			for _, source := range []string{"digipeater", "digipeater", "kiss"} {
				if err := g.Submit(ctx, 1, makeFrame(t, "same payload"), SubmitSource{Kind: source}); err != nil {
					t.Fatal(err)
				}
			}
			g.processOne(ctx)
			if sends != 1 {
				t.Fatalf("backend attempts = %d, want 1", sends)
			}
			expected := map[string]int{"digipeater/enqueued": 1, "digipeater/deduped": 1, "kiss/deduped": 1}
			if fail {
				expected["digipeater/send_error"] = 1
			} else {
				expected["digipeater/forwarded"] = 1
			}
			if len(counts) != len(expected) {
				t.Fatalf("unexpected outcomes: %v", counts)
			}
			for key, want := range expected {
				if counts[key] != want {
					t.Errorf("%s = %d, want %d", key, counts[key], want)
				}
			}
			// Historical aggregate behavior is deliberately unchanged.
			if g.Stats().Deduped != 2 {
				t.Errorf("dedup total = %d", g.Stats().Deduped)
			}
		})
	}
}

func TestQueueFullOutcomeDoesNotPoisonRetry(t *testing.T) {
	counts := map[Outcome]int{}
	g := New(Config{
		Logger: silentLogger(), QueueCapacity: 1,
		Sender:    func(*pb.TransmitFrame) error { return nil },
		Channels:  map[uint32]ChannelTiming{1: {FullDup: true}},
		OnOutcome: func(_ uint32, _ SubmitSource, o Outcome) { counts[o]++ },
	})
	ctx := context.Background()
	source := SubmitSource{Kind: "digipeater"}
	if err := g.Submit(ctx, 1, makeFrame(t, "first"), source); err != nil {
		t.Fatal(err)
	}
	if err := g.Submit(ctx, 1, makeFrame(t, "retry"), source); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("got %v", err)
	}
	g.processOne(ctx)
	if err := g.Submit(ctx, 1, makeFrame(t, "retry"), source); err != nil {
		t.Fatal(err)
	}
	g.processOne(ctx)
	if counts[OutcomeQueueFull] != 1 || counts[OutcomeEnqueued] != 2 || counts[OutcomeForwarded] != 2 || counts[OutcomeDeduped] != 0 {
		t.Fatalf("outcomes: %v", counts)
	}
}

func TestEncodeFailureIsNotForwarded(t *testing.T) {
	counts := map[Outcome]int{}
	sends := 0
	g := New(Config{
		Logger:    silentLogger(),
		Sender:    func(*pb.TransmitFrame) error { sends++; return nil },
		Channels:  map[uint32]ChannelTiming{1: {FullDup: true}},
		OnOutcome: func(_ uint32, _ SubmitSource, o Outcome) { counts[o]++ },
	})
	frame := makeFrame(t, "invalid control")
	frame.Control = 0 // neither UI nor an explicitly encoded connected frame
	if err := g.Submit(context.Background(), 1, frame, SubmitSource{Kind: "digipeater"}); err != nil {
		t.Fatal(err)
	}
	g.processOne(context.Background())
	if sends != 0 || counts[OutcomeEncodeError] != 1 || counts[OutcomeForwarded] != 0 {
		t.Fatalf("sends=%d outcomes=%v", sends, counts)
	}
}
