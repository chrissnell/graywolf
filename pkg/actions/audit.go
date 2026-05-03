package actions

import (
	"context"
	"sync"
	"time"
)

const (
	DefaultMaxInvocationRows = 1000
	DefaultMaxInvocationAge  = 30 * 24 * time.Hour
	DefaultPruneInterval     = 24 * time.Hour
)

// InvocationPruner is the subset of the configstore repo the pruner
// loop needs. Wrapping it keeps the loop testable without spinning up
// a database.
type InvocationPruner interface {
	PruneActionInvocations(ctx context.Context, maxRows int, maxAge time.Duration) (int, error)
}

type AuditPrunerConfig struct {
	MaxRows  int
	MaxAge   time.Duration
	Interval time.Duration
}

// StartAuditPruner runs prune at Interval (default 24h) until the
// returned stop func is called or ctx is cancelled.
func StartAuditPruner(ctx context.Context, p InvocationPruner, cfg AuditPrunerConfig) func() {
	if cfg.MaxRows <= 0 {
		cfg.MaxRows = DefaultMaxInvocationRows
	}
	if cfg.MaxAge <= 0 {
		cfg.MaxAge = DefaultMaxInvocationAge
	}
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultPruneInterval
	}
	stopCh := make(chan struct{})
	go func() {
		t := time.NewTicker(cfg.Interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			case <-t.C:
				_, _ = p.PruneActionInvocations(ctx, cfg.MaxRows, cfg.MaxAge)
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(stopCh) }) }
}
