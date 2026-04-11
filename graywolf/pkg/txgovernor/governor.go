// Package txgovernor is graywolf's centralized transmit governor. All
// transmit sources — KISS, AGW, beacons, digipeater, iGate IS→RF — funnel
// through a single Governor before frames reach the Rust modem. It
// enforces:
//
//   - Per-channel rate limits (packets/min and packets/5min, sliding window)
//   - Deduplication keyed on (dest + source + info) within a configurable
//     window (default 30s) across all input sources
//   - Priority queue (beacons < digipeated < KISS/AGW < iGate message) so
//     higher-priority traffic preempts lower-priority traffic
//   - DCD-aware timing: before sending, wait until the radio channel is
//     clear (DCD low) and run a p-persistence / slot-time CSMA decision
package txgovernor

import (
	"container/heap"
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/internal/dedup"
	"github.com/chrissnell/graywolf/pkg/internal/ratelimit"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

// Priority levels. Higher value = sent sooner. These re-export the
// canonical constants defined in pkg/ax25 so existing call sites keep
// working; the ax25 package owns the truth so pkg/kiss, pkg/agw, and
// pkg/aprs can reference the same values without importing txgovernor.
const (
	PriorityBeacon     = ax25.PriorityBeacon
	PriorityDigipeated = ax25.PriorityDigipeated
	PriorityClient     = ax25.PriorityClient // KISS/AGW client-originated
	PriorityIGateMsg   = ax25.PriorityIGateMsg
)

// SubmitSource describes the origin of a TX request for logging, dedup
// scoping, and metrics.
type SubmitSource struct {
	Kind     string // "kiss" | "agw" | "beacon" | "digipeater" | "igate"
	Detail   string
	Priority int
}

// Sender transmits one frame to the Rust modem. In production this is
// modembridge.Bridge.SendTransmitFrame; in tests, a fake.
type Sender func(*pb.TransmitFrame) error

// ChannelTiming holds the CSMA parameters for one radio channel. Values
// mirror the tx_timing SQLite row (ms units).
type ChannelTiming struct {
	TxDelayMs uint32
	TxTailMs  uint32
	SlotTime  time.Duration // defaults to 100 ms
	Persist   uint8         // 0..255, default 63
	FullDup   bool          // skip CSMA entirely
}

// Config is the Governor's static configuration.
type Config struct {
	// Sender is the downstream TransmitFrame consumer. Required.
	Sender Sender
	// DcdEvents is an optional channel of per-channel DCD state changes
	// from modembridge. If nil, CSMA falls back to "always clear".
	DcdEvents <-chan *pb.DcdChange
	// Rate1MinLimit and Rate5MinLimit cap the number of frames transmitted
	// per channel in the last 1 and 5 minutes. Zero = unlimited.
	Rate1MinLimit int
	Rate5MinLimit int
	// DedupWindow is the suppression window for identical frames. Default
	// 30s if zero.
	DedupWindow time.Duration
	// QueueCapacity caps the total pending queue. Default 256.
	QueueCapacity int
	// Channels maps channel number to timing parameters. Missing channels
	// use defaults.
	Channels map[uint32]ChannelTiming
	// Logger is optional.
	Logger *slog.Logger
	// RandSource allows tests to inject a deterministic random source for
	// p-persist decisions. Defaults to time-seeded rand.
	RandSource *rand.Rand
}

func (c *Config) applyDefaults() {
	if c.DedupWindow == 0 {
		c.DedupWindow = 30 * time.Second
	}
	if c.QueueCapacity == 0 {
		c.QueueCapacity = 256
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.RandSource == nil {
		c.RandSource = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	if c.Channels == nil {
		c.Channels = make(map[uint32]ChannelTiming)
	}
}

func (c *Config) timingFor(channel uint32) ChannelTiming {
	t, ok := c.Channels[channel]
	if !ok {
		return ChannelTiming{TxDelayMs: 300, TxTailMs: 100, SlotTime: 100 * time.Millisecond, Persist: 63}
	}
	if t.SlotTime == 0 {
		t.SlotTime = 100 * time.Millisecond
	}
	if t.Persist == 0 {
		t.Persist = 63
	}
	return t
}

// TxHook is invoked after a frame has been successfully submitted to the
// downstream Sender. It runs inline in the governor's worker goroutine,
// so implementations must be fast and non-blocking (packetlog record,
// counter bumps, etc). Zero or one hook is supported; use a fanout
// wrapper for multiple consumers.
type TxHook func(channel uint32, frame *ax25.Frame, source SubmitSource)

// Stats exposes counters for metrics.
type Stats struct {
	Enqueued     uint64
	Sent         uint64
	Deduped      uint64
	RateLimited  uint64
	QueueDropped uint64
}

// channelRate holds the pair of sliding-window counters used to enforce
// the 1-minute and 5-minute rate caps on a single channel. Both Windows
// are created lazily per channel on first send.
type channelRate struct {
	oneMin  *ratelimit.Window
	fiveMin *ratelimit.Window
}

// Governor is the centralized TX scheduler.
type Governor struct {
	cfg    Config
	logger *slog.Logger

	mu     sync.Mutex
	q      pqueue
	seq    uint64
	dedup  *dedup.Window[string, struct{}]
	rates  map[uint32]*channelRate // per-channel send rate trackers
	dcd    map[uint32]bool         // current DCD per channel

	wake   chan struct{}
	stats  Stats
	closed bool
	txHook TxHook
}

// SetTxHook installs (or clears) the post-send hook. Safe to call at
// any time. Passing nil removes the hook.
func (g *Governor) SetTxHook(h TxHook) {
	g.mu.Lock()
	g.txHook = h
	g.mu.Unlock()
}

// SetChannelTiming installs or replaces the timing parameters for one
// channel under the governor's lock. Safe to call from startup and
// live-reconfig paths.
func (g *Governor) SetChannelTiming(channel uint32, t ChannelTiming) {
	g.mu.Lock()
	if g.cfg.Channels == nil {
		g.cfg.Channels = make(map[uint32]ChannelTiming)
	}
	g.cfg.Channels[channel] = t
	g.mu.Unlock()
}

// New builds a Governor. Call Run to start its background loop.
func New(cfg Config) *Governor {
	cfg.applyDefaults()
	return &Governor{
		cfg:    cfg,
		logger: cfg.Logger.With("component", "txgovernor"),
		dedup:  dedup.New[string, struct{}](dedup.Config{TTL: cfg.DedupWindow}),
		rates:  make(map[uint32]*channelRate),
		dcd:    make(map[uint32]bool),
		wake:   make(chan struct{}, 1),
	}
}

// Submit enqueues a frame. Deduplicates, rate-checks are deferred to the
// worker loop so Submit never blocks on the channel. Returns an error if
// the queue is full, the governor is closed, or the caller's context has
// already been cancelled. Submit honors ctx so that a caller-imposed
// deadline (e.g. the iGate's IS->RF timeout) propagates into the
// governor's accept path and so that a cancelled session cannot be
// charged a newly enqueued frame.
func (g *Governor) Submit(ctx context.Context, channel uint32, frame *ax25.Frame, src SubmitSource) error {
	if frame == nil {
		return errors.New("txgovernor: nil frame")
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	now := time.Now()

	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return errors.New("txgovernor: closed")
	}

	// Dedup check (Has also GCs expired entries opportunistically).
	key := frame.DedupKey()
	if g.dedup.Has(key) {
		g.stats.Deduped++
		g.mu.Unlock()
		g.logger.Debug("tx deduped", "source", src.Kind, "key-len", len(key))
		return nil
	}

	// Capacity check before recording dedup: if we reject the frame, we
	// must not poison the dedup map, or the caller's retry within the
	// window would be silently suppressed with zero visibility. This is
	// why the governor uses Has+Record rather than the atomic Seen.
	if len(g.q) >= g.cfg.QueueCapacity {
		g.stats.QueueDropped++
		g.mu.Unlock()
		return errors.New("txgovernor: queue full")
	}

	g.dedup.Record(key, struct{}{})
	g.seq++
	heap.Push(&g.q, &queueItem{
		channel:  channel,
		frame:    frame,
		source:   src,
		priority: src.Priority,
		seq:      g.seq,
		enqueued: now,
	})
	g.stats.Enqueued++
	g.mu.Unlock()

	select {
	case g.wake <- struct{}{}:
	default:
	}
	return nil
}

// Run executes the worker loop until ctx is cancelled. It consumes DCD
// events from cfg.DcdEvents, drains the queue, and calls Sender. Blocks.
func (g *Governor) Run(ctx context.Context) error {
	// Fan in DCD events into the governor state.
	go g.dcdWatcher(ctx)

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		g.processOne(ctx)

		select {
		case <-ctx.Done():
			g.mu.Lock()
			g.closed = true
			g.mu.Unlock()
			return nil
		case <-g.wake:
		case <-ticker.C:
		}
	}
}

func (g *Governor) dcdWatcher(ctx context.Context) {
	if g.cfg.DcdEvents == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-g.cfg.DcdEvents:
			if !ok {
				return
			}
			if ev == nil {
				continue
			}
			g.mu.Lock()
			g.dcd[ev.Channel] = ev.Detected
			g.mu.Unlock()
			// DCD dropped: wake the worker so it can attempt to send.
			if !ev.Detected {
				select {
				case g.wake <- struct{}{}:
				default:
				}
			}
		}
	}
}

