package ax25conn

import (
	"context"
	"fmt"
	"log/slog"
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
	if c.Backoff == 0 {
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

	stats   LinkStats
	pending [128]*Frame // I-frame retransmit buffer keyed by NS
	txBuf   []byte      // operator bytes pending TX
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
	s.stats.State = ns
	s.cfg.Logger.Info("ax25conn: state change",
		"peer", s.cfg.Peer.String(), "to", ns)
	s.emit(OutEvent{Kind: OutStateChange, State: ns})
}

// Stub state handlers; filled in by transitions_<state>.go in later
// tasks. Keeping the package compilable lets every task land its own
// commit.
func (s *Session) onDisconnected(ctx context.Context, ev Event) bool {
	if ev.Kind == EventShutdown {
		return false
	}
	return true
}

func (s *Session) onAwaitingConnection(ctx context.Context, ev Event) bool {
	if ev.Kind == EventShutdown {
		return false
	}
	return true
}

func (s *Session) onConnected(ctx context.Context, ev Event) bool {
	if ev.Kind == EventShutdown {
		return false
	}
	return true
}

func (s *Session) onTimerRecovery(ctx context.Context, ev Event) bool {
	if ev.Kind == EventShutdown {
		return false
	}
	return true
}

func (s *Session) onAwaitingRelease(ctx context.Context, ev Event) bool {
	if ev.Kind == EventShutdown {
		return false
	}
	return true
}
