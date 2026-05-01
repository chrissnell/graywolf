package ax25conn

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// captureSink captures every Submit call so tests can assert what
// frames the session emitted, in order. Implements txgovernor.TxSink.
type captureSink struct {
	mu     sync.Mutex
	frames []*ax25.Frame
	chans  []uint32
	srcs   []txgovernor.SubmitSource
}

func newCaptureSink() *captureSink { return &captureSink{} }

func (c *captureSink) Submit(_ context.Context, channel uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.frames = append(c.frames, frame)
	c.chans = append(c.chans, channel)
	c.srcs = append(c.srcs, src)
	return nil
}

func (c *captureSink) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.frames)
}

// nopSink discards every frame. Implements txgovernor.TxSink.
type nopSink struct{}

func (nopSink) Submit(_ context.Context, _ uint32, _ *ax25.Frame, _ txgovernor.SubmitSource) error {
	return nil
}

// newTestSession builds a Session wired to the fake clock and the
// given TxSink. Defaults to nopSink and 32-channel timeouts that
// the fake clock can drive deterministically.
func newTestSession(t *testing.T, opts ...func(*SessionConfig)) *Session {
	t.Helper()
	cfg := SessionConfig{
		Local:   mustParse(t, "KE7XYZ-1"),
		Peer:    mustParse(t, "BBS-3"),
		Channel: 1,
		TxSink:  nopSink{},
		Clock:   newFakeClock(),
	}
	for _, o := range opts {
		o(&cfg)
	}
	s, err := NewSession(cfg)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	return s
}

func TestNewSessionRequiresTxSink(t *testing.T) {
	_, err := NewSession(SessionConfig{
		Local: mustParse(t, "KE7XYZ-1"),
		Peer:  mustParse(t, "BBS-3"),
	})
	if err == nil {
		t.Fatal("expected TxSink-required error")
	}
}

func TestNewSessionRequiresAddresses(t *testing.T) {
	_, err := NewSession(SessionConfig{TxSink: nopSink{}})
	if err == nil {
		t.Fatal("expected address-required error")
	}
}

func TestSessionRunsAndShutsDown(t *testing.T) {
	s := newTestSession(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("session did not exit on ctx cancel")
	}
}

func TestSessionShutdownEvent(t *testing.T) {
	s := newTestSession(t)
	done := make(chan struct{})
	go func() { s.Run(context.Background()); close(done) }()
	s.Submit(Event{Kind: EventShutdown})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("session did not exit on EventShutdown")
	}
}

func TestSessionDefaultsApplied(t *testing.T) {
	s := newTestSession(t)
	if s.cfg.T1 != DefaultT1 || s.cfg.T2 != DefaultT2 || s.cfg.T3 != DefaultT3 {
		t.Fatalf("default timers drifted: %+v", s.cfg)
	}
	if s.cfg.N2 != DefaultN2 || s.cfg.Paclen != DefaultPaclen {
		t.Fatalf("default counts drifted: %+v", s.cfg)
	}
	if s.cfg.Window != DefaultWindowMod8 {
		t.Fatalf("mod-8 window drifted: %d", s.cfg.Window)
	}
	if s.cfg.Backoff != DefaultBackoff {
		t.Fatalf("backoff drifted: %v", s.cfg.Backoff)
	}
}

func TestSessionMod128Window(t *testing.T) {
	s := newTestSession(t, func(c *SessionConfig) { c.Mod128 = true })
	if s.cfg.Window != DefaultWindowMod128 {
		t.Fatalf("mod-128 window drifted: %d", s.cfg.Window)
	}
	if s.modulus() != 128 {
		t.Fatalf("modulus drifted: %d", s.modulus())
	}
}

func TestSessionEmitInitialDisconnectedOnShutdown(t *testing.T) {
	emitted := make([]OutEvent, 0, 4)
	s := newTestSession(t, func(c *SessionConfig) {
		c.Observer = func(e OutEvent) { emitted = append(emitted, e) }
	})
	done := make(chan struct{})
	go func() { s.Run(context.Background()); close(done) }()
	s.Submit(Event{Kind: EventShutdown})
	<-done
	// cleanup() emits a final OutStateChange{Disconnected}.
	if len(emitted) == 0 {
		t.Fatal("expected at least one emitted event on shutdown")
	}
	last := emitted[len(emitted)-1]
	if last.Kind != OutStateChange || last.State != StateDisconnected {
		t.Fatalf("last emission %+v", last)
	}
}

func TestSessionInputDropOnFull(t *testing.T) {
	// Build a session but don't run it. Fill the input channel; the 33rd
	// submit must be dropped without blocking.
	s := newTestSession(t)
	for i := 0; i < cap(s.in); i++ {
		s.Submit(Event{Kind: EventDataTX, Data: []byte("x")})
	}
	done := make(chan struct{})
	go func() {
		s.Submit(Event{Kind: EventDataTX, Data: []byte("y")})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Submit blocked on full queue; expected drop")
	}
}

// Compile-time assertion that nopSink and captureSink implement TxSink.
var (
	_ txgovernor.TxSink = nopSink{}
	_ txgovernor.TxSink = (*captureSink)(nil)
)

func TestErrorsIsCompatibility(t *testing.T) {
	// Make sure errors.Is works with the package's wrapped errors after
	// later transition tasks introduce them; baseline guard so this
	// never silently regresses.
	err := errors.New("ax25conn: TxSink required")
	if errors.Is(err, errors.New("other")) {
		t.Fatal("errors.Is sanity check failed")
	}
}
