package ax25conn

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// SessionConfig captures the per-session knobs. Required fields:
// Local, Peer, Channel, TxSink. Defaults are filled in for missing
// timer/window/paclen.
type SessionConfig struct {
	Local, Peer ax25.Address
	Path        []ax25.Address
	Channel     uint32
	Mod128      bool
	T1, T2, T3  time.Duration
	Heartbeat   time.Duration
	N2          int
	Paclen      int
	Window      int
	Backoff     Backoff

	TxSink   txgovernor.TxSink
	Clock    Clock
	Logger   *slog.Logger
	Observer func(OutEvent) // non-blocking; manager fans to bridge
}

func (c *SessionConfig) applyDefaults() {
	if c.T1 == 0 {
		c.T1 = DefaultT1
	}
	if c.T2 == 0 {
		c.T2 = DefaultT2
	}
	if c.T3 == 0 {
		c.T3 = DefaultT3
	}
	if c.Heartbeat == 0 {
		c.Heartbeat = DefaultHeartbeat
	}
	if c.N2 == 0 {
		c.N2 = DefaultN2
	}
	if c.Paclen == 0 {
		c.Paclen = DefaultPaclen
	}
	if c.Window == 0 {
		if c.Mod128 {
			c.Window = DefaultWindowMod128
		} else {
			c.Window = DefaultWindowMod8
		}
	}
	if c.Backoff == backoffUnset {
		c.Backoff = DefaultBackoff
	}
	if c.Clock == nil {
		c.Clock = realClock{}
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
}

// Pending-timer bits. Timer goroutines OR these into pendingTimers
// atomically; the run loop drains them after each channel receive.
// This makes timer events guaranteed-delivery without unbounding the
// frame channel — a dropped T1 expiry would silently hang the link in
// TIMER_RECOVERY without ever reaching N2 retries.
const (
	pendT1 uint32 = 1 << iota
	pendT2
	pendT3
	pendHB
)

// Session is the per-link LAPB state machine driver.
type Session struct {
	cfg   SessionConfig
	in    chan Event
	state State
	v     vars

	t1, t2, t3, hb *timer
	pendingTimers  atomic.Uint32 // OR-set by timer goroutines, drained by run loop
	wakeup         chan struct{} // 1-slot signal that pendingTimers changed

	statsMu sync.Mutex // guards stats; holders: session goroutine (writes via mutateStats); external readers (Snapshot)
	stats   LinkStats
	pending [128]*Frame // I-frame retransmit buffer keyed by NS
	txBuf   []byte      // operator bytes pending TX
}

// Snapshot returns a goroutine-safe copy of the current LinkStats.
// Callable from any goroutine; the session goroutine mutates stats
// under the same mutex via mutateStats / setStateStats helpers.
func (s *Session) Snapshot() LinkStats {
	s.statsMu.Lock()
	defer s.statsMu.Unlock()
	return s.stats
}

// mutateStats applies fn under the stats mutex. Used by the session
// goroutine for every stats update so external Snapshot() readers see
// a consistent view.
func (s *Session) mutateStats(fn func(*LinkStats)) {
	s.statsMu.Lock()
	fn(&s.stats)
	s.statsMu.Unlock()
}

// NewSession constructs a Session. The returned Session is in
// StateDisconnected; the caller drives it via Submit + Run.
func NewSession(cfg SessionConfig) (*Session, error) {
	cfg.applyDefaults()
	if cfg.TxSink == nil {
		return nil, fmt.Errorf("ax25conn: TxSink required")
	}
	if cfg.Local.Call == "" || cfg.Peer.Call == "" {
		return nil, fmt.Errorf("ax25conn: Local and Peer required")
	}
	s := &Session{
		cfg:    cfg,
		in:     make(chan Event, 32),
		wakeup: make(chan struct{}, 1),
		state:  StateDisconnected,
	}
	s.t1 = newTimer(cfg.Clock, cfg.T1, func() { s.signalTimer(pendT1) })
	s.t2 = newTimer(cfg.Clock, cfg.T2, func() { s.signalTimer(pendT2) })
	s.t3 = newTimer(cfg.Clock, cfg.T3, func() { s.signalTimer(pendT3) })
	s.hb = newTimer(cfg.Clock, cfg.Heartbeat, func() { s.signalTimer(pendHB) })
	return s, nil
}

// modulus returns the active modulo (8 or 128) for sequence numbers.
func (s *Session) modulus() int {
	if s.cfg.Mod128 {
		return 128
	}
	return 8
}

// nextT1 returns the T1 duration to set on the next reset. Mirrors
// ax25_calculate_t1 in net/ax25/ax25_subr.c:220-258. The kernel
// scales a measured-and-clamped RTT by a backoff factor that grows
// with N2Count.
func (s *Session) nextT1() time.Duration {
	rtt := s.v.RTT
	if rtt == 0 {
		rtt = s.cfg.T1 / 2
	}
	if rtt < RTTClampLo {
		rtt = RTTClampLo
	}
	if rtt > RTTClampHi {
		rtt = RTTClampHi
	}
	switch s.cfg.Backoff {
	case BackoffNone:
		return 2 * rtt
	case BackoffExponential:
		shift := uint(s.v.N2Count)
		// Cap shift to avoid overflow at large N2; even N2=20 already
		// hits the 8x ceiling below. 30 covers any future expansion.
		if shift > 30 {
			shift = 30
		}
		d := time.Duration(1<<shift) * 2 * rtt
		if d > 8*rtt {
			d = 8 * rtt
		}
		if d < 2*rtt {
			d = 2 * rtt
		}
		return d
	case BackoffLinear:
		fallthrough
	default:
		return time.Duration(2+2*s.v.N2Count) * rtt
	}
}

// resetT1 stamps the start time and arms T1 with the backoff-adjusted
// duration. State handlers should call this rather than t1.reset()
// directly so RTT measurement remains accurate.
func (s *Session) resetT1() {
	s.v.T1Started = s.cfg.Clock.Now()
	s.t1.resetTo(s.nextT1())
}

// calcRTT folds the latest T1 measurement into the RTT EWMA per
// ax25_calculate_rtt (net/ax25/ax25_subr.c:220-258). Sampled only on
// first-shot success (n2count==0); retransmits don't poison the
// estimate.
func (s *Session) calcRTT() {
	if s.v.N2Count != 0 || s.v.T1Started.IsZero() {
		return
	}
	measured := s.cfg.Clock.Now().Sub(s.v.T1Started)
	if measured < 0 {
		return
	}
	if s.v.RTT == 0 {
		s.v.RTT = s.cfg.T1 / 2
	}
	s.v.RTT = (9*s.v.RTT + measured) / 10
	if s.v.RTT < RTTClampLo {
		s.v.RTT = RTTClampLo
	}
	if s.v.RTT > RTTClampHi {
		s.v.RTT = RTTClampHi
	}
}

// Submit places an event into the input queue.
func (s *Session) Submit(ev Event) { s.enqueue(ev) }

// enqueue routes a non-timer event onto the bounded channel. Drop-on-
// full is acceptable: EventFrameRX bursts are recoverable via LAPB
// retransmission; EventDataTX losses cause operator-visible UI errors
// that prompt a retry. Timer events do NOT use this path — see
// signalTimer.
func (s *Session) enqueue(e Event) {
	select {
	case s.in <- e:
	default:
		s.cfg.Logger.Warn("ax25conn: dropping event; session input full",
			"kind", e.Kind, "peer", s.cfg.Peer.String())
	}
}

// signalTimer ORs a timer-pending bit and wakes the run loop.
// Idempotent across multiple firings before the loop drains; the run
// loop processes each kind of timer expiry at most once per drain
// cycle, matching kernel ax25_timer.c semantics.
func (s *Session) signalTimer(bit uint32) {
	s.pendingTimers.Or(bit)
	select {
	case s.wakeup <- struct{}{}:
	default:
	}
}

// State returns the session's current LAPB state. Not goroutine-safe
// against concurrent transitions; use only for tests/stats display
// when the session is stable.
func (s *Session) State() State { return s.state }

// Run blocks until ctx is cancelled or EventShutdown is processed.
// Manager invokes Run in a goroutine. Each iteration drains pending
// timer bits before reading the channel so timer events never starve
// under RX bursts.
func (s *Session) Run(ctx context.Context) {
	defer s.cleanup()
	for {
		if bits := s.pendingTimers.Swap(0); bits != 0 {
			if bits&pendT1 != 0 && !s.handle(ctx, Event{Kind: EventT1Expiry}) {
				return
			}
			if bits&pendT2 != 0 && !s.handle(ctx, Event{Kind: EventT2Expiry}) {
				return
			}
			if bits&pendT3 != 0 && !s.handle(ctx, Event{Kind: EventT3Expiry}) {
				return
			}
			if bits&pendHB != 0 && !s.handle(ctx, Event{Kind: EventHeartbeat}) {
				return
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-s.wakeup:
			continue
		case ev := <-s.in:
			if !s.handle(ctx, ev) {
				return
			}
		}
	}
}

// handle dispatches by current state. Returns false when the session
// should exit.
func (s *Session) handle(ctx context.Context, ev Event) bool {
	// EventHeartbeat runs the housekeeping tick across all states; it
	// is not state-dispatched. The tick re-arms itself unconditionally
	// (see heartbeatTick).
	if ev.Kind == EventHeartbeat {
		s.heartbeatTick()
		return true
	}
	switch s.state {
	case StateDisconnected:
		return s.onDisconnected(ctx, ev)
	case StateAwaitingConnection:
		return s.onAwaitingConnection(ctx, ev)
	case StateConnected:
		return s.onConnected(ctx, ev)
	case StateTimerRecovery:
		return s.onTimerRecovery(ctx, ev)
	case StateAwaitingRelease:
		return s.onAwaitingRelease(ctx, ev)
	}
	return false
}

func (s *Session) cleanup() {
	s.t1.stop()
	s.t2.stop()
	s.t3.stop()
	s.hb.stop()
	s.emit(OutEvent{Kind: OutStateChange, State: StateDisconnected})
}

func (s *Session) emit(ev OutEvent) {
	if s.cfg.Observer != nil {
		s.cfg.Observer(ev)
	}
}

func (s *Session) setState(ns State) {
	if ns == s.state {
		return
	}
	s.state = ns
	s.mutateStats(func(st *LinkStats) { st.State = ns })
	s.cfg.Logger.Info("ax25conn: state change",
		"peer", s.cfg.Peer.String(), "to", ns)
	s.emit(OutEvent{Kind: OutStateChange, State: ns})
	s.emit(OutEvent{Kind: OutLinkStats, Stats: s.Snapshot()})
}

// State handlers live in transitions_<state>.go.
