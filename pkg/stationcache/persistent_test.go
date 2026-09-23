package stationcache

import (
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/internal/testsync"
)

// fakePersistStore is an in-memory HistoryStore double used to verify
// PersistentCache's async writer without touching real SQLite. All
// exported methods are safe for concurrent use since the writer goroutine
// and the test's assertions run concurrently.
type fakePersistStore struct {
	mu sync.Mutex

	// writeEntriesCalls records the batch size of every WriteEntries
	// invocation, in call order, so tests can assert coalescing behavior.
	writeEntriesCalls [][]CacheEntry
	rxEventsCalls     [][]RxEvent

	// blockWrites, when non-nil, is closed by the test to unblock a
	// WriteEntries call that the test deliberately stalled — used to
	// prove Update()/RecordRxEvent() never block the caller even while
	// the writer goroutine is stuck mid-flush.
	blockWrites chan struct{}

	closed bool
}

func (f *fakePersistStore) WriteEntries(entries []CacheEntry) error {
	if f.blockWrites != nil {
		<-f.blockWrites
	}
	cp := append([]CacheEntry(nil), entries...)
	f.mu.Lock()
	f.writeEntriesCalls = append(f.writeEntriesCalls, cp)
	f.mu.Unlock()
	return nil
}

func (f *fakePersistStore) RecordRxEvent(ev RxEvent) error {
	return f.RecordRxEvents([]RxEvent{ev})
}

func (f *fakePersistStore) RecordRxEvents(evs []RxEvent) error {
	cp := append([]RxEvent(nil), evs...)
	f.mu.Lock()
	f.rxEventsCalls = append(f.rxEventsCalls, cp)
	f.mu.Unlock()
	return nil
}

func (f *fakePersistStore) LoadRecent(time.Duration, int) (map[string]*Station, error) {
	return nil, nil
}
func (f *fakePersistStore) Prune(time.Duration) error { return nil }
func (f *fakePersistStore) QueryHeatmap(time.Duration, BBox) (*HeatmapResult, error) {
	return &HeatmapResult{}, nil
}
func (f *fakePersistStore) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

func (f *fakePersistStore) entryBatchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.writeEntriesCalls)
}

func (f *fakePersistStore) totalEntries() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, b := range f.writeEntriesCalls {
		n += len(b)
	}
	return n
}

func (f *fakePersistStore) totalRxEvents() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, b := range f.rxEventsCalls {
		n += len(b)
	}
	return n
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, nil))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// TestPersistentCache_UpdateNeverBlocks proves the async writer's core
// contract: Update() (and RecordRxEvent()) return immediately even while
// the history store is stalled mid-write, so a slow/contended SQLite
// connection can never stall the RF or IS ingest goroutines that call
// them.
func TestPersistentCache_UpdateNeverBlocks(t *testing.T) {
	store := &fakePersistStore{blockWrites: make(chan struct{})}
	p := NewPersistentCache(quietLogger())
	if err := p.Reconfigure(store); err != nil {
		t.Fatalf("Reconfigure: %v", err)
	}
	// Unblock the writer's stalled flush, then close the cache -- in that
	// order (defers run LIFO), so Close's wg.Wait() doesn't itself hang
	// on a writer goroutine we deliberately stalled for this test.
	defer p.Close()
	defer close(store.blockWrites)

	// First Update seeds a write that will block inside the fake store
	// once the writer's flush ticker fires. Subsequent calls must still
	// return immediately regardless.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			p.Update([]CacheEntry{{Key: "stn:TEST", Callsign: "TEST", HasPos: true, Timestamp: time.Now()}})
			p.RecordRxEvent(RxEvent{Timestamp: time.Now(), AttrKey: "stn:TEST"})
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Update/RecordRxEvent blocked despite a stalled history store")
	}
}

