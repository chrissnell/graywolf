package messages

import (
	"sync"
	"sync/atomic"
	"time"
)

// Event types emitted by the router and sender. The set is open —
// additional types may be added in later phases (e.g. message.deleted
// from REST soft-delete).
const (
	EventMessageReceived     = "message.received"
	EventMessageAcked        = "message.acked"
	EventMessageRejected     = "message.rejected"
	EventMessageReplyAckRcvd = "message.reply_ack_received"
	// EventMessageSentRF is emitted when the TxHook confirms the
	// governor sent an outbound RF frame we originated. SentAt on the
	// row is flipped in the same transaction.
	EventMessageSentRF = "message.sent_rf"
	// EventMessageSentIS is emitted when the APRS-IS SendLine call
	// returned nil for an operator-originated outbound.
	EventMessageSentIS = "message.sent_is"
	// EventMessageFailed is emitted when the sender has exhausted
	// its retry budget on a DM or hit a terminal governor error.
	EventMessageFailed = "message.failed"
	// EventMessageDeleted is emitted when an operator soft-deletes
	// an outbound or inbound row via REST.
	EventMessageDeleted = "message.deleted"
)

// Event is the payload delivered to subscribers. MessageID is 0 for
// events that do not correspond to a stored row (reserved for future
// use). ThreadKind/ThreadKey may be empty when the event is not
// thread-scoped.
type Event struct {
	Type       string
	MessageID  uint64
	ThreadKind string
	ThreadKey  string
	Timestamp  time.Time
}

// EventHub is a small non-blocking pub/sub. Subscribers receive
// events on a buffered channel; a slow consumer drops events rather
// than blocking the publisher. The default buffer size is 32 — large
// enough to absorb a burst of classifications without dropping, small
// enough to make a buggy consumer visible quickly via the
// EventsDropped counter.
type EventHub struct {
	mu          sync.RWMutex
	subscribers map[int]*subscription
	next        int
	bufSize     int
	dropped     atomic.Uint64
}

type subscription struct {
	ch chan Event
}

// DefaultSubscriberBuffer controls per-subscriber buffering.
const DefaultSubscriberBuffer = 32

// NewEventHub constructs an empty hub. Pass bufSize <= 0 to use the
// default.
func NewEventHub(bufSize int) *EventHub {
	if bufSize <= 0 {
		bufSize = DefaultSubscriberBuffer
	}
	return &EventHub{
		subscribers: make(map[int]*subscription),
		bufSize:     bufSize,
	}
}

// Subscribe registers a listener and returns its receive channel plus
// an unsubscribe closure. The closure is idempotent. Closing the
// channel is the hub's responsibility — the caller must NOT close it.
func (h *EventHub) Subscribe() (<-chan Event, func()) {
	sub := &subscription{ch: make(chan Event, h.bufSize)}
	h.mu.Lock()
	id := h.next
	h.next++
	h.subscribers[id] = sub
	h.mu.Unlock()

	var once sync.Once
	unsub := func() {
		once.Do(func() {
			h.mu.Lock()
			if _, ok := h.subscribers[id]; ok {
				delete(h.subscribers, id)
				close(sub.ch)
			}
			h.mu.Unlock()
		})
	}
	return sub.ch, unsub
}

// Publish broadcasts e to all current subscribers. Each send is
// non-blocking; a slow subscriber's event is dropped and the
// EventsDropped counter advances. Publish never blocks the caller.
func (h *EventHub) Publish(e Event) {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, sub := range h.subscribers {
		select {
		case sub.ch <- e:
		default:
			h.dropped.Add(1)
		}
	}
}

// EventsDropped returns the cumulative number of events that could
// not be delivered because a subscriber's buffer was full.
func (h *EventHub) EventsDropped() uint64 {
	return h.dropped.Load()
}

// Subscribers returns the current number of live subscribers (for
// metrics / tests).
func (h *EventHub) Subscribers() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}
