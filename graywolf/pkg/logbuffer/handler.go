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

	// shared throttle state lives behind a pointer so every chained
	// child Handler (produced by WithAttrs / WithGroup) increments the
	// same counter. Without this, per-subsystem loggers like
	// slog.With("component","ptt") each get their own counter and
	// MaintenanceEvery=200 never fires on cold chains. Putting the
	// mutex here also avoids the `go vet` "assignment copies lock value"
	// warning that a `clone := *h` would otherwise trip.
	shared *handlerShared
}

// handlerShared is the throttle state common to every Handler in a
// chain. Allocated once by New and aliased by every clone produced
// by WithAttrs / WithGroup.
type handlerShared struct {
	mu      sync.Mutex
	counter int
}

// New returns a Handler that wraps inner and tees to db.
func New(inner slog.Handler, db *DB, cfg Config) *Handler {
	return &Handler{
		inner:  inner,
		db:     db,
		cfg:    cfg,
		shared: &handlerShared{},
	}
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
// nesting (both from the WithGroup chain and from per-record
// slog.Group attrs) is flattened into dotted-prefix keys -- chosen
// over slog.NewJSONHandler's nested-object convention because a flat
// map serializes cleanly into a single TEXT column and stays
// grep-friendly when an operator dumps the ring with `sqlite3`.
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
	for _, a := range h.goAttrs {
		flattenAttr(out, prefix, a)
	}
	r.Attrs(func(a slog.Attr) bool {
		flattenAttr(out, prefix, a)
		return true
	})
	return out
}

// flattenAttr writes one attribute into out, recursing into
// slog.Group values so the flat-map invariant holds. Resolve()
// honours LogValuer attributes the same way slog's built-in handlers
// do. Empty group keys (slog spec: a group with key "" inlines its
// contents) are handled by treating them as no-prefix recursion.
func flattenAttr(out map[string]any, prefix string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		groupAttrs := a.Value.Group()
		childPrefix := prefix
		if a.Key != "" {
			if childPrefix == "" {
				childPrefix = a.Key
			} else {
				childPrefix = childPrefix + "." + a.Key
			}
		}
		for _, ga := range groupAttrs {
			flattenAttr(out, childPrefix, ga)
		}
		return
	}
	key := a.Key
	if prefix != "" {
		key = prefix + "." + key
	}
	out[key] = a.Value.Any()
}

// afterInsert is the maintenance hook; bodied in Task 5.
func (h *Handler) afterInsert() {}