// TestPersistentCache_CoalescesBatches verifies that multiple Update calls
// within one flush window land in a single WriteEntries transaction
// instead of one transaction per call -- the mechanism that removes
// per-packet SQLite contention between the RF and APRS-IS ingest paths.
func TestPersistentCache_CoalescesBatches(t *testing.T) {
	store := &fakePersistStore{}
	p := NewPersistentCache(quietLogger())
	if err := p.Reconfigure(store); err != nil {
		t.Fatalf("Reconfigure: %v", err)
	}
	defer p.Close()

	for i := 0; i < 20; i++ {
		p.Update([]CacheEntry{{Key: "stn:A", Callsign: "A", HasPos: true, Timestamp: time.Now()}})
	}

	testsync.WaitFor(t, func() bool { return store.totalEntries() == 20 }, 2*time.Second,
		"writer to flush all entries")

	if got := store.entryBatchCount(); got == 0 || got >= 20 {
		t.Fatalf("WriteEntries call count = %d, want a small number of coalesced batches (not 0, not one per Update)", got)
	}
}

// TestPersistentCache_ConcurrentRFAndISProducers simulates the exact
// contention scenario from the packet-loss report: two goroutines
// (standing in for the RF dispatch path and the APRS-IS read loop) hammer
// Update/RecordRxEvent concurrently. Neither must block on the other, and
// every job is eventually observed by the history store.
func TestPersistentCache_ConcurrentRFAndISProducers(t *testing.T) {
	store := &fakePersistStore{}
	p := NewPersistentCache(quietLogger())
	if err := p.Reconfigure(store); err != nil {
		t.Fatalf("Reconfigure: %v", err)
	}
	defer p.Close()

	const perProducer = 200
	var wg sync.WaitGroup
	wg.Add(2)
	start := time.Now()
	go func() {
		defer wg.Done()
		for i := 0; i < perProducer; i++ {
			p.Update([]CacheEntry{{Key: "stn:RF", Callsign: "RF", HasPos: true, Timestamp: time.Now()}})
			p.RecordRxEvent(RxEvent{Timestamp: time.Now(), AttrKey: "stn:RF"})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < perProducer; i++ {
			p.Update([]CacheEntry{{Key: "stn:IS", Callsign: "IS", HasPos: true, Timestamp: time.Now()}})
			p.RecordRxEvent(RxEvent{Timestamp: time.Now(), AttrKey: "stn:IS"})
		}
	}()
	wg.Wait()
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("producers took %s, want well under 1s (no lock/DB contention)", elapsed)
	}

	testsync.WaitFor(t, func() bool {
		return store.totalEntries() == 2*perProducer && store.totalRxEvents() == 2*perProducer
	}, 2*time.Second, "writer to flush all RF+IS jobs")
}

// TestPersistentCache_QueueOverflowDropsAndCounts verifies the safety
// valve: when the writer falls far enough behind that the bounded queue
// fills up, further Update/RecordRxEvent calls drop (rather than block)
// and the drop is observable via WriteDropped.
func TestPersistentCache_QueueOverflowDropsAndCounts(t *testing.T) {
	store := &fakePersistStore{blockWrites: make(chan struct{})}
	p := NewPersistentCache(quietLogger())
	if err := p.Reconfigure(store); err != nil {
		t.Fatalf("Reconfigure: %v", err)
	}
	defer func() {
		close(store.blockWrites)
		p.Close()
	}()

	// Seed one entry and wait past the flush ticker so the writer picks
	// it up and blocks inside WriteEntries (on blockWrites). Once the
	// writer goroutine is stuck there, it stops draining the queue
	// entirely, so the flood below is guaranteed to overflow it
	// deterministically instead of racing the ticker.
	p.Update([]CacheEntry{{Key: "stn:SEED", Callsign: "SEED", HasPos: true, Timestamp: time.Now()}})
	time.Sleep(writeFlushInterval + 100*time.Millisecond)

	for i := 0; i < writeQueueCapacity+50; i++ {
		p.Update([]CacheEntry{{Key: "stn:FLOOD", Callsign: "FLOOD", HasPos: true, Timestamp: time.Now()}})
	}

	if got := p.WriteDropped(); got == 0 {
		t.Fatal("WriteDropped() = 0, want > 0 after overflowing the queue while the writer was stalled")
	}
}