// processOne pops the highest-priority frame eligible for sending right
// now and sends it. If the head item is rate-limited or DCD-blocked, it
// is left in place and the function returns.
func (g *Governor) processOne(ctx context.Context) {
	g.mu.Lock()
	if len(g.q) == 0 {
		g.mu.Unlock()
		return
	}
	top := g.q[0]
	if g.isRateLimitedLocked(top.channel, time.Now()) {
		// Only count the first observation of this held item so
		// repeated ticks do not inflate the counter.
		if !top.rateLimitCounted {
			g.stats.RateLimited++
			top.rateLimitCounted = true
		}
		g.mu.Unlock()
		return
	}
	// Head is no longer rate-limited; clear the flag so a future
	// re-throttle (rate window fills again) counts fresh.
	top.rateLimitCounted = false
	timing := g.cfg.timingFor(top.channel)
	channelBusy := g.dcd[top.channel]
	// p-persistence roll is done under g.mu because *math/rand.Rand is
	// not safe for concurrent use. Today processOne runs from a single
	// goroutine, but taking the lock here makes the invariant explicit.
	var persistDefer bool
	if !timing.FullDup && !channelBusy {
		roll := byte(g.cfg.RandSource.Intn(256))
		persistDefer = roll > timing.Persist
	}
	g.mu.Unlock()

	if channelBusy && !timing.FullDup {
		// Wait up to one slot and retry on the next tick / DCD event.
		return
	}

	if persistDefer {
		// Defer by one slot.
		time.Sleep(timing.SlotTime)
		return
	}

	// Pop under lock, then send.
	g.mu.Lock()
	if len(g.q) == 0 || g.q[0] != top {
		// Something changed while we were deciding; retry next tick.
		g.mu.Unlock()
		return
	}
	heap.Pop(&g.q)
	g.recordSendLocked(top.channel, time.Now())
	g.stats.Sent++
	g.mu.Unlock()

	raw, err := top.frame.Encode()
	if err != nil {
		g.logger.Warn("encode frame", "err", err)
		return
	}
	tf := &pb.TransmitFrame{
		Channel:           top.channel,
		Data:              raw,
		TxdelayOverrideMs: timing.TxDelayMs,
		TxtailOverrideMs:  timing.TxTailMs,
		Priority:          uint32(top.priority),
	}
	if err := g.cfg.Sender(tf); err != nil {
		g.logger.Warn("send transmit frame", "err", err, "channel", top.channel)
	} else {
		g.mu.Lock()
		hook := g.txHook
		g.mu.Unlock()
		if hook != nil {
			hook(top.channel, top.frame, top.source)
		}
	}
	// Touch ctx just to satisfy lint if unused on some paths.
	_ = ctx
}

