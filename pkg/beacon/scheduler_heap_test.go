package beacon

import (
	"container/heap"
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/gps"
	"github.com/chrissnell/graywolf/pkg/internal/testtx"
)

// fakeClock is a deterministic Clock for heap-scheduler tests. Now is
// controlled directly; After returns a channel that fires only when the
// test calls Advance past the target. Waiter registration is observable
// via waitForWaiters so tests know the scheduler is blocked and ready
// for the next Advance.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []*fakeWaiter
}

type fakeWaiter struct {
	target time.Time
	ch     chan time.Time
}

func newFakeClock(start time.Time) *fakeClock { return &fakeClock{now: start} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	target := c.now.Add(d)
	if !target.After(c.now) {
		ch <- c.now
		return ch
	}
	c.waiters = append(c.waiters, &fakeWaiter{target: target, ch: ch})
	return ch
}

// Advance pushes the clock forward by d and fires every waiter whose
// target has been reached. The firing happens outside the lock so
// receivers that re-enter After (the scheduler does, every iteration)
// cannot deadlock against Advance.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	kept := c.waiters[:0]
	var fire []*fakeWaiter
	for _, w := range c.waiters {
		if !w.target.After(now) {
			fire = append(fire, w)
		} else {
			kept = append(kept, w)
		}
	}
	c.waiters = append([]*fakeWaiter(nil), kept...)
	c.mu.Unlock()
	for _, w := range fire {
		w.ch <- now
	}
}

// waiterCount reports the current pending-After count.
func (c *fakeClock) waiterCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.waiters)
}

// waitForWaiters spins until at least n After calls are pending or the
// deadline elapses. Tests use it to gate on "scheduler is blocked and
// ready for the next Advance" without racing on goroutine scheduling.
func (c *fakeClock) waitForWaiters(t *testing.T, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if c.waiterCount() >= n {
			return
		}
		time.Sleep(200 * time.Microsecond)
	}
	t.Fatalf("timed out waiting for %d clock waiters (have %d)", n, c.waiterCount())
}

// skipObserver counts OnBeaconSkipped callbacks and implements
// beacon.Observer + beacon.SkipObserver.
type skipObserver struct {
	sent    atomic.Int64
	rate    atomic.Int64
	skipped atomic.Int64

	mu           sync.Mutex
	lastSkipName string
	lastSkipKey  string
}

func (o *skipObserver) OnBeaconSent(_ Type)                         { o.sent.Add(1) }
func (o *skipObserver) OnSmartBeaconRate(_ uint32, _ time.Duration) { o.rate.Add(1) }
func (o *skipObserver) OnBeaconSkipped(name, reason string) {
	o.skipped.Add(1)
	o.mu.Lock()
	o.lastSkipName = name
	o.lastSkipKey = reason
	o.mu.Unlock()
}

// blockingSink holds Submit calls on a release channel so tests can
// saturate the scheduler's worker pool deterministically.
type blockingSink struct {
	*testtx.Recorder
	release chan struct{}
	inFlight atomic.Int64
}

func newBlockingSink() *blockingSink {
	s := &blockingSink{Recorder: testtx.NewRecorder(), release: make(chan struct{})}
	s.OnSubmit(func(_ testtx.Capture) {
		s.inFlight.Add(1)
		<-s.release
		s.inFlight.Add(-1)
	})
	return s
}

func mkPosBeacon(t *testing.T, id uint32, every time.Duration, comment string) Config {
	t.Helper()
	return Config{
		ID:          id,
		Type:        TypePosition,
		Channel:     0,
		Source:      mustAddr(t, "N0CALL-9"),
		Dest:        mustAddr(t, "APGRWO"),
		Path:        []ax25.Address{mustAddr(t, "WIDE1-1")},
		Delay:       0,
		Every:       every,
		Slot:        -1,
		Lat:         37.0,
		Lon:         -122.0,
		SymbolTable: '/',
		SymbolCode:  '-',
		Comment:     comment,
		Enabled:     true,
	}
}

