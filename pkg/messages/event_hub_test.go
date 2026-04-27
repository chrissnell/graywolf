package messages

import (
	"sync"
	"testing"
	"time"
)

func TestEventHubSubscribePublish(t *testing.T) {
	h := NewEventHub(4)
	ch, unsub := h.Subscribe()
	defer unsub()

	h.Publish(Event{Type: EventMessageReceived, MessageID: 42})

	select {
	case got := <-ch:
		if got.Type != EventMessageReceived {
			t.Fatalf("unexpected type: %q", got.Type)
		}
		if got.MessageID != 42 {
			t.Fatalf("unexpected id: %d", got.MessageID)
		}
		if got.Timestamp.IsZero() {
			t.Fatal("expected hub to default Timestamp")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestEventHubMultipleSubscribers(t *testing.T) {
	h := NewEventHub(4)
	chA, unsubA := h.Subscribe()
	chB, unsubB := h.Subscribe()
	defer unsubA()
	defer unsubB()

	h.Publish(Event{Type: EventMessageAcked, MessageID: 7})

	for _, ch := range []<-chan Event{chA, chB} {
		select {
		case got := <-ch:
			if got.MessageID != 7 {
				t.Fatalf("unexpected id: %d", got.MessageID)
			}
		case <-time.After(time.Second):
			t.Fatal("subscriber timed out")
		}
	}
}

func TestEventHubUnsubscribeStopsDelivery(t *testing.T) {
	h := NewEventHub(4)
	ch, unsub := h.Subscribe()
	unsub()
	// Channel should be closed.
	if _, ok := <-ch; ok {
		t.Fatal("expected closed channel after unsubscribe")
	}
	// Publish after unsubscribe — no panic.
	h.Publish(Event{Type: EventMessageReceived})
	if got := h.Subscribers(); got != 0 {
		t.Fatalf("expected 0 subscribers, got %d", got)
	}
}

func TestEventHubUnsubscribeIdempotent(t *testing.T) {
	h := NewEventHub(4)
	_, unsub := h.Subscribe()
	unsub()
	// Second call must not panic or close an already-closed channel.
	unsub()
}

func TestEventHubSlowConsumerDrops(t *testing.T) {
	h := NewEventHub(2)
	ch, unsub := h.Subscribe()
	defer unsub()

	for i := 0; i < 10; i++ {
		h.Publish(Event{Type: EventMessageReceived, MessageID: uint64(i)})
	}
	// Buffer is 2, so 10 publishes → 2 delivered, 8 dropped.
	if got := h.EventsDropped(); got < 8 {
		t.Fatalf("expected at least 8 drops, got %d", got)
	}
	// Drain whatever made it in.
	received := 0
	for i := 0; i < 2; i++ {
		select {
		case <-ch:
			received++
		case <-time.After(50 * time.Millisecond):
		}
	}
	if received == 0 {
		t.Fatal("expected at least one delivered event")
	}
}

func TestEventHubConcurrentSubscribePublish(t *testing.T) {
	h := NewEventHub(64)
	var wg sync.WaitGroup

	// Subscribers.
	subs := make([]func(), 0, 4)
	for i := 0; i < 4; i++ {
		ch, unsub := h.Subscribe()
		subs = append(subs, unsub)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				_, ok := <-ch
				if !ok {
					return
				}
			}
		}()
	}

	// Publishers.
	var pub sync.WaitGroup
	for p := 0; p < 4; p++ {
		pub.Add(1)
		go func() {
			defer pub.Done()
			for i := 0; i < 100; i++ {
				h.Publish(Event{Type: EventMessageReceived, MessageID: uint64(i)})
			}
		}()
	}
	pub.Wait()
	for _, u := range subs {
		u()
	}
	wg.Wait()
}
