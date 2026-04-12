// Package beacon implements the graywolf beacon scheduler: position,
// object, tracker, custom, and igate beacons driven by the configstore
// `beacons` table, with optional SmartBeaconing for tracker beacons and
// safe `comment_cmd` execution for dynamic comments. All outgoing frames
// are submitted through a txgovernor.TxSink at PriorityBeacon.
//
// Runtime model: a single scheduler goroutine maintains a min-heap of
// *beaconPlan keyed by nextFire. Each tick pops the earliest plan,
// dispatches it onto a bounded worker pool, then pushes the rescheduled
// plan back onto the heap. Reloads are serviced on the same goroutine,
// so there is no interleaving between the old and new schedules.
package beacon

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/gps"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// DefaultMaxConcurrentFires is the default size of the fire worker pool
// when Options.MaxConcurrentFires is zero. Four workers is enough for
// realistic home-station configurations; operators with dozens of
// beacons can raise the limit explicitly.
const DefaultMaxConcurrentFires = 4

// smartPollInterval is the maximum gap between wakeups for a
// SmartBeacon-enabled tracker. GPS-driven turn detection needs to be
// responsive, so we peek at the cache this often even when the
// fixed-rate interval is longer.
const smartPollInterval = 1 * time.Second

// Scheduler owns the run-loop goroutine and the bounded worker pool
// that dispatches beacon fires. Configure via New, drive with Run;
// SetBeacons / Reload / SendNow are safe to call from any goroutine.
type Scheduler struct {
	sink     txgovernor.TxSink
	isSink   ISSink // optional APRS-IS destination; guarded by mu
	cache    gps.PositionCache
	logger   *slog.Logger
	observer Observer
	clock    Clock
	version  string
	maxFires int
	workers  chan struct{} // counting semaphore sized to maxFires

	mu       sync.Mutex
	beacons  []Config
	reloadCh chan struct{}
}

// Options configures a Scheduler.
type Options struct {
	Sink     txgovernor.TxSink
	Cache    gps.PositionCache // may be nil for fixed/igate-only deployments
	Logger   *slog.Logger
	Observer Observer
	Clock    Clock  // defaults to wall clock
	Version  string // running graywolf version, used to expand {{version}} in comments
	ISSink   ISSink // optional APRS-IS line sender for beacons with SendToAPRSIS
	// MaxConcurrentFires bounds how many beacon fires can be in flight
	// at once. Zero selects DefaultMaxConcurrentFires. The scheduler
	// never blocks on submit — if all workers are busy when a beacon is
	// due, the fire is dropped and a skipped_busy event is recorded.
	MaxConcurrentFires int
}

