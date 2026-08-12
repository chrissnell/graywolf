package app

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Stop shuts down every component that Start successfully brought up,
// in reverse of the startup order. A component's stop error is logged
// and collected but does not abort the loop — the next component gets
// its turn regardless so that a crashed service cannot strand an
// OS-level resource (socket, goroutine, file descriptor) owned by a
// later component.
//
// Stop is idempotent: calling it twice drains the started slice and
// the second call is a no-op.
func (a *App) Stop(shutdownCtx context.Context) error {
	// Compute a per-component budget from shutdownCtx's deadline (captured
	// before any stops run, so it equals the full configured ShutdownTimeout).
	// Each component gets its own fresh context — one slow component (e.g.
	// http.Server.Shutdown waiting on an open browser connection) can no longer
	// exhaust the shared deadline and cascade "timed out" to all subsequent ones.
	perComponent := 10 * time.Second
	if d, ok := shutdownCtx.Deadline(); ok {
		if budget := time.Until(d); budget > 0 {
			perComponent = budget
		}
	}
	var errs []error
	for i := len(a.started) - 1; i >= 0; i-- {
		c := a.started[i]
		a.logger.Info("stopping component", "name", c.name)
		cCtx, cCancel := context.WithTimeout(context.Background(), perComponent)
		err := c.stop(cCtx)
		cCancel()
		if err != nil {
			a.logger.Error("component shutdown error", "name", c.name, "err", err)
			errs = append(errs, fmt.Errorf("%s: %w", c.name, err))
		}
	}
	a.started = nil
	return errors.Join(errs...)
}
