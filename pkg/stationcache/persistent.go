package stationcache

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// writeQueueCapacity bounds the async persistence queue shared by both the
// RF ingest path (dispatchRxFrame) and the APRS-IS ingest path
// (onIGateIsRxPacket). Sized to absorb a multi-second burst from either
// side without unbounded memory growth; on overflow the oldest-style
// backpressure is a drop-and-count rather than blocking either caller.
const writeQueueCapacity = 512

// writeFlushInterval is how often the async writer goroutine coalesces
// pending CacheEntry batches and RxEvents into one SQLite transaction each.
// Short enough that the Live Map / heatmap still feel real-time; long
// enough to collapse a busy APRS-IS feed's per-packet write cost into a
// handful of commits per second instead of one transaction per packet.
const writeFlushInterval = 150 * time.Millisecond

// writeDropLogInterval rate-limits the "queue full" warning so a sustained
// overflow cannot flood the log.
const writeDropLogInterval = 10 * time.Second

// writeJob is one unit of pending persistence work. Exactly one of
// entries/rxEvent is set; kept as a single type so both producers
// (Update, RecordRxEvent) share one queue and one writer goroutine.
type writeJob struct {
	entries []CacheEntry
	rxEvent *RxEvent
}

// PersistentCache wraps a MemCache with optional SQLite persistence.
// When persistence is disabled, it behaves identically to a bare
// MemCache. Persistence can be toggled at runtime via Reconfigure.
//
// The in-memory update (MemCache.Update) stays synchronous — it's cheap
// and needs to be visible to readers immediately. The SQLite write does
// not: it is handed off to a single background writer goroutine via
// queue, so neither the RF ingest path nor the APRS-IS ingest path ever
// blocks on (or contends over) the history database's single connection.
// Both paths call Update/RecordRxEvent from independent goroutines; before
// this queue existed they shared hdb's *sql.DB (SetMaxOpenConns(1))
// directly, so a busy APRS-IS feed could serialize against, and delay,
// the latency-sensitive RF path (graywolf packet-loss report, 2026-09).
type PersistentCache struct {
	mu     sync.RWMutex
	mem    *MemCache
	hdb    HistoryStore  // nil when disabled
	done   chan struct{} // signals prune + writer goroutines; nil when disabled
	queue  chan writeJob // nil when disabled
	wg     sync.WaitGroup
	logger *slog.Logger

	writeDropped    atomic.Uint64
	lastDropLogNano atomic.Int64
}

var _ StationStore = (*PersistentCache)(nil)

const (
	memMaxAge     = 24 * time.Hour
	pruneInterval = 1 * time.Hour
	pruneMaxAge   = 30 * 24 * time.Hour // 30 days
)

// NewPersistentCache creates a PersistentCache with persistence
// disabled. Call Reconfigure to enable it.
func NewPersistentCache(logger *slog.Logger) *PersistentCache {
	return &PersistentCache{
		mem:    NewMemCache(memMaxAge),
		logger: logger,
	}
}

// Update applies entries to the in-memory cache synchronously and, if
// persistence is enabled, enqueues them for the async history writer.
// Never blocks: a full queue drops the batch and counts it rather than
// stalling the caller (which may be the RF dispatch goroutine).
func (p *PersistentCache) Update(entries []CacheEntry) {
	p.mem.Update(entries)

	p.mu.RLock()
	q := p.queue
	p.mu.RUnlock()
	if q == nil {
		return
	}
	select {
	case q <- writeJob{entries: entries}:
	default:
		p.writeDropped.Add(1)
		p.logDropRateLimited()
	}
}

// WriteDropped returns the number of persistence jobs (station-cache
// batches or rx_events) dropped because the async writer's queue was
// full. Zero when persistence is disabled or the writer is keeping up.
func (p *PersistentCache) WriteDropped() uint64 {
	return p.writeDropped.Load()
}

// logDropRateLimited emits a rate-limited warning when the async
// persistence queue overflows, so a sustained overflow cannot flood the
// log the way an unthrottled per-drop warning would.
func (p *PersistentCache) logDropRateLimited() {
	if p.logger == nil {
		return
	}
	now := time.Now().UnixNano()
	last := p.lastDropLogNano.Load()
	if now-last < int64(writeDropLogInterval) {
		return
	}
	if !p.lastDropLogNano.CompareAndSwap(last, now) {
		return
	}
	p.logger.Warn("history write queue full; dropping station cache persistence job",
		"queue_cap", writeQueueCapacity, "dropped_total", p.writeDropped.Load())
}

// QueryBBox delegates to the in-memory cache.
func (p *PersistentCache) QueryBBox(bbox BBox, maxAge time.Duration) []Station {
	return p.mem.QueryBBox(bbox, maxAge)
}

// Lookup delegates to the in-memory cache.
func (p *PersistentCache) Lookup(callsigns []string) map[string]LatLon {
	return p.mem.Lookup(callsigns)
}