// TestHeapScheduler_SingleBeaconFixedInterval (scenario 1) drives a
// single beacon through the heap loop on a fake clock, advances time,
// and asserts the sink recorded the expected frame count.
func TestHeapScheduler_SingleBeaconFixedInterval(t *testing.T) {
	clock := newFakeClock(time.Unix(0, 0))
	sink := testtx.NewRecorder()
	obs := &skipObserver{}
	s, err := New(Options{
		Sink:     sink,
		Logger:   slog.New(slog.NewTextHandler(logSink{}, nil)),
		Clock:    clock,
		Observer: obs,
	})
	if err != nil {
		t.Fatal(err)
	}
	s.SetBeacons([]Config{mkPosBeacon(t, 1, 100*time.Millisecond, "hi")})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	// First fire happens immediately (Delay=0). After the loop reaches
	// its next wait we can start advancing the clock.
	waitForFrames(t, sink, 1, time.Second)
	clock.waitForWaiters(t, 1, time.Second)

	for i := 2; i <= 4; i++ {
		clock.Advance(100 * time.Millisecond)
		waitForFrames(t, sink, i, time.Second)
		clock.waitForWaiters(t, 1, time.Second)
	}

	if n := sink.Len(); n != 4 {
		t.Fatalf("frame count = %d, want 4", n)
	}
	if obs.sent.Load() != 4 {
		t.Errorf("observer sent = %d, want 4", obs.sent.Load())
	}
}

// TestHeapScheduler_MultipleBeaconsOrder (scenario 2) configures two
// beacons with different intervals and verifies the run-loop fires them
// in the right order after successive clock advances.
func TestHeapScheduler_MultipleBeaconsOrder(t *testing.T) {
	clock := newFakeClock(time.Unix(0, 0))
	sink := testtx.NewRecorder()
	s, _ := New(Options{
		Sink:   sink,
		Logger: slog.New(slog.NewTextHandler(logSink{}, nil)),
		Clock:  clock,
	})
	// Two beacons: A at 100ms cadence, B at 150ms. Both share initial
	// fire at t=0 so the heap test starts deterministically.
	a := mkPosBeacon(t, 1, 100*time.Millisecond, "A")
	b := mkPosBeacon(t, 2, 150*time.Millisecond, "B")
	s.SetBeacons([]Config{a, b})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	// Both initial fires happen at t=0 before the first wait. They
	// dispatch onto the worker pool concurrently, so the two initial
	// frames can land in either order; assert on the set, not the
	// sequence.
	waitForFrames(t, sink, 2, time.Second)
	clock.waitForWaiters(t, 1, time.Second)
	initialDetails := map[string]int{}
	for _, c := range sink.Captures()[:2] {
		initialDetails[c.Source.Detail]++
	}
	if initialDetails["position/1"] != 1 || initialDetails["position/2"] != 1 {
		t.Fatalf("initial frames = %+v, want one each of position/{1,2}", initialDetails)
	}

	// t=100ms → A (period 100ms) fires next. The advance triggers
	// exactly one fire and we wait for the sink to record it before
	// advancing again, so frame[2] is deterministically A.
	clock.Advance(100 * time.Millisecond)
	waitForFrames(t, sink, 3, time.Second)
	clock.waitForWaiters(t, 1, time.Second)
	if got := sink.Captures()[2].Source.Detail; got != "position/1" {
		t.Errorf("frame[2] detail = %q, want position/1", got)
	}

	// t=150ms → B (period 150ms) fires next. Same reasoning: the wait
	// gate serializes observation of frame[3].
	clock.Advance(50 * time.Millisecond)
	waitForFrames(t, sink, 4, time.Second)
	clock.waitForWaiters(t, 1, time.Second)
	if got := sink.Captures()[3].Source.Detail; got != "position/2" {
		t.Errorf("frame[3] detail = %q, want position/2", got)
	}
}

