package logbuffer

// maintenance increments the chain-shared insert counter and runs
// evict() every MaintenanceEvery inserts. Errors from evict are
// intentionally swallowed: a transient DB error must not take down
// the application.
//
// Counter increment + threshold check are guarded by h.shared.mu so
// concurrent Handle calls — across every Handler in a chain — don't
// double-trigger or skip eviction. The shared pointer is set up by
// New (handler.go) and aliased by every WithAttrs / WithGroup clone.
func (h *Handler) maintenance() {
	if h.cfg.MaintenanceEvery <= 0 {
		return
	}
	h.shared.mu.Lock()
	h.shared.counter++
	due := h.shared.counter >= h.cfg.MaintenanceEvery
	if due {
		h.shared.counter = 0
	}
	h.shared.mu.Unlock()
	if !due {
		return
	}
	_ = evict(h.db, h.cfg.RingSize)
}
