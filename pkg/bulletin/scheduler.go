package bulletin

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// planKey uniquely identifies one scheduled bulletin plan.
type planKey struct {
	GroupID uint32
	Slot    int
}

// Scheduler owns the bulletin run loop and dispatches periodic transmissions
// using a min-heap keyed by next-fire time. The interval decays after each
// transmission: currentInterval = min(currentInterval * decayFactor, stableRate).
// On reload, starting intervals are re-derived from sendCount so no separate
// state column is needed in the DB.
type Scheduler struct {
	txSink    TxSink
	isSink    ISSink // optional; nil skips the IS leg
	logger    *slog.Logger
	ourCallFn func() string // returns the station callsign at transmit time
	onSent    func(ctx context.Context, groupID uint32, slot int)
	clock     Clock

	mu       sync.Mutex
	groups   []GroupConfig
	reloadCh chan struct{}

	// nextFireMu protects nextFireMap; written by the Run goroutine, read by API handlers.
	nextFireMu  sync.RWMutex
	nextFireMap map[planKey]time.Time

	// fetchGroup is a fallback for SendNow when the target is not in the active scheduler state.
	fetchGroup func(ctx context.Context, groupID uint32) (*GroupConfig, error)
}

// Options configures a Scheduler.
type Options struct {
	TxSink    TxSink
	ISSink    ISSink      // optional
	Logger    *slog.Logger
	OurCallFn func() string
	Clock     Clock // defaults to wall clock
	// OnSent is called after each successful transmission to record the send.
	OnSent func(ctx context.Context, groupID uint32, slot int)
	// FetchGroup is called by SendNow when the target group is not in the active scheduler
	// state (e.g. group or item is inactive). Returns a GroupConfig with all items that have
	// text, regardless of active flag.
	FetchGroup func(ctx context.Context, groupID uint32) (*GroupConfig, error)
}

