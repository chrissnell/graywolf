package packetlog

import (
	"context"
	"sync"
	"sync/atomic"
)

// subscriberBuf bounds each subscriber's channel. The Record path is
// non-blocking: when a subscriber's buffer is full the entry is dropped
// and dropCount is bumped. 256 absorbs typical bursts (full window of
// inbound APRS frames) without keeping the writer goroutine waiting.
const subscriberBuf = 256

// subscriber is one active Subscribe consumer.
type subscriber struct {
	ch      chan Entry
	drops   atomic.Uint64
	closeFn func()
}

// Subscribe returns a channel that receives every Entry recorded after
// the call until ctx is cancelled. The channel is closed when the
// subscription ends (ctx done OR the Log is closed). The non-blocking
// fanout drops on slow consumers and bumps a per-subscriber counter so
// callers can detect the loss; total drops are exposed via SubscribeStats.
//
// Subscribe never replays existing buffered entries -- callers that need
// historical context must call Query first, then Subscribe for tail.
//
// Multiple subscribers are independent; each gets its own channel.
func (l *Log) Subscribe(ctx context.Context) <-chan Entry {
	l.subMu.Lock()
	if l.subs == nil {
		l.subs = make(map[*subscriber]struct{})
	}
	s := &subscriber{ch: make(chan Entry, subscriberBuf)}
	once := sync.Once{}
	s.closeFn = func() {
		once.Do(func() {
			l.subMu.Lock()
			delete(l.subs, s)
			l.subMu.Unlock()
			close(s.ch)
		})
	}
	l.subs[s] = struct{}{}
	l.subMu.Unlock()
	go func() {
		<-ctx.Done()
		s.closeFn()
	}()
	return s.ch
}

// SubscribeStats reports an aggregate snapshot of all live subscribers.
// Used by /metrics or test assertions to detect ongoing drops without
// having to track each subscriber individually.
type SubscribeStats struct {
	Subscribers int
	TotalDrops  uint64
}

// SubscribeStats returns a snapshot of the fanout state.
func (l *Log) SubscribeStats() SubscribeStats {
	l.subMu.Lock()
	defer l.subMu.Unlock()
	var total uint64
	for s := range l.subs {
		total += s.drops.Load()
	}
	return SubscribeStats{Subscribers: len(l.subs), TotalDrops: total}
}

// fanout pushes one Entry to every live subscriber. Non-blocking: a
// subscriber whose channel is full drops the entry and bumps its
// drops counter. Called by Record under l.mu held.
func (l *Log) fanout(e Entry) {
	l.subMu.Lock()
	if len(l.subs) == 0 {
		l.subMu.Unlock()
		return
	}
	subs := make([]*subscriber, 0, len(l.subs))
	for s := range l.subs {
		subs = append(subs, s)
	}
	l.subMu.Unlock()
	for _, s := range subs {
		select {
		case s.ch <- e:
		default:
			s.drops.Add(1)
		}
	}
}
