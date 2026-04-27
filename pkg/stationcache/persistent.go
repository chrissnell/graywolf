package stationcache

import (
	"log/slog"
	"sync"
	"time"
)

// PersistentCache wraps a MemCache with optional SQLite persistence.
// When persistence is disabled, it behaves identically to a bare
// MemCache. Persistence can be toggled at runtime via Reconfigure.
type PersistentCache struct {
	mu     sync.RWMutex
	mem    *MemCache
	hdb    HistoryStore // nil when disabled
	done   chan struct{} // signals prune goroutine; nil when disabled
	logger *slog.Logger
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

// Update applies entries to the in-memory cache and, if persistence
// is enabled, writes them to the history database.
func (p *PersistentCache) Update(entries []CacheEntry) {
	p.mem.Update(entries)

	p.mu.RLock()
	hdb := p.hdb
	p.mu.RUnlock()

	if hdb != nil {
		if err := hdb.WriteEntries(entries); err != nil {
			p.logger.Warn("history write failed", "err", err)
		}
	}
}

// QueryBBox delegates to the in-memory cache.
func (p *PersistentCache) QueryBBox(bbox BBox, maxAge time.Duration) []Station {
	return p.mem.QueryBBox(bbox, maxAge)
}

// Lookup delegates to the in-memory cache.
func (p *PersistentCache) Lookup(callsigns []string) map[string]LatLon {
	return p.mem.Lookup(callsigns)
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
	go p.pruneLoop(p.done, hdb)
	return nil
}

// Close shuts down persistence and the in-memory cache.
func (p *PersistentCache) Close() {
	p.mu.Lock()
	p.stopLocked()
	p.mu.Unlock()
	p.mem.Close()
}

// stopLocked closes the history database and stops the prune goroutine.
// Caller must hold p.mu.
func (p *PersistentCache) stopLocked() {
	if p.done != nil {
		close(p.done)
		p.done = nil
	}
	if p.hdb != nil {
		p.hdb.Close()
		p.hdb = nil
	}
}

func (p *PersistentCache) pruneLoop(done chan struct{}, hdb HistoryStore) {
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
