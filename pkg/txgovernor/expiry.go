package txgovernor

import (
	"context"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
)

// Submit enqueues ordinary traffic with the existing unbounded queue lifetime.
func (g *Governor) Submit(ctx context.Context, channel uint32, frame *ax25.Frame, src SubmitSource) error {
	return g.submit(ctx, channel, frame, src, time.Time{}, nil)
}

// SubmitExpiring bounds a time-sensitive report's governor queue lifetime.
// Cancellation is checked before rate/carrier waits and again before dispatch.
// Backend buffers after acceptance are outside this queue's lifetime contract.
func (g *Governor) SubmitExpiring(ctx context.Context, channel uint32, frame *ax25.Frame, src SubmitSource, validUntil time.Time, canceled <-chan struct{}) error {
	return g.submit(ctx, channel, frame, src, validUntil, canceled)
}

func (q *queueItem) expired(now time.Time) bool {
	if !q.validUntil.IsZero() && !now.Before(q.validUntil) {
		return true
	}
	if q.canceled != nil {
		select {
		case <-q.canceled:
			return true
		default:
		}
	}
	return false
}