// QueryHeatmap returns aggregated directly-received-packet heat over the
// window within bbox. Returns an empty result when persistence is disabled.
func (p *PersistentCache) QueryHeatmap(window time.Duration, bbox BBox) (*HeatmapResult, error) {
	p.mu.RLock()
	hdb := p.hdb
	p.mu.RUnlock()
	if hdb == nil {
		return &HeatmapResult{}, nil
	}
	return hdb.QueryHeatmap(window, bbox)
}

// RecordRxEvent enqueues one heatmap reception event for the async writer,
// best-effort. It is a no-op when persistence is disabled. Called once per
// RF frame from the ingest edge and once per IS packet from the iGate's
// IsRxHook; never blocks the caller (see Update).
func (p *PersistentCache) RecordRxEvent(ev RxEvent) {
	p.mu.RLock()
	q := p.queue
	p.mu.RUnlock()
	if q == nil {
		return
	}
	e := ev
	select {
	case q <- writeJob{rxEvent: &e}:
	default:
		p.writeDropped.Add(1)
		p.logDropRateLimited()
	}
}

// Gen returns the in-memory generation counter (ETag support).
func (p *PersistentCache) Gen() uint64 {
	return p.mem.Gen()
}

// Reconfigure enables, disables, or changes the persistence backend.
// Pass a non-nil HistoryStore to enable, or nil to disable.
func (p *PersistentCache) Reconfigure(hdb HistoryStore) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Shut down previous persistence if any.
	p.stopLocked()

	if hdb == nil {
		return nil
	}

	// Hydrate the in-memory cache from the history database.
	stations, err := hdb.LoadRecent(memMaxAge, MaxTrailLen)
	if err != nil {
		hdb.Close()
		return err
	}
	if len(stations) > 0 {
		p.mem.Hydrate(stations)
		p.logger.Info("hydrated station cache from history db", "stations", len(stations))
	}

	p.hdb = hdb
	p.done = make(chan struct{})
	p.queue = make(chan writeJob, writeQueueCapacity)
	p.wg.Add(2)
	go p.pruneLoop(p.done, hdb)
	go p.writeLoop(p.done, hdb, p.queue)
	return nil
}

// Close shuts down persistence and the in-memory cache.
func (p *PersistentCache) Close() {
	p.mu.Lock()
	p.stopLocked()
	p.mu.Unlock()
	p.mem.Close()
}

// stopLocked closes the history database and stops the prune + writer
// goroutines. Caller must hold p.mu. Waits for both goroutines to exit
// before closing hdb so the writer's final flush can never race a closed
// DB handle.
func (p *PersistentCache) stopLocked() {
	if p.done != nil {
		close(p.done)
		p.wg.Wait()
		p.done = nil
	}
	p.queue = nil
	if p.hdb != nil {
		p.hdb.Close()
		p.hdb = nil
	}
}

func (p *PersistentCache) pruneLoop(done chan struct{}, hdb HistoryStore) {
	defer p.wg.Done()
	ticker := time.NewTicker(pruneInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if err := hdb.Prune(pruneMaxAge); err != nil {
				p.logger.Warn("history prune failed", "err", err)
			}
		}
	}
}

// writeLoop is the single background writer that owns every SQLite write
// against hdb. Both the RF ingest path and the APRS-IS ingest path only
// ever enqueue onto queue (see Update/RecordRxEvent); this goroutine is the
// sole caller of hdb.WriteEntries/RecordRxEvents, so the two ingest paths
// never contend over the history database's single connection. Pending
// work is coalesced over writeFlushInterval into one transaction each
// (station-cache batches, rx_events) instead of one transaction per packet.
func (p *PersistentCache) writeLoop(done chan struct{}, hdb HistoryStore, queue chan writeJob) {
	defer p.wg.Done()
	ticker := time.NewTicker(writeFlushInterval)
	defer ticker.Stop()

	var pendingEntries []CacheEntry
	var pendingEvents []RxEvent

	flush := func() {
		if len(pendingEntries) > 0 {
			if err := hdb.WriteEntries(pendingEntries); err != nil {
				p.logger.Warn("history write failed", "err", err, "batch", len(pendingEntries))
			}
			pendingEntries = nil
		}
		if len(pendingEvents) > 0 {
			if err := hdb.RecordRxEvents(pendingEvents); err != nil {
				p.logger.Warn("heatmap rx_event write failed", "err", err, "batch", len(pendingEvents))
			}
			pendingEvents = nil
		}
	}
	enqueue := func(job writeJob) {
		if job.rxEvent != nil {
			pendingEvents = append(pendingEvents, *job.rxEvent)
			return
		}
		pendingEntries = append(pendingEntries, job.entries...)
	}

	for {
		select {
		case <-done:
			// Drain whatever is already queued so a clean shutdown doesn't
			// lose the last burst of updates, then flush once more.
			for {
				select {
				case job := <-queue:
					enqueue(job)
				default:
					flush()
					return
				}
			}
		case job := <-queue:
			enqueue(job)
		case <-ticker.C:
			flush()
		}
	}
}
