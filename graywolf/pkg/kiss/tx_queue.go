package kiss

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// Per-instance tx queue for KISS-TNC egress. One instanceTxQueue lives
// on every Server where AllowTxFromGovernor=true. Dispatcher.Send →
// KissTncBackend.Submit → kiss.Manager.TransmitOnChannel → this queue.
//
// Design (D20): the enqueue path is non-blocking so a stuck or slow
// peer can never stall the TX governor. Writer goroutine drains the
// queue, performs the socket write with a generous 10s deadline
// (purely as a hung-peer guard — slow-but-working links are NOT
// penalised), and returns any failure to a backoff-managed supervisor
// that Phase 4 adds for tcp-client. For server-listen, a write error
// on one connected client does not affect others: the queue's writer
// fans out via the server's existing Broadcast path.

// tnc instance tx queue defaults.
const (
	// instanceTxQueueDepth bounds per-instance pending frames. 16 is a
	// fanout that comfortably absorbs a beacon burst on a single
	// channel without letting a broken peer leak memory.
	instanceTxQueueDepth = 16
	// instanceTxSocketDeadline is the per-write deadline on the
	// drainer's socket write. Ten seconds is generous on purpose —
	// this is a hung-peer guard, not a backpressure mechanism.
	instanceTxSocketDeadline = 10 * time.Second
)

// ErrBackendBusy mirrors txbackend.ErrBackendBusy but lives in the
// kiss package so the manager doesn't have to import txbackend.
// Exported so tests can errors.Is against it.
var (
	ErrBackendBusy = errors.New("kiss: tx queue full")
	ErrBackendDown = errors.New("kiss: tx writer stopped")
)

// txFrame is one enqueued (frame, frameID) pair awaiting the drainer.
// frameID is propagated from the governor for log correlation.
type txFrame struct {
	data    []byte
	frameID uint64
}

// instanceTxQueue is the per-interface bounded tx queue. Owned by a
// Server; drained by a single writer goroutine that this type spawns
// on construction.
type instanceTxQueue struct {
	ch      chan txFrame
	stopped atomic.Bool
	depth   int32 // current queue depth, approximate; atomic

	// writer lifecycle.
	wg     sync.WaitGroup
	cancel context.CancelFunc

	// broadcast is the per-server socket write path. Takes the raw
	// AX.25 frame (no KISS framing yet — broadcast handles that).
	broadcast func(axBytes []byte)

	// onEnqueue / onDrop are optional observation hooks (metrics).
	onEnqueue func()
	onDrop    func(reason string)
	// onDepth reports the current queue depth after every enqueue /
	// drain so the wiring layer can surface a per-instance gauge.
	onDepth func(d int32)
}

// newInstanceTxQueue constructs and starts the per-instance queue.
// broadcast is the server's socket write callback — the drainer calls
// it for every dequeued frame. A single writer goroutine is started
// immediately; it exits when ctx is cancelled OR Close is called.
func newInstanceTxQueue(parent context.Context, broadcast func(axBytes []byte)) *instanceTxQueue {
	ctx, cancel := context.WithCancel(parent)
	q := &instanceTxQueue{
		ch:        make(chan txFrame, instanceTxQueueDepth),
		broadcast: broadcast,
		cancel:    cancel,
	}
	q.wg.Add(1)
	go q.drain(ctx)
	return q
}

// SetObservers installs metric callbacks. Called by the wiring layer
// after construction so the queue stays constructable in unit tests
// without a metrics dependency.
func (q *instanceTxQueue) SetObservers(onEnqueue func(), onDrop func(reason string), onDepth func(int32)) {
	q.onEnqueue = onEnqueue
	q.onDrop = onDrop
	q.onDepth = onDepth
}

// Enqueue hands the frame to the writer. Non-blocking: a full queue
// returns ErrBackendBusy immediately, a stopped queue returns
// ErrBackendDown. Never blocks on I/O.
func (q *instanceTxQueue) Enqueue(frame []byte, frameID uint64) error {
	if q.stopped.Load() {
		if q.onDrop != nil {
			q.onDrop("down")
		}
		return ErrBackendDown
	}
	select {
	case q.ch <- txFrame{data: frame, frameID: frameID}:
		d := atomic.AddInt32(&q.depth, 1)
		if q.onEnqueue != nil {
			q.onEnqueue()
		}
		if q.onDepth != nil {
			q.onDepth(d)
		}
		return nil
	default:
		if q.onDrop != nil {
			q.onDrop("busy")
		}
		return ErrBackendBusy
	}
}

// Close stops the writer goroutine. Idempotent. After Close the queue
// returns ErrBackendDown for every subsequent Enqueue.
func (q *instanceTxQueue) Close() {
	if q.stopped.Swap(true) {
		return
	}
	q.cancel()
	q.wg.Wait()
}

// drain is the writer goroutine. Exits on ctx.Done() or Close.
func (q *instanceTxQueue) drain(ctx context.Context) {
	defer q.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case f := <-q.ch:
			d := atomic.AddInt32(&q.depth, -1)
			if q.onDepth != nil {
				q.onDepth(d)
			}
			if q.broadcast == nil {
				continue
			}
			// The server's Broadcast path already serialises per-client
			// writes via clientConn.mu; setting a socket deadline here
			// would require reaching into individual conns. The 10s
			// deadline is a hung-peer guard implemented inside the
			// broadcast callback when the server wraps writes — see
			// server.go's per-write deadline in BroadcastFromChannel.
			q.broadcast(f.data)
		}
	}
}

// Depth returns the approximate current queue depth. Primarily for
// metrics / test assertions.
func (q *instanceTxQueue) Depth() int32 {
	return atomic.LoadInt32(&q.depth)
}
