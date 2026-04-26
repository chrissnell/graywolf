package logbuffer

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
)

// Config tunes the Handler.
//
// RingSize is the maximum number of rows retained. <=0 disables
// persistence entirely (Handler still forwards to the inner handler).
//
// MaintenanceEvery controls how often eviction runs (every Nth Handle
// call). 0 means "never trigger from Handle" (used by tests that drive
// eviction manually). Wired to 200 in production by cmd/graywolf/main.go.
type Config struct {
	RingSize         int
	MaintenanceEvery int
}

// Handler is a slog.Handler that forwards every record to an inner
// handler (typically the console TextHandler) and tees it to a logbuffer
// DB. Capture is always at DEBUG regardless of the inner handler's
// threshold so a future flare submission has full detail.
type Handler struct {
	inner slog.Handler
	db    *DB
	cfg   Config

	// goAttrs / goGroups carry the handler chain produced by With() and
	// WithGroup(). They are accumulated here so we can serialize them
	// into the DB row alongside the per-record attrs without relying on
	// the inner handler's internal state.
	goAttrs  []slog.Attr
	goGroups []string

	mu      sync.Mutex
	counter int // used by maintenance.go to throttle eviction
}

// New returns a Handler that wraps inner and tees to db.
func New(inner slog.Handler, db *DB, cfg Config) *Handler {
	return &Handler{inner: inner, db: db, cfg: cfg}
}

// Enabled returns true for every level >= Debug. The inner handler is
// asked separately inside Handle so the console keeps its configured
// threshold.
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelDebug
}

// Handle forwards the record to the inner handler (subject to the
// inner handler's own Enabled check) and persists it to the DB at
// DEBUG-and-above.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	// Forward to inner first so console output is never delayed by DB
	// work. Errors from the inner handler propagate; DB errors do not
	// (Task 7).
	if h.inner.Enabled(ctx, r.Level) {
		if err := h.inner.Handle(ctx, r.Clone()); err != nil {
			return err
		}
	}
	if h.db != nil && h.cfg.RingSize > 0 {
		h.persist(r)
	}
	return nil
}

// WithAttrs returns a new Handler whose subsequent records carry attrs.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.inner = h.inner.WithAttrs(attrs)
	clone.goAttrs = append(append([]slog.Attr(nil), h.goAttrs...), attrs...)
	return &clone
}

// WithGroup returns a new Handler scoped under the named group.
func (h *Handler) WithGroup(name string) slog.Handler {
	clone := *h
	clone.inner = h.inner.WithGroup(name)
	clone.goGroups = append(append([]string(nil), h.goGroups...), name)
	return &clone
}

// persist writes one row. Errors are intentionally swallowed in this
// task so a transient DB problem can't take down the program; Task 7
// adds the "log to stderr once" surfacing.
func (h *Handler) persist(r slog.Record) {
	attrs := h.collectAttrs(r)
	attrsJSON, _ := json.Marshal(attrs)
	component := "" // populated in Task 6
	_ = h.db.gorm.Exec(
		"INSERT INTO logs (ts_ns, level, component, msg, attrs_json) VALUES (?,?,?,?,?)",
		r.Time.UnixNano(),
		r.Level.String(),
		component,
		r.Message,
		string(attrsJSON),
	).Error

	h.afterInsert()
}

// collectAttrs merges the handler-chain attrs (from With()) with the
// per-record attrs into a single map keyed by attribute key. Group
// nesting is encoded as a dotted prefix on the key, matching the slog
// JSON handler's convention.
func (h *Handler) collectAttrs(r slog.Record) map[string]any {
	out := make(map[string]any, len(h.goAttrs)+r.NumAttrs())
	prefix := ""
	for _, g := range h.goGroups {
		if g == "" {
			continue
		}
		if prefix == "" {
			prefix = g
		} else {
			prefix = prefix + "." + g
		}
	}
	addAttr := func(a slog.Attr) {
		key := a.Key
		if prefix != "" {
			key = prefix + "." + key
		}
		out[key] = a.Value.Any()
	}
	for _, a := range h.goAttrs {
		addAttr(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		addAttr(a)
		return true
	})
	return out
}

// afterInsert is the maintenance hook; bodied in Task 5.
func (h *Handler) afterInsert() {}
