package txgovernor

import (
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
)

type queueItem struct {
	channel  uint32
	frame    *ax25.Frame
	source   SubmitSource
	priority int
	seq      uint64 // monotonic tie-breaker for stable FIFO within priority
	enqueued time.Time
	// rateLimitCounted is set once this item has contributed to
	// stats.RateLimited, so subsequent ticks that re-observe the same
	// held frame do not inflate the counter.
	rateLimitCounted bool
}

// pqueue is a max-heap on priority, with lower seq breaking ties (FIFO).
type pqueue []*queueItem

func (p pqueue) Len() int { return len(p) }
func (p pqueue) Less(i, j int) bool {
	if p[i].priority != p[j].priority {
		return p[i].priority > p[j].priority
	}
	return p[i].seq < p[j].seq
}
func (p pqueue) Swap(i, j int) { p[i], p[j] = p[j], p[i] }
func (p *pqueue) Push(x any)   { *p = append(*p, x.(*queueItem)) }
func (p *pqueue) Pop() any {
	old := *p
	n := len(old)
	x := old[n-1]
	*p = old[:n-1]
	return x
}
