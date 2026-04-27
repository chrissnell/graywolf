package beacon

import (
	"container/heap"
	"time"
)

// beaconPlan is one entry in the scheduler's min-heap. It wraps a Config
// with the next wall-clock time the scheduler should act on that beacon
// plus the state SmartBeaconing needs to make turn-pegging decisions
// without spawning a per-beacon goroutine.
//
// All fields are owned by the scheduler goroutine; they are only read or
// written from the run loop or from helper methods called on it.
type beaconPlan struct {
	cfg      Config
	nextFire time.Time

	// lastSent is the wall-clock time the beacon last actually transmitted
	// (not the scheduled-and-skipped time). Zero value means "has never
	// fired yet", which the SmartBeacon logic treats as "fire on the next
	// wake".
	lastSent time.Time

	// lastHeading and hasHeading record the heading used for the most
	// recent SmartBeacon corner-peg comparison.
	lastHeading float64
	hasHeading  bool

	// index is maintained by container/heap and is unused by the scheduler
	// itself; it only exists to make Swap cheap.
	index int
}

// beaconHeap is a min-heap of *beaconPlan ordered by nextFire.
//
// The scheduler owns exactly one heap at a time; reloads build a fresh
// one and swap it in. That means beaconHeap itself does not need to be
// concurrency-safe — all access happens on the run-loop goroutine.
type beaconHeap []*beaconPlan

// Compile-time check.
var _ heap.Interface = (*beaconHeap)(nil)

func (h beaconHeap) Len() int { return len(h) }

func (h beaconHeap) Less(i, j int) bool {
	if h[i].nextFire.Equal(h[j].nextFire) {
		// Stable tie-break on ID so tests can assert deterministic order
		// when several beacons share the same initial fire time.
		return h[i].cfg.ID < h[j].cfg.ID
	}
	return h[i].nextFire.Before(h[j].nextFire)
}

func (h beaconHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *beaconHeap) Push(x any) {
	p := x.(*beaconPlan)
	p.index = len(*h)
	*h = append(*h, p)
}

func (h *beaconHeap) Pop() any {
	old := *h
	n := len(old)
	p := old[n-1]
	old[n-1] = nil
	p.index = -1
	*h = old[:n-1]
	return p
}

// Peek returns the earliest-scheduled plan without removing it. Returns
// nil if the heap is empty.
func (h beaconHeap) Peek() *beaconPlan {
	if len(h) == 0 {
		return nil
	}
	return h[0]
}
