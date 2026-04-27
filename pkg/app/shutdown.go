package app

import (
	"context"
	"errors"
	"fmt"
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
	var errs []error
	for i := len(a.started) - 1; i >= 0; i-- {
		c := a.started[i]
		a.logger.Info("stopping component", "name", c.name)
		if err := c.stop(shutdownCtx); err != nil {
			a.logger.Error("component shutdown error", "name", c.name, "err", err)
			errs = append(errs, fmt.Errorf("%s: %w", c.name, err))
		}
	}
	a.started = nil
	return errors.Join(errs...)
}