// TestHeapScheduler_ReloadAtomic (scenario 3) starts with three beacons,
// reloads with a different set, and verifies only the new set fires
// after the reload takes effect.
func TestHeapScheduler_ReloadAtomic(t *testing.T) {
	clock := newFakeClock(time.Unix(0, 0))
	sink := testtx.NewRecorder()
	s, _ := New(Options{
		Sink:   sink,
		Logger: slog.New(slog.NewTextHandler(logSink{}, nil)),
		Clock:  clock,
	})
	initial := []Config{
		mkPosBeacon(t, 1, time.Second, "old-1"),
		mkPosBeacon(t, 2, time.Second, "old-2"),
		mkPosBeacon(t, 3, time.Second, "old-3"),
	}
	s.SetBeacons(initial)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	// Initial three fires land first.
	waitForFrames(t, sink, 3, time.Second)
	clock.waitForWaiters(t, 1, time.Second)
	preReload := sink.Len()

	// Reload with two beacons, different IDs and comments.
	next := []Config{
		mkPosBeacon(t, 10, 500*time.Millisecond, "new-10"),
		mkPosBeacon(t, 11, 500*time.Millisecond, "new-11"),
	}
	s.Reload(next)

	// After reload the new heap has two plans with Delay=0 so both fire
	// immediately on the rebuild. Wait for the two post-reload frames.
	waitForFrames(t, sink, preReload+2, time.Second)
	clock.waitForWaiters(t, 1, time.Second)

	frames := sink.Captures()
	for i := preReload; i < len(frames); i++ {
		det := frames[i].Source.Detail
		if det != "position/10" && det != "position/11" {
			t.Errorf("post-reload frame %d has detail %q (old beacon leaked)", i, det)
		}
	}
	// Advance past one new interval; the new beacons should fire again,
	// the old ones must not.
	clock.Advance(500 * time.Millisecond)
	waitForFrames(t, sink, preReload+4, time.Second)
	for i := preReload; i < sink.Len(); i++ {
		det := sink.Captures()[i].Source.Detail
		if det == "position/1" || det == "position/2" || det == "position/3" {
			t.Errorf("frame %d post-reload still references old beacon %q", i, det)
		}
	}
}

// TestHeapScheduler_WorkerPoolSaturated (scenario 4) saturates the
// bounded worker pool with a blocking sink and verifies that subsequent
// fires increment the skipped_busy counter on the observer.
func TestHeapScheduler_WorkerPoolSaturated(t *testing.T) {
	sink := newBlockingSink()
	obs := &skipObserver{}
	s, err := New(Options{
		Sink:               sink,
		Logger:             slog.New(slog.NewTextHandler(logSink{}, nil)),
		Observer:           obs,
		MaxConcurrentFires: 1, // single worker so test can saturate it
	})
	if err != nil {
		t.Fatal(err)
	}

	// Two enabled beacons. Both are due immediately; the first grabs
	// the single worker slot and blocks in Submit, the second must be
	// skipped because fireAsync cannot enqueue.
	s.SetBeacons([]Config{
		mkPosBeacon(t, 1, 10*time.Millisecond, "a"),
		mkPosBeacon(t, 2, 10*time.Millisecond, "b"),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx) }()

	// Wait for the first Submit to be in flight (worker pool full).
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && sink.inFlight.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	if sink.inFlight.Load() == 0 {
		t.Fatal("first Submit never reached the sink")
	}

	// Wait until at least one skip has been reported. The scheduler
	// will keep rescheduling beacon 2 (and more of beacon 1) while the
	// first Submit is blocked; every one of those fires should skip.
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) && obs.skipped.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	if got := obs.skipped.Load(); got == 0 {
		t.Fatalf("expected skipped_busy count > 0, got 0")
	}
	obs.mu.Lock()
	if obs.lastSkipKey != "busy" {
		t.Errorf("last skip reason = %q, want %q", obs.lastSkipKey, "busy")
	}
	obs.mu.Unlock()

	// Release the blocked worker so the scheduler can drain cleanly.
	close(sink.release)
}