// New constructs a Scheduler.
func New(opts Options) (*Scheduler, error) {
	if opts.Sink == nil {
		return nil, fmt.Errorf("beacon: nil sink")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	clock := opts.Clock
	if clock == nil {
		clock = realClock{}
	}
	maxFires := opts.MaxConcurrentFires
	if maxFires <= 0 {
		maxFires = DefaultMaxConcurrentFires
	}
	return &Scheduler{
		sink:     opts.Sink,
		isSink:   opts.ISSink,
		cache:    opts.Cache,
		logger:   logger.With("component", "beacon"),
		observer: opts.Observer,
		clock:    clock,
		version:  opts.Version,
		maxFires: maxFires,
		workers:  make(chan struct{}, maxFires),
		reloadCh: make(chan struct{}, 1),
	}, nil
}

// SetISSink sets the optional APRS-IS sink. Safe to call before Run.
func (s *Scheduler) SetISSink(sink ISSink) {
	s.mu.Lock()
	s.isSink = sink
	s.mu.Unlock()
}

// SetBeacons replaces the beacon list. If Run is active, call Reload
// instead to also tell the scheduler to pick up the new config.
func (s *Scheduler) SetBeacons(b []Config) {
	s.mu.Lock()
	s.beacons = append([]Config(nil), b...)
	s.mu.Unlock()
}

// Reload atomically swaps in a new beacon list and signals Run to
// rebuild its heap from the new config. Safe to call from any goroutine;
// non-blocking — rapid successive calls coalesce into one rebuild.
//
// The rebuild happens on the scheduler's single run-loop goroutine, so
// there is no interleaving between the old and new schedules: a beacon
// either fires from the pre-reload heap or from the post-reload heap,
// never both.
func (s *Scheduler) Reload(b []Config) {
	s.SetBeacons(b)
	select {
	case s.reloadCh <- struct{}{}:
	default:
	}
}

// SendNow finds the beacon with the given id in the current beacon list
// and transmits it once immediately, independently of its scheduled
// interval. Returns an error if the id is not present. The Enabled flag
// is intentionally ignored — operators may want to test a beacon that
// is otherwise disabled.
func (s *Scheduler) SendNow(ctx context.Context, id uint32) error {
	s.mu.Lock()
	var found *Config
	for i := range s.beacons {
		if s.beacons[i].ID == id {
			b := s.beacons[i]
			found = &b
			break
		}
	}
	s.mu.Unlock()
	if found == nil {
		return fmt.Errorf("beacon: id %d not found", id)
	}
	s.sendBeacon(ctx, *found)
	return nil
}

// Run drives the scheduler's single heap-based run loop until ctx is
// cancelled. It returns nil on clean shutdown. In-flight worker
// goroutines detach from Run and complete (or cancel via ctx) on their
// own; Run returning does not wait for them.
func (s *Scheduler) Run(ctx context.Context) error {
	h := s.buildHeap(s.clock.Now())
	for {
		// Drain any pending reload first so we always act on the freshest
		// config before deciding whether to sleep or fire.
		select {
		case <-s.reloadCh:
			h = s.buildHeap(s.clock.Now())
		default:
		}

		if h.Len() == 0 {
			// Nothing scheduled — wait for a reload or cancellation.
			select {
			case <-s.reloadCh:
				h = s.buildHeap(s.clock.Now())
			case <-ctx.Done():
				return nil
			}
			continue
		}

		now := s.clock.Now()
		next := h.Peek()
		wait := next.nextFire.Sub(now)
		if wait <= 0 {
			heap.Pop(h)
			if s.shouldFire(next, now) {
				s.fireAsync(ctx, next)
				next.lastSent = now
				if s.isSmart(next.cfg) {
					fix, ok := s.cache.Get()
					if ok && fix.HasCourse {
						next.lastHeading = fix.Heading
						next.hasHeading = true
					}
				}
			}
			next.nextFire = s.nextWake(next, now)
			heap.Push(h, next)
			continue
		}

		select {
		case <-s.clock.After(wait):
			// Loop back and re-peek; the earliest plan may have changed
			// if the clock fake advanced several plans past due at once.
		case <-s.reloadCh:
			h = s.buildHeap(s.clock.Now())
		case <-ctx.Done():
			return nil
		}
	}
}

// buildHeap snapshots the current beacon list and returns a fresh heap
// with one *beaconPlan per enabled beacon. Called from Run only.
func (s *Scheduler) buildHeap(now time.Time) *beaconHeap {
	s.mu.Lock()
	configs := append([]Config(nil), s.beacons...)
	s.mu.Unlock()

	h := make(beaconHeap, 0, len(configs))
	for _, cfg := range configs {
		if !cfg.Enabled {
			continue
		}
		h = append(h, &beaconPlan{
			cfg:      cfg,
			nextFire: s.initialFire(cfg, now),
		})
	}
	heap.Init(&h)
	s.logger.Info("beacon scheduler heap built", "count", len(h))
	return &h
}

// initialFire returns the wall-clock time a newly-scheduled plan should
// first consider firing. Slot alignment wins over Delay when set.
func (s *Scheduler) initialFire(cfg Config, now time.Time) time.Time {
	delay := cfg.Delay
	if cfg.Slot >= 0 && cfg.Slot < 3600 {
		delay = timeToNextSlot(now, cfg.Slot)
	}
	if delay < 0 {
		delay = 0
	}
	return now.Add(delay)
}

// isSmart reports whether a Config should be driven by SmartBeaconing.
// A nil cache disables smart behavior even when configured, matching the
// pre-refactor semantics.
func (s *Scheduler) isSmart(c Config) bool {
	return c.Type == TypeTracker && c.SmartBeacon != nil && c.SmartBeacon.Enabled && s.cache != nil
}

// shouldFire decides, at wake time, whether to actually transmit a
// plan's beacon. Non-smart plans always fire at their scheduled time.
// Smart plans fire on three conditions: their first scheduled fire,
// expiry of the speed-dependent fixed-rate interval, or a
// heading-delta exceeding the corner-peg threshold after TurnTime has
// elapsed since the last transmit.
func (s *Scheduler) shouldFire(p *beaconPlan, now time.Time) bool {
	if !s.isSmart(p.cfg) {
		return true
	}
	if p.lastSent.IsZero() {
		return true
	}
	fix, _ := s.cache.Get()
	cfg := p.cfg.SmartBeacon
	elapsed := now.Sub(p.lastSent)
	// Corner pegging: only consider once TurnTime has elapsed and we
	// have a previous heading to diff against.
	if p.hasHeading && fix.HasCourse && elapsed >= cfg.TurnTime {
		delta := HeadingDelta(p.lastHeading, fix.Heading)
		if delta >= cfg.TurnThreshold(fix.Speed) {
			return true
		}
	}
	// Fixed-rate trigger.
	return elapsed >= cfg.Interval(fix.Speed)
}

// nextWake computes the time at which the scheduler should next pop p
// from the heap. Non-smart plans wake once per Every interval; smart
// plans wake at min(now+smartPollInterval, now+Interval(speed)) so they
// can re-evaluate turn detection frequently without paying for a
// per-beacon goroutine.
func (s *Scheduler) nextWake(p *beaconPlan, now time.Time) time.Time {
	if !s.isSmart(p.cfg) {
		every := p.cfg.Every
		if every <= 0 {
			every = 10 * time.Minute
		}
		return now.Add(every)
	}
	fix, _ := s.cache.Get()
	interval := p.cfg.SmartBeacon.Interval(fix.Speed)
	if s.observer != nil {
		s.observer.OnSmartBeaconRate(p.cfg.Channel, interval)
	}
	poll := smartPollInterval
	if interval < poll {
		poll = interval
	}
	return now.Add(poll)
}

// fireAsync dispatches sendBeacon onto a worker pool goroutine without
// blocking the run loop. If the pool is saturated the fire is dropped
// and a skipped_busy event is emitted; the plan's next scheduled wake
// is unaffected, so the next tick will come around normally.
func (s *Scheduler) fireAsync(ctx context.Context, p *beaconPlan) {
	cfg := p.cfg
	name := beaconName(cfg)
	select {
	case s.workers <- struct{}{}:
	default:
		s.logger.Warn("beacon fire skipped", "name", name, "reason", "busy")
		if so, ok := s.observer.(SkipObserver); ok && so != nil {
			so.OnBeaconSkipped(name, "busy")
		}
		return
	}
	go func() {
		defer func() { <-s.workers }()
		s.sendBeacon(ctx, cfg)
	}()
}

// sendBeacon builds and submits one beacon frame.
func (s *Scheduler) sendBeacon(ctx context.Context, b Config) {
	name := beaconName(b)
	info, err := s.buildInfo(ctx, b)
	if err != nil {
		// Build errors (comment_cmd missing required GPS, bad PHG, etc.)
		// are surfaced to the operator as warnings but are not
		// "encode" errors in the AX.25 sense, so they do not feed the
		// encode counter. The root cause is usually configuration.
		s.logger.Warn("beacon build", "id", b.ID, "type", b.Type, "err", err)
		return
	}
	frame, err := ax25.NewUIFrame(b.Source, b.Dest, b.Path, []byte(info))
	if err != nil {
		// AX.25 encode failure (almost always a malformed callsign).
		// Warn-level because the operator needs to fix the config;
		// also counted so the dashboard can show "beacon X has been
		// failing to encode for the last hour".
		s.logger.Warn("beacon encode", "id", b.ID, "name", name, "err", err)
		if eo, ok := s.observer.(ErrorObserver); ok && eo != nil {
			eo.OnEncodeError(name)
		}
		return
	}
	src := txgovernor.SubmitSource{
		Kind:     "beacon",
		Detail:   fmt.Sprintf("%s/%d", b.Type, b.ID),
		Priority: ax25.PriorityBeacon,
	}
	if err := s.sink.Submit(ctx, b.Channel, frame, src); err != nil {
		reason := classifySubmitError(err)
		s.logger.Warn("beacon submit", "id", b.ID, "name", name, "reason", reason, "err", err)
		if eo, ok := s.observer.(ErrorObserver); ok && eo != nil {
			eo.OnSubmitError(name, reason)
		}
		return
	}
	s.logger.Info("beacon sent", "id", b.ID, "type", b.Type, "channel", b.Channel, "info", info)
	if s.observer != nil {
		s.observer.OnBeaconSent(b.Type)
	}

	// Optionally duplicate the beacon to APRS-IS.
	if b.SendToAPRSIS && s.isSink != nil {
		line := formatTNC2(b.Source, b.Dest, b.Path, info)
		if err := s.isSink.SendLine(line); err != nil {
			s.logger.Warn("beacon aprs-is send", "id", b.ID, "name", name, "err", err)
		} else {
			s.logger.Info("beacon sent to aprs-is", "id", b.ID, "line", line)
		}
	}
}

// formatTNC2 renders a beacon as a TNC-2 monitor line for APRS-IS.
// The APRS-IS server adds the q-construct; we send the bare packet.
func formatTNC2(src, dest ax25.Address, path []ax25.Address, info string) string {
	var b strings.Builder
	b.WriteString(src.Call)
	if src.SSID != 0 {
		fmt.Fprintf(&b, "-%d", src.SSID)
	}
	b.WriteByte('>')
	b.WriteString(dest.Call)
	if dest.SSID != 0 {
		fmt.Fprintf(&b, "-%d", dest.SSID)
	}
	for _, p := range path {
		b.WriteByte(',')
		b.WriteString(p.Call)
		if p.SSID != 0 {
			fmt.Fprintf(&b, "-%d", p.SSID)
		}
		if p.Repeated {
			b.WriteByte('*')
		}
	}
	b.WriteByte(':')
	b.WriteString(info)
	return b.String()
}

// beaconName returns a stable, human-readable label for a beacon,
// used as the "beacon_name" metric label. Prefer ObjectName for
// object beacons (so two distinct objects on the same schedule are
// distinguishable); otherwise use "type/id" which is unique across
// the schedule by construction.
func beaconName(b Config) string {
	if b.Type == TypeObject && b.ObjectName != "" {
		return b.ObjectName
	}
	return fmt.Sprintf("%s/%d", b.Type, b.ID)
}

// classifySubmitError maps a Submit error into one of the beacon_submit_errors
// counter buckets. Centralized here so ErrorObserver implementations don't
// need to know the txgovernor sentinel set; extend when governor grows a new
// sentinel so the counter classification stays closed.
//
//	context.DeadlineExceeded | Canceled => "timeout"
//	txgovernor.ErrQueueFull             => "queue_full"
//	otherwise                           => "other"
func classifySubmitError(err error) string {
	switch {
	case err == nil:
		return "other"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "timeout"
	case errors.Is(err, txgovernor.ErrQueueFull):
		return "queue_full"
	}
	return "other"
}
