package cot

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/beacon"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// --- test fakes ------------------------------------------------------

type fakeSink struct {
	mu     sync.Mutex
	frames []capturedSubmit
}

// capturedSubmit pairs a submitted frame with its SubmitSource so tests
// can assert on the wire info-field content (e.g. the kill status byte)
// as well as the dedup/priority metadata.
type capturedSubmit struct {
	frame *ax25.Frame
	src   txgovernor.SubmitSource
}

func (f *fakeSink) Submit(_ context.Context, _ uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.frames = append(f.frames, capturedSubmit{frame: frame, src: src})
	return nil
}

func (f *fakeSink) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.frames)
}

type fakeISSink struct {
	mu    sync.Mutex
	lines []string
	err   error
}

func (f *fakeISSink) SendLine(line string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.lines = append(f.lines, line)
	return nil
}

func (f *fakeISSink) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.lines)
}

type fakeStore struct {
	mu      sync.Mutex
	targets map[uint32]configstore.CotTarget
}

func newFakeStore(targets ...configstore.CotTarget) *fakeStore {
	m := make(map[uint32]configstore.CotTarget, len(targets))
	for _, t := range targets {
		m[t.ID] = t
	}
	return &fakeStore{targets: m}
}

func (f *fakeStore) GetCotTarget(_ context.Context, id uint32) (*configstore.CotTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.targets[id]
	if !ok {
		return nil, errors.New("cot: target not found")
	}
	return &t, nil
}