// rateFor returns the per-channel rate tracker, creating it on first
// use. Must be called with g.mu held.
func (g *Governor) rateFor(channel uint32) *channelRate {
	r, ok := g.rates[channel]
	if !ok {
		r = &channelRate{
			oneMin:  ratelimit.New(1 * time.Minute),
			fiveMin: ratelimit.New(5 * time.Minute),
		}
		g.rates[channel] = r
	}
	return r
}

func (g *Governor) isRateLimitedLocked(channel uint32, _ time.Time) bool {
	if g.cfg.Rate1MinLimit == 0 && g.cfg.Rate5MinLimit == 0 {
		return false
	}
	r := g.rateFor(channel)
	if g.cfg.Rate5MinLimit > 0 && r.fiveMin.Count() >= g.cfg.Rate5MinLimit {
		return true
	}
	if g.cfg.Rate1MinLimit > 0 && r.oneMin.Count() >= g.cfg.Rate1MinLimit {
		return true
	}
	return false
}

func (g *Governor) recordSendLocked(channel uint32, _ time.Time) {
	r := g.rateFor(channel)
	r.oneMin.Record()
	r.fiveMin.Record()
}

// Stats returns a snapshot of the counters.
func (g *Governor) Stats() Stats {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.stats
}

// QueueLen returns the current number of pending frames.
func (g *Governor) QueueLen() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.q)
}