// TestHeapScheduler_SmartBeaconSpeed (scenario 5) exercises nextWake
// against a GPS cache that returns a rising speed and verifies the
// smart-beacon wake interval falls as speed increases, matching the
// SmartBeaconConfig.Interval curve.
func TestHeapScheduler_SmartBeaconSpeed(t *testing.T) {
	cache := gps.NewMemCache()
	sink := testtx.NewRecorder()
	obs := &skipObserver{}
	sb := &SmartBeaconConfig{
		Enabled:   true,
		FastSpeed: 60,
		FastRate:  180 * time.Second,
		SlowSpeed: 5,
		SlowRate:  1800 * time.Second,
		TurnTime:  15 * time.Second,
		TurnAngle: 30,
		TurnSlope: 255,
	}
	s, err := New(Options{
		Sink:     sink,
		Cache:    cache,
		Logger:   slog.New(slog.NewTextHandler(logSink{}, nil)),
		Observer: obs,
	})
	if err != nil {
		t.Fatal(err)
	}

	plan := &beaconPlan{cfg: Config{
		ID:          99,
		Type:        TypeTracker,
		Source:      mustAddr(t, "N0CALL-7"),
		Dest:        mustAddr(t, "APGRWO"),
		SmartBeacon: sb,
		Enabled:     true,
	}}

	now := time.Unix(0, 0)

	// Idle (speed=0). Interval should be SlowRate; nextWake is clamped
	// to smartPollInterval (1s), so the wake must be exactly 1s out.
	cache.Update(gps.Fix{Speed: 0})
	gotIdle := s.nextWake(plan, now).Sub(now)
	if gotIdle != smartPollInterval {
		t.Errorf("idle nextWake = %v, want %v", gotIdle, smartPollInterval)
	}
	if sb.Interval(0) != sb.SlowRate {
		t.Errorf("interval at speed=0 = %v, want %v", sb.Interval(0), sb.SlowRate)
	}

	// Highway speed: interval collapses to FastRate (180s); nextWake
	// still clamps to smartPollInterval because 1s < 180s.
	cache.Update(gps.Fix{Speed: 65, Heading: 90, HasCourse: true})
	gotFast := s.nextWake(plan, now).Sub(now)
	if gotFast != smartPollInterval {
		t.Errorf("fast nextWake = %v, want %v", gotFast, smartPollInterval)
	}
	if sb.Interval(65) != sb.FastRate {
		t.Errorf("interval at 65kt = %v, want %v", sb.Interval(65), sb.FastRate)
	}

	// OnSmartBeaconRate must have been called for every scheduleNext
	// invocation above (two so far).
	if obs.rate.Load() != 2 {
		t.Errorf("OnSmartBeaconRate calls = %d, want 2", obs.rate.Load())
	}

	// Sanity-check the monotonic-in-speed property of the raw interval
	// curve: higher speed → strictly shorter interval (between slow
	// and fast bounds).
	lo := sb.Interval(10)
	hi := sb.Interval(50)
	if !(hi < lo) {
		t.Errorf("interval not monotonic: Interval(10)=%v, Interval(50)=%v", lo, hi)
	}
}

// TestHeapScheduler_CtxCancelStopsQuickly (scenario 6) asserts that
// cancelling the run context terminates the loop within 100 ms, so
// shutdown cannot hang behind a long beacon interval.
func TestHeapScheduler_CtxCancelStopsQuickly(t *testing.T) {
	sink := testtx.NewRecorder()
	s, _ := New(Options{
		Sink:   sink,
		Logger: slog.New(slog.NewTextHandler(logSink{}, nil)),
	})
	s.SetBeacons([]Config{mkPosBeacon(t, 1, time.Hour, "x")})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = s.Run(ctx)
		close(done)
	}()

	// Give the scheduler a moment to make the initial fire and block
	// on the hour-long wait.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Run did not return within 100ms of ctx cancel")
	}
}

// TestBeaconHeap_PushPopOrder pins the min-heap ordering (earliest
// nextFire first, ID tie-break) so future heap tweaks cannot silently
// change the scheduler's emission order.
func TestBeaconHeap_PushPopOrder(t *testing.T) {
	base := time.Unix(0, 0)
	plans := []*beaconPlan{
		{cfg: Config{ID: 3}, nextFire: base.Add(300 * time.Millisecond)},
		{cfg: Config{ID: 1}, nextFire: base.Add(100 * time.Millisecond)},
		{cfg: Config{ID: 2}, nextFire: base.Add(100 * time.Millisecond)}, // ties
		{cfg: Config{ID: 4}, nextFire: base.Add(200 * time.Millisecond)},
	}
	h := beaconHeap{}
	for _, p := range plans {
		h = append(h, p)
	}
	// heap.Init orders h in-place.
	heap.Init(&h)

	wantIDs := []uint32{1, 2, 4, 3}
	for i, want := range wantIDs {
		if h.Peek() == nil {
			t.Fatalf("peek %d returned nil", i)
		}
		got := h.Peek().cfg.ID
		if got != want {
			t.Errorf("peek %d = %d, want %d", i, got, want)
		}
		heap.Pop(&h)
	}
}

// waitForFrames blocks until the recorder has at least n captures or
// times out.
func waitForFrames(t *testing.T, r *testtx.Recorder, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if r.Len() >= n {
			return
		}
		time.Sleep(200 * time.Microsecond)
	}
	t.Fatalf("timed out waiting for %d frames (have %d)", n, r.Len())
}