// New constructs a Scheduler. TxSink and OurCallFn are required.
func New(opts Options) (*Scheduler, error) {
	if opts.TxSink == nil {
		return nil, errors.New("bulletin: nil TxSink")
	}
	if opts.OurCallFn == nil {
		return nil, errors.New("bulletin: nil OurCallFn")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	clk := opts.Clock
	if clk == nil {
		clk = realClock{}
	}
	return &Scheduler{
		txSink:      opts.TxSink,
		isSink:      opts.ISSink,
		logger:      logger.With("component", "bulletin"),
		ourCallFn:   opts.OurCallFn,
		onSent:      opts.OnSent,
		clock:       clk,
		fetchGroup:  opts.FetchGroup,
		reloadCh:    make(chan struct{}, 1),
		nextFireMap: make(map[planKey]time.Time),
	}, nil
}

// SetISSink swaps the APRS-IS sink. Safe to call before or during Run.
func (s *Scheduler) SetISSink(sink ISSink) {
	s.mu.Lock()
	s.isSink = sink
	s.mu.Unlock()
}

// Reload atomically replaces the group list and signals Run to rebuild.
func (s *Scheduler) Reload(groups []GroupConfig) {
	s.mu.Lock()
	s.groups = append([]GroupConfig(nil), groups...)
	s.mu.Unlock()
	select {
	case s.reloadCh <- struct{}{}:
	default:
	}
}

// SendNow immediately transmits a single bulletin item, independent of its
// scheduled interval. It searches the current group list for (groupID, slot).
func (s *Scheduler) SendNow(ctx context.Context, groupID uint32, slot int) error {
	s.mu.Lock()
	var found *bulletinPlan
	for i := range s.groups {
		g := &s.groups[i]
		if g.ID != groupID {
			continue
		}
		for j := range g.Items {
			item := &g.Items[j]
			if item.Slot == slot {
				found = planFromItem(g, item, s.clock.Now())
				break
			}
		}
	}
	s.mu.Unlock()
	// Fallback: item not in the active scheduler state (group/item may be inactive).
	// Load directly so the operator can send any slot that has text.
	if found == nil && s.fetchGroup != nil {
		if gc, err := s.fetchGroup(ctx, groupID); err == nil && gc != nil {
			for j := range gc.Items {
				if gc.Items[j].Slot == slot {
					found = planFromItem(gc, &gc.Items[j], s.clock.Now())
					break
				}
			}
		}
	}
	if found == nil {
		return fmt.Errorf("bulletin: group=%d slot=%d not found", groupID, slot)
	}
	return s.fire(ctx, found)
}

// NextFireAt returns the scheduler's current next-fire time for one slot.
// Returns zero time and false when the plan is not currently scheduled.
func (s *Scheduler) NextFireAt(groupID uint32, slot int) (time.Time, bool) {
	s.nextFireMu.RLock()
	defer s.nextFireMu.RUnlock()
	t, ok := s.nextFireMap[planKey{GroupID: groupID, Slot: slot}]
	return t, ok
}

// setNextFireMap atomically replaces the published next-fire table from a heap snapshot.
func (s *Scheduler) setNextFireMap(h *bulletinHeap) {
	m := make(map[planKey]time.Time, h.Len())
	for _, p := range *h {
		m[planKey{GroupID: p.groupID, Slot: p.slot}] = p.nextFire
	}
	s.nextFireMu.Lock()
	s.nextFireMap = m
	s.nextFireMu.Unlock()
}

// Run drives the scheduler until ctx is cancelled. Blocking; call from a
// dedicated goroutine.
func (s *Scheduler) Run(ctx context.Context) error {
	h := s.buildHeap(s.clock.Now(), nil)
	s.setNextFireMap(h)
	for {
		// Drain any pending reload before deciding what to do.
		select {
		case <-s.reloadCh:
			h = s.buildHeap(s.clock.Now(), heapIndex(h))
			s.setNextFireMap(h)
		default:
		}

		if h.Len() == 0 {
			select {
			case <-s.reloadCh:
				h = s.buildHeap(s.clock.Now(), heapIndex(h))
				s.setNextFireMap(h)
			case <-ctx.Done():
				return nil
			}
			continue
		}

		now := s.clock.Now()
		next := h.peek()
		wait := next.nextFire.Sub(now)
		if wait <= 0 {
			heap.Pop(h)
			if err := s.fire(ctx, next); err != nil {
				s.logger.Warn("bulletin fire", "group", next.groupID, "slot", next.slot, "err", err)
			}
			next.currentInterval = min(
				time.Duration(float64(next.currentInterval)*next.decayFactor),
				next.stableRate,
			)
			next.nextFire = s.clock.Now().Add(next.currentInterval)
			heap.Push(h, next)
			s.setNextFireMap(h)
			continue
		}

		select {
		case <-s.clock.After(wait):
		case <-s.reloadCh:
			h = s.buildHeap(s.clock.Now(), heapIndex(h))
			s.setNextFireMap(h)
		case <-ctx.Done():
			return nil
		}
	}
}

// buildHeap constructs a fresh heap from the current group list. Only active
// groups with at least one active item are included.
// carried maps existing plans so their nextFire/currentInterval survive a reload.
// Items absent from carried are new and fire immediately (nextFire = now).
func (s *Scheduler) buildHeap(now time.Time, carried map[planKey]*bulletinPlan) *bulletinHeap {
	s.mu.Lock()
	groups := append([]GroupConfig(nil), s.groups...)
	s.mu.Unlock()

	h := make(bulletinHeap, 0)
	for i := range groups {
		g := &groups[i]
		for j := range g.Items {
			item := &g.Items[j]
			p := planFromItem(g, item, now)
			key := planKey{GroupID: g.ID, Slot: item.Slot}
			if old, ok := carried[key]; ok {
				// Preserve timing so saving settings doesn't restart the clock.
				p.nextFire = old.nextFire
				p.currentInterval = old.currentInterval
			}
			// Plans absent from carried are new; nextFire=now fires them immediately.
			h = append(h, p)
		}
	}
	heap.Init(&h)
	return &h
}

// heapIndex builds a planKey→plan map from the current heap for carry-over.
func heapIndex(h *bulletinHeap) map[planKey]*bulletinPlan {
	if h == nil || h.Len() == 0 {
		return nil
	}
	m := make(map[planKey]*bulletinPlan, h.Len())
	for _, p := range *h {
		m[planKey{GroupID: p.groupID, Slot: p.slot}] = p
	}
	return m
}

// planFromItem creates a bulletinPlan, computing the starting interval from
// the item's sendCount so the heap is consistent with the decaying formula.
func planFromItem(g *GroupConfig, item *ItemConfig, now time.Time) *bulletinPlan {
	initialRate := g.InitialRate
	if initialRate < MinInitialRate {
		initialRate = MinInitialRate
	}
	stableRate := g.StableRate
	if stableRate < initialRate {
		stableRate = initialRate
	}
	decayFactor := g.DecayFactor
	if decayFactor < 1.0 {
		decayFactor = 1.0
	}

	// Re-derive interval from sendCount:  initialRate * decayFactor^sendCount, capped at stableRate.
	current := time.Duration(float64(initialRate) * math.Pow(decayFactor, float64(item.SendCount)))
	if current > stableRate {
		current = stableRate
	}

	return &bulletinPlan{
		groupID:         g.ID,
		groupName:       g.Name,
		slot:            item.Slot,
		text:            item.Text,
		channel:         g.Channel,
		sendPath:        g.SendPath,
		digiPath:        g.DigiPath,
		initialRate:     initialRate,
		decayFactor:     decayFactor,
		stableRate:      stableRate,
		currentInterval: current,
		nextFire:        now, // fire immediately; buildHeap carries over existing nextFire
	}
}

// fire builds the APRS frame and transmits via RF and/or APRS-IS.
func (s *Scheduler) fire(ctx context.Context, p *bulletinPlan) error {
	ourCall := s.ourCallFn()
	if ourCall == "" {
		return errors.New("bulletin: station callsign not set")
	}

	src, err := ax25.ParseAddress(ourCall)
	if err != nil {
		return fmt.Errorf("bulletin: parse source callsign %q: %w", ourCall, err)
	}
	dest, err := ax25.ParseAddress(aprsDestination)
	if err != nil {
		return fmt.Errorf("bulletin: parse dest: %w", err)
	}

	var digiPath []ax25.Address
	for _, hop := range strings.Split(p.digiPath, ",") {
		hop = strings.TrimSpace(hop)
		if hop == "" {
			continue
		}
		a, perr := ax25.ParseAddress(hop)
		if perr != nil {
			s.logger.Warn("bulletin: bad digi hop, skipping", "hop", hop, "err", perr)
			continue
		}
		digiPath = append(digiPath, a)
	}

	info := buildInfoField(p.slot, p.groupName, p.text)
	frame, err := ax25.NewUIFrame(src, dest, digiPath, []byte(info))
	if err != nil {
		return fmt.Errorf("bulletin: encode frame: %w", err)
	}

	txSrc := txgovernor.SubmitSource{
		Kind:     "bulletin",
		Detail:   fmt.Sprintf("group=%d slot=%d", p.groupID, p.slot),
		Priority: ax25.PriorityBeacon,
	}

	sendRF := p.sendPath != SendPathISOnly
	sendIS := p.sendPath == SendPathBoth || p.sendPath == SendPathISOnly

	if sendRF && p.channel > 0 {
		if err := s.txSink.Submit(ctx, p.channel, frame, txSrc); err != nil {
			s.logger.Warn("bulletin RF submit failed", "group", p.groupID, "slot", p.slot, "err", err)
			if !sendIS {
				return fmt.Errorf("bulletin RF submit: %w", err)
			}
		} else {
			s.logger.Info("bulletin sent RF", "group", p.groupID, "slot", p.slot, "info", info)
		}
	}

	s.mu.Lock()
	isSink := s.isSink
	s.mu.Unlock()

	if sendIS {
		if isSink == nil {
			if !sendRF {
				return errors.New("bulletin: APRS-IS sink not configured for is_only send_path")
			}
		} else {
			line := aprs.FormatTNC2(src.String(), dest.String(), []string{"TCPIP*"}, []byte(info))
			if isErr := isSink.SendLine(line); isErr != nil {
				s.logger.Warn("bulletin APRS-IS send failed", "group", p.groupID, "slot", p.slot, "err", isErr)
				if !sendRF {
					return fmt.Errorf("bulletin APRS-IS send: %w", isErr)
				}
			} else {
				s.logger.Info("bulletin sent APRS-IS", "group", p.groupID, "slot", p.slot, "line", line)
			}
		}
	}
	if s.onSent != nil {
		s.onSent(ctx, p.groupID, p.slot)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Min-heap implementation
// ---------------------------------------------------------------------------

type bulletinPlan struct {
	groupID         uint32
	groupName       string
	slot            int
	text            string
	channel         uint32
	sendPath        string
	digiPath        string
	initialRate     time.Duration
	decayFactor     float64
	stableRate      time.Duration
	currentInterval time.Duration
	nextFire        time.Time
}

type bulletinHeap []*bulletinPlan

func (h bulletinHeap) Len() int           { return len(h) }
func (h bulletinHeap) Less(i, j int) bool { return h[i].nextFire.Before(h[j].nextFire) }
func (h bulletinHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *bulletinHeap) Push(x any) { *h = append(*h, x.(*bulletinPlan)) }
func (h *bulletinHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	return x
}

func (h *bulletinHeap) peek() *bulletinPlan { return (*h)[0] }

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
