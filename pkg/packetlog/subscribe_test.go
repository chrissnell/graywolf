package packetlog

import (
	"context"
	"testing"
	"time"
)

func TestSubscribeReceivesNewEntries(t *testing.T) {
	t.Parallel()
	l := New(Config{Capacity: 8, MaxAge: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := l.Subscribe(ctx)
	l.Record(Entry{Source: "K0SWE", Type: "p"})
	select {
	case got := <-ch:
		if got.Source != "K0SWE" {
			t.Fatalf("got %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber never received entry")
	}
}

func TestSubscribeMultipleSubscribersIndependent(t *testing.T) {
	t.Parallel()
	l := New(Config{Capacity: 8, MaxAge: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch1 := l.Subscribe(ctx)
	ch2 := l.Subscribe(ctx)
	l.Record(Entry{Source: "A"})
	for _, ch := range []<-chan Entry{ch1, ch2} {
		select {
		case e := <-ch:
			if e.Source != "A" {
				t.Fatalf("source drift: %s", e.Source)
			}
		case <-time.After(time.Second):
			t.Fatal("a subscriber missed the entry")
		}
	}
}

func TestSubscribeContextCancelClosesChannel(t *testing.T) {
	t.Parallel()
	l := New(Config{Capacity: 8, MaxAge: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	ch := l.Subscribe(ctx)
	cancel()
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // channel closed -> success
			}
		case <-deadline:
			t.Fatal("channel never closed after ctx cancel")
		}
	}
}

func TestSubscribeDropsOnSlowConsumer(t *testing.T) {
	t.Parallel()
	l := New(Config{Capacity: 8, MaxAge: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = l.Subscribe(ctx) // never drained
	for i := 0; i < subscriberBuf+50; i++ {
		l.Record(Entry{Source: "X"})
	}
	stats := l.SubscribeStats()
	if stats.Subscribers != 1 {
		t.Fatalf("expected 1 subscriber, got %d", stats.Subscribers)
	}
	if stats.TotalDrops == 0 {
		t.Fatal("expected drops on slow consumer")
	}
}

func TestSubscribeRecordOrderingPreserved(t *testing.T) {
	t.Parallel()
	l := New(Config{Capacity: 16, MaxAge: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := l.Subscribe(ctx)
	for i := 0; i < 5; i++ {
		l.Record(Entry{Source: "A", Type: string(rune('0' + i))})
	}
	for i := 0; i < 5; i++ {
		select {
		case got := <-ch:
			want := string(rune('0' + i))
			if got.Type != want {
				t.Fatalf("entry %d: type=%q want %q", i, got.Type, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("missed entry %d", i)
		}
	}
}