func (f *fakeStore) DueCotTargets(_ context.Context, now time.Time) ([]configstore.CotTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []configstore.CotTarget
	for _, t := range f.targets {
		if t.NextSendAt != nil && !t.NextSendAt.After(now) {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeStore) RecordCotSend(_ context.Context, id uint32, sentAt time.Time, nextSendAt *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.targets[id]
	if !ok {
		return errors.New("cot: target not found")
	}
	t.TxCount++
	if t.FirstSentAt == nil {
		fs := sentAt
		t.FirstSentAt = &fs
	}
	ls := sentAt
	t.LastSentAt = &ls
	t.NextSendAt = nextSendAt
	f.targets[id] = t
	return nil
}

func (f *fakeStore) get(id uint32) configstore.CotTarget {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.targets[id]
}

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func mkTarget(id uint32, sendPath string) configstore.CotTarget {
	return configstore.CotTarget{
		ID:                   id,
		ObjectName:           "TESTOBJ",
		SymbolTable:          "/",
		Symbol:               "D",
		Comment:              "test cot",
		Latitude:             37.7749,
		Longitude:            -122.4194,
		Type:                 "object",
		SendPath:             sendPath,
		Channel:              1,
		Destination:          "APGRWO",
		Path:                 "WIDE1-1",
		NumTransmits:         4,
		SecondTxDelaySeconds: 300,
		DecayFactor:          2,
	}
}

func newTestScheduler(t *testing.T, sink txgovernor.TxSink, isSink beacon.ISSink, store Store, now time.Time) *Scheduler {
	t.Helper()
	s, err := New(Options{
		Sink:   sink,
		ISSink: isSink,
		Store:  store,
		Clock:  fixedClock{t: now},
		StationCallsignResolver: func(context.Context) (string, error) {
			return "N0CALL-9", nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// --- tests -------------------------------------------------------------

func TestSendScheduled_RFOnly_IncrementsCountAndSchedulesNext(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	target := mkTarget(1, beacon.SendPathRF)
	store := newFakeStore(target)
	sink := &fakeSink{}
	isSink := &fakeISSink{}
	sched := newTestScheduler(t, sink, isSink, store, now)

	if err := sched.SendScheduled(context.Background(), 1); err != nil {
		t.Fatalf("SendScheduled: %v", err)
	}
	if got := sink.count(); got != 1 {
		t.Fatalf("RF frames = %d, want 1", got)
	}
	if got := isSink.count(); got != 0 {
		t.Fatalf("IS lines = %d, want 0 (rf-only)", got)
	}
	updated := store.get(1)
	if updated.TxCount != 1 {
		t.Fatalf("TxCount = %d, want 1", updated.TxCount)
	}
	if updated.FirstSentAt == nil || !updated.FirstSentAt.Equal(now) {
		t.Fatalf("FirstSentAt = %v, want %v", updated.FirstSentAt, now)
	}
	if updated.NextSendAt == nil {
		t.Fatal("NextSendAt is nil, want scheduled (TxCount 1 < NumTransmits 4)")
	}
	wantNext := now.Add(300 * time.Second)
	if !updated.NextSendAt.Equal(wantNext) {
		t.Fatalf("NextSendAt = %v, want %v", updated.NextSendAt, wantNext)
	}
}

func TestSendScheduled_Both_SendsRFAndIS(t *testing.T) {
	now := time.Now()
	target := mkTarget(1, beacon.SendPathBoth)
	store := newFakeStore(target)
	sink := &fakeSink{}
	isSink := &fakeISSink{}
	sched := newTestScheduler(t, sink, isSink, store, now)

	if err := sched.SendScheduled(context.Background(), 1); err != nil {
		t.Fatalf("SendScheduled: %v", err)
	}
	if got := sink.count(); got != 1 {
		t.Fatalf("RF frames = %d, want 1", got)
	}
	if got := isSink.count(); got != 1 {
		t.Fatalf("IS lines = %d, want 1", got)
	}
}

func TestSendScheduled_ISOnly_NoRFSubmit(t *testing.T) {
	now := time.Now()
	target := mkTarget(1, beacon.SendPathISOnly)
	store := newFakeStore(target)
	sink := &fakeSink{}
	isSink := &fakeISSink{}
	sched := newTestScheduler(t, sink, isSink, store, now)

	if err := sched.SendScheduled(context.Background(), 1); err != nil {
		t.Fatalf("SendScheduled: %v", err)
	}
	if got := sink.count(); got != 0 {
		t.Fatalf("RF frames = %d, want 0 (is_only)", got)
	}
	if got := isSink.count(); got != 1 {
		t.Fatalf("IS lines = %d, want 1", got)
	}
}

func TestSendScheduled_ISOnly_NoSink_Errors(t *testing.T) {
	now := time.Now()
	target := mkTarget(1, beacon.SendPathISOnly)
	store := newFakeStore(target)
	sink := &fakeSink{}
	sched := newTestScheduler(t, sink, nil, store, now) // no ISSink

	if err := sched.SendScheduled(context.Background(), 1); err == nil {
		t.Fatal("expected error when is_only target has no APRS-IS sink")
	}
	if updated := store.get(1); updated.TxCount != 0 {
		t.Fatalf("TxCount = %d, want 0 (send failed, no record)", updated.TxCount)
	}
}

func TestSendScheduled_Exhausted_SetsNextSendAtNil(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 20, 0, 0, time.UTC)
	first := now.Add(-20 * time.Minute)
	target := mkTarget(1, beacon.SendPathRF)
	target.TxCount = 3 // about to become the 4th (final) send
	target.FirstSentAt = &first
	store := newFakeStore(target)
	sched := newTestScheduler(t, &fakeSink{}, &fakeISSink{}, store, now)

	if err := sched.SendScheduled(context.Background(), 1); err != nil {
		t.Fatalf("SendScheduled: %v", err)
	}
	updated := store.get(1)
	if updated.TxCount != 4 {
		t.Fatalf("TxCount = %d, want 4", updated.TxCount)
	}
	if updated.NextSendAt != nil {
		t.Fatalf("NextSendAt = %v, want nil (exhausted)", updated.NextSendAt)
	}
}

func TestSendNow_DoesNotTouchTxCountOrNextSendAt(t *testing.T) {
	now := time.Now()
	next := now.Add(5 * time.Minute)
	target := mkTarget(1, beacon.SendPathRF)
	target.TxCount = 1
	target.NextSendAt = &next
	store := newFakeStore(target)
	sink := &fakeSink{}
	sched := newTestScheduler(t, sink, &fakeISSink{}, store, now)

	if err := sched.SendNow(context.Background(), 1); err != nil {
		t.Fatalf("SendNow: %v", err)
	}
	if got := sink.count(); got != 1 {
		t.Fatalf("RF frames = %d, want 1", got)
	}
	updated := store.get(1)
	if updated.TxCount != 1 {
		t.Fatalf("TxCount = %d, want unchanged 1", updated.TxCount)
	}
	if updated.NextSendAt == nil || !updated.NextSendAt.Equal(next) {
		t.Fatalf("NextSendAt = %v, want unchanged %v", updated.NextSendAt, next)
	}
	// SkipDedup must be true on a manual send.
	if !sink.frames[0].src.SkipDedup {
		t.Error("manual SendNow submitted with SkipDedup=false, want true")
	}
}

// TestSendScheduled_OnISSent_FiresForISOnly guards invariant 57: the IS
// leg bypasses the governor entirely, so an is_only target's own
// OnISSent callback is the only thing that can feed the station cache.
func TestSendScheduled_OnISSent_FiresForISOnly(t *testing.T) {
	now := time.Now()
	target := mkTarget(1, beacon.SendPathISOnly)
	store := newFakeStore(target)
	var gotFrame *ax25.Frame
	sched, err := New(Options{
		Sink:   &fakeSink{},
		ISSink: &fakeISSink{},
		Store:  store,
		Clock:  fixedClock{t: now},
		StationCallsignResolver: func(context.Context) (string, error) {
			return "N0CALL-9", nil
		},
		OnISSent: func(frame *ax25.Frame, _ uint32) { gotFrame = frame },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := sched.SendScheduled(context.Background(), 1); err != nil {
		t.Fatalf("SendScheduled: %v", err)
	}
	if gotFrame == nil {
		t.Fatal("OnISSent was not called for an is_only send")
	}
}

func TestSendScheduled_OnISSent_NotFiredForRFOnly(t *testing.T) {
	now := time.Now()
	target := mkTarget(1, beacon.SendPathRF)
	store := newFakeStore(target)
	called := false
	sched, err := New(Options{
		Sink:  &fakeSink{},
		Store: store,
		Clock: fixedClock{t: now},
		StationCallsignResolver: func(context.Context) (string, error) {
			return "N0CALL-9", nil
		},
		OnISSent: func(*ax25.Frame, uint32) { called = true },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := sched.SendScheduled(context.Background(), 1); err != nil {
		t.Fatalf("SendScheduled: %v", err)
	}
	if called {
		t.Fatal("OnISSent fired for an rf-only send, want no-op")
	}
}

func TestSend_MissingCallsignResolver_ReturnsCallsignError(t *testing.T) {
	now := time.Now()
	target := mkTarget(1, beacon.SendPathRF)
	store := newFakeStore(target)
	sched, err := New(Options{Sink: &fakeSink{}, Store: store, Clock: fixedClock{t: now}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = sched.SendNow(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error when no station callsign resolver is configured")
	}
	var sendErr *SendError
	if !errors.As(err, &sendErr) || sendErr.Kind != SendErrorCallsign {
		t.Fatalf("err = %v, want *SendError{Kind: SendErrorCallsign}", err)
	}
}

func TestTick_SendsOnlyDueTargets(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	due := mkTarget(1, beacon.SendPathRF)
	dueAt := now.Add(-time.Second)
	due.NextSendAt = &dueAt
	notDue := mkTarget(2, beacon.SendPathRF)
	future := now.Add(time.Hour)
	notDue.NextSendAt = &future
	store := newFakeStore(due, notDue)
	sink := &fakeSink{}
	sched := newTestScheduler(t, sink, &fakeISSink{}, store, now)

	sched.tick(context.Background())

	if got := sink.count(); got != 1 {
		t.Fatalf("frames sent = %d, want 1 (only the due target)", got)
	}
	if got := store.get(1).TxCount; got != 1 {
		t.Fatalf("due target TxCount = %d, want 1", got)
	}
	if got := store.get(2).TxCount; got != 0 {
		t.Fatalf("not-due target TxCount = %d, want 0", got)
	}
}

func TestSendKill_UsesDeadStatusByte(t *testing.T) {
	now := time.Now()
	target := mkTarget(1, beacon.SendPathRF)
	store := newFakeStore(target)
	sink := &fakeSink{}
	sched := newTestScheduler(t, sink, &fakeISSink{}, store, now)

	if err := sched.SendKill(context.Background(), 1); err != nil {
		t.Fatalf("SendKill: %v", err)
	}
	if got := sink.count(); got != 1 {
		t.Fatalf("RF frames = %d, want 1", got)
	}
	// APRS101 ch 11: object name is a fixed 9-byte field, so the status
	// byte (live '*' vs killed '_') sits at info[10].
	info := sink.frames[0].frame.Info
	if info[10] != '_' {
		t.Fatalf("status byte = %q, want kill ('_')", info[10])
	}
}

func TestSendKill_DoesNotRecordSend(t *testing.T) {
	now := time.Now()
	next := now.Add(5 * time.Minute)
	target := mkTarget(1, beacon.SendPathRF)
	target.TxCount = 1
	target.NextSendAt = &next
	store := newFakeStore(target)
	sched := newTestScheduler(t, &fakeSink{}, &fakeISSink{}, store, now)

	if err := sched.SendKill(context.Background(), 1); err != nil {
		t.Fatalf("SendKill: %v", err)
	}
	updated := store.get(1)
	if updated.TxCount != 1 {
		t.Fatalf("TxCount = %d, want unchanged 1", updated.TxCount)
	}
	if updated.NextSendAt == nil || !updated.NextSendAt.Equal(next) {
		t.Fatalf("NextSendAt = %v, want unchanged %v", updated.NextSendAt, next)
	}
}

func TestSendKill_SkipsDedupLikeManualSend(t *testing.T) {
	now := time.Now()
	target := mkTarget(1, beacon.SendPathRF)
	store := newFakeStore(target)
	sink := &fakeSink{}
	sched := newTestScheduler(t, sink, &fakeISSink{}, store, now)

	if err := sched.SendKill(context.Background(), 1); err != nil {
		t.Fatalf("SendKill: %v", err)
	}
	if !sink.frames[0].src.SkipDedup {
		t.Error("SendKill submitted with SkipDedup=false, want true")
	}
}
