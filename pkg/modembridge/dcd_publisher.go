package modembridge

import (
	"log/slog"
	"sync"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

// dcdPublisher is a non-blocking fan-out for DcdChange events. Slow
// subscribers drop rather than stalling the IPC read loop; drops are
// reported via incDropped (for metrics) and at debug level through the
// logger with a 10-second rate limit per publisher.
//
// Lifetime: constructed once at Bridge creation, closed once from the
// supervise() defer chain when Stop is called. After Close, every
// subscriber channel is closed and Subscribe returns an already-closed
// channel.
type dcdPublisher struct {
	mu         sync.Mutex
	subs       []chan *pb.DcdChange
	closed     bool
	logger     *slog.Logger
	incDropped func() // optional metrics hook; nil is a no-op
	lastDropAt time.Time
}

// dcdPublisherBufferSize is the per-subscriber channel capacity. It
// matches the historical Bridge.dcd buffer (64) and is big enough to
// absorb ordinary IPC bursts without dropping.
const dcdPublisherBufferSize = 64

func newDcdPublisher(logger *slog.Logger, incDropped func()) *dcdPublisher {
	if logger == nil {
		logger = slog.Default()
	}
	return &dcdPublisher{
		logger:     logger,
		incDropped: incDropped,
	}
}

// Subscribe returns a new buffered channel that will receive every
// published event until the caller Unsubscribes or the publisher Closes.
// After Close, Subscribe returns an already-closed channel so callers
// see an immediate EOF rather than blocking forever.
func (p *dcdPublisher) Subscribe() <-chan *pb.DcdChange {
	p.mu.Lock()
	defer p.mu.Unlock()
	ch := make(chan *pb.DcdChange, dcdPublisherBufferSize)
	if p.closed {
		close(ch)
		return ch
	}
	p.subs = append(p.subs, ch)
	return ch
}

// Unsubscribe removes ch from the subscriber list and closes it so a
// ranging consumer exits cleanly. Safe to call multiple times (later
// calls are no-ops). Without this, a subscriber whose caller dies would
// leak memory on every fan-out Publish.
func (p *dcdPublisher) Unsubscribe(ch <-chan *pb.DcdChange) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, c := range p.subs {
		// Compare by channel identity. Each subscription returns a
		// distinct chan so this match is unambiguous.
		if (<-chan *pb.DcdChange)(c) == ch {
			p.subs = append(p.subs[:i], p.subs[i+1:]...)
			close(c)
			return
		}
	}
}

// Publish sends ev to every subscriber non-blockingly. A subscriber whose
// buffer is full has its delivery dropped and the drop is accounted for
// via incDropped and a rate-limited debug log.
func (p *dcdPublisher) Publish(ev *pb.DcdChange) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	// Snapshot under lock so we can Publish without holding the mutex
	// during the per-subscriber send.
	subs := append([]chan *pb.DcdChange(nil), p.subs...)
	p.mu.Unlock()

	var dropped int
	for _, c := range subs {
		select {
		case c <- ev:
		default:
			dropped++
		}
	}
	if dropped == 0 {
		return
	}
	if p.incDropped != nil {
		for i := 0; i < dropped; i++ {
			p.incDropped()
		}
	}
	// Rate-limit debug logs to once per 10s to avoid spamming during
	// sustained back-pressure.
	p.mu.Lock()
	now := time.Now()
	if now.Sub(p.lastDropAt) > 10*time.Second {
		p.lastDropAt = now
		p.mu.Unlock()
		p.logger.Debug("dcd publisher dropped events",
			"dropped", dropped,
			"subscribers", len(subs))
		return
	}
	p.mu.Unlock()
}

// Close closes every subscriber channel so range consumers exit cleanly
// and marks the publisher closed. After Close, Subscribe returns
// already-closed channels and Publish is a no-op. Idempotent.
func (p *dcdPublisher) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	for _, c := range p.subs {
		close(c)
	}
	p.subs = nil
}
