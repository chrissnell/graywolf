package modembridge

import "sync"

// dispatcher correlates numeric request IDs with single-use reply channels.
// It is safe for concurrent use.
//
// A single dispatcher is kept per request kind (enumerate audio, play test
// tone, scan input levels, ...) for the bridge's lifetime. supervise() calls
// Reset at the top of every iteration and Close in its defer, so a dispatcher
// is usable again after its owning bridge restarts.
type dispatcher[Resp any] struct {
	mu      sync.Mutex
	nextID  uint32
	pending map[uint32]chan Resp
	closed  bool
}

func newDispatcher[Resp any]() *dispatcher[Resp] {
	return &dispatcher[Resp]{
		pending: make(map[uint32]chan Resp),
	}
}

// Register allocates a new request ID and a buffered reply channel. The
// caller must consume the channel exactly once (or defer Cancel to drop
// its registration on early return). If the dispatcher is already Closed,
// Register returns a closed channel so the caller's select fires
// immediately with a zero-value Resp, which every caller treats as
// "bridge stopped".
func (d *dispatcher[Resp]) Register() (id uint32, ch <-chan Resp) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		c := make(chan Resp)
		close(c)
		return 0, c
	}
	d.nextID++
	id = d.nextID
	c := make(chan Resp, 1)
	d.pending[id] = c
	return id, c
}

// Cancel drops the pending registration for id. Callers use it in a defer
// so a late Deliver does not leave an orphan entry in the pending map.
func (d *dispatcher[Resp]) Cancel(id uint32) {
	d.mu.Lock()
	delete(d.pending, id)
	d.mu.Unlock()
}

// Deliver routes resp to the channel registered for id. Returns false if
// no caller is waiting (late reply; drop it) or if the channel's single
// slot is already full (duplicate delivery; drop it).
func (d *dispatcher[Resp]) Deliver(id uint32, resp Resp) bool {
	d.mu.Lock()
	ch, ok := d.pending[id]
	d.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- resp:
		return true
	default:
		return false
	}
}

// Close closes every pending reply channel so callers blocked in their
// per-call select unblock immediately with a zero-value Resp. After Close,
// Register returns a closed channel until Reset runs. Idempotent.
//
// Must only be called when no Deliver caller is in flight (i.e. from the
// bridge's defer chain, after the session goroutine has already returned).
// Otherwise a concurrent Deliver could race a send on a closed channel.
func (d *dispatcher[Resp]) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	d.closed = true
	for id, ch := range d.pending {
		close(ch)
		delete(d.pending, id)
	}
}

// Reset reopens a dispatcher that was previously Closed so the next
// supervise iteration can reuse it. It is safe to call on a fresh
// dispatcher. Reset does not touch nextID so IDs remain monotonic across
// restart cycles.
func (d *dispatcher[Resp]) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = false
	if d.pending == nil {
		d.pending = make(map[uint32]chan Resp)
	}
}
