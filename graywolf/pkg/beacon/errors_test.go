package beacon

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// errorObs implements both Observer and ErrorObserver so we can
// assert that the scheduler fires the right hooks for each failure
// mode. OnBeaconSent is still satisfied so "normal" paths in other
// tests are unaffected.
type errorObs struct {
	sent          atomic.Int64
	encodeErrs    atomic.Int64
	submitQueue   atomic.Int64
	submitTimeout atomic.Int64
	submitOther   atomic.Int64

	mu             sync.Mutex
	lastBeaconName string
}

func (o *errorObs) OnBeaconSent(_ Type)                         { o.sent.Add(1) }
func (o *errorObs) OnSmartBeaconRate(_ uint32, _ time.Duration) {}

func (o *errorObs) OnEncodeError(beaconName string) {
	o.encodeErrs.Add(1)
	o.mu.Lock()
	o.lastBeaconName = beaconName
	o.mu.Unlock()
}

func (o *errorObs) OnSubmitError(beaconName string, reason string) {
	switch reason {
	case "queue_full":
		o.submitQueue.Add(1)
	case "timeout":
		o.submitTimeout.Add(1)
	default:
		o.submitOther.Add(1)
	}
	o.mu.Lock()
	o.lastBeaconName = beaconName
	o.mu.Unlock()
}

// erroringSink always returns a specific error from Submit. Used to
// drive the scheduler's submit-error path deterministically without
// having to provoke an actual governor queue-full.
type erroringSink struct{ err error }

func (s *erroringSink) Submit(_ context.Context, _ uint32, _ *ax25.Frame, _ txgovernor.SubmitSource) error {
	return s.err
}

// TestSendBeacon_SubmitErrorClassifiedQueueFull drives a beacon through
// sendBeacon with a sink that returns txgovernor.ErrQueueFull. The
// scheduler must classify the error as "queue_full" and surface it
// via the ErrorObserver hook. The build/encode path must still have
// succeeded — otherwise we're testing the wrong thing.
func TestSendBeacon_SubmitErrorClassifiedQueueFull(t *testing.T) {
	obs := &errorObs{}
	sched, err := New(Options{
		Sink:     &erroringSink{err: txgovernor.ErrQueueFull},
		Logger:   slog.New(slog.NewTextHandler(logSink{}, nil)),
		Observer: obs,
	})
	if err != nil {
		t.Fatal(err)
	}
	b := Config{
		ID:          42,
		Type:        TypePosition,
		Channel:     1,
		Source:      mustAddr(t, "N0CALL-1"),
		Dest:        mustAddr(t, "APGRW"),
		Lat:         37.0,
		Lon:         -122.0,
		SymbolTable: '/',
		SymbolCode:  '-',
		Enabled:     true,
	}
	sched.sendBeacon(context.Background(), b)

	if got := obs.submitQueue.Load(); got != 1 {
		t.Errorf("submitQueue = %d, want 1", got)
	}
	if got := obs.submitTimeout.Load() + obs.submitOther.Load(); got != 0 {
		t.Errorf("wrong-reason submit error counted: timeout+other = %d, want 0", got)
	}
	if got := obs.encodeErrs.Load(); got != 0 {
		t.Errorf("encodeErrs = %d, want 0 (frame should have encoded fine)", got)
	}
	if got := obs.sent.Load(); got != 0 {
		t.Errorf("sent = %d, want 0 (submit failed)", got)
	}
	if obs.lastBeaconName != "position/42" {
		t.Errorf("beacon name = %q, want %q", obs.lastBeaconName, "position/42")
	}
}

// TestSendBeacon_SubmitErrorClassifiedTimeout verifies that a
// context.DeadlineExceeded from Submit routes to the "timeout" bucket,
// matching what happens when the governor gets wedged and the
// scheduler's per-call context deadline expires.
func TestSendBeacon_SubmitErrorClassifiedTimeout(t *testing.T) {
	obs := &errorObs{}
	sched, _ := New(Options{
		Sink:     &erroringSink{err: context.DeadlineExceeded},
		Logger:   slog.New(slog.NewTextHandler(logSink{}, nil)),
		Observer: obs,
	})
	b := Config{
		ID:          7,
		Type:        TypeIGate,
		Channel:     1,
		Source:      mustAddr(t, "N0CALL-1"),
		Dest:        mustAddr(t, "APGRW"),
		Lat:         37.0,
		Lon:         -122.0,
		SymbolTable: '/',
		SymbolCode:  '-',
		Enabled:     true,
	}
	sched.sendBeacon(context.Background(), b)

	if got := obs.submitTimeout.Load(); got != 1 {
		t.Errorf("submitTimeout = %d, want 1", got)
	}
}

// TestSendBeacon_SubmitErrorClassifiedOther verifies the catch-all
// bucket: a generic non-sentinel error must route to "other" so no
// submit failure goes uncounted even if the governor grows a new
// error kind we don't classify here.
func TestSendBeacon_SubmitErrorClassifiedOther(t *testing.T) {
	obs := &errorObs{}
	sched, _ := New(Options{
		Sink:     &erroringSink{err: errors.New("boom")},
		Logger:   slog.New(slog.NewTextHandler(logSink{}, nil)),
		Observer: obs,
	})
	b := Config{
		ID:          1,
		Type:        TypePosition,
		Channel:     1,
		Source:      mustAddr(t, "N0CALL-1"),
		Dest:        mustAddr(t, "APGRW"),
		Lat:         37.0,
		Lon:         -122.0,
		SymbolTable: '/',
		SymbolCode:  '-',
		Enabled:     true,
	}
	sched.sendBeacon(context.Background(), b)

	if got := obs.submitOther.Load(); got != 1 {
		t.Errorf("submitOther = %d, want 1", got)
	}
}

// TestClassifySubmitError covers the classification function in
// isolation so future error-sentinel additions to txgovernor have a
// clear test to update.
func TestClassifySubmitError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, "other"},
		{"ErrQueueFull", txgovernor.ErrQueueFull, "queue_full"},
		{"DeadlineExceeded", context.DeadlineExceeded, "timeout"},
		{"Canceled", context.Canceled, "timeout"},
		{"random", errors.New("something"), "other"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifySubmitError(tc.err); got != tc.want {
				t.Errorf("classifySubmitError(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// TestBeaconName covers the small helper that produces the
// "beacon_name" metric label. Object beacons prefer ObjectName;
// others fall back to "type/id".
func TestBeaconName(t *testing.T) {
	if got := beaconName(Config{Type: TypePosition, ID: 5}); got != "position/5" {
		t.Errorf("beaconName(position/5) = %q", got)
	}
	if got := beaconName(Config{Type: TypeObject, ID: 9, ObjectName: "TESTOBJ"}); got != "TESTOBJ" {
		t.Errorf("beaconName(object TESTOBJ) = %q", got)
	}
	if got := beaconName(Config{Type: TypeObject, ID: 9}); got != "object/9" {
		t.Errorf("beaconName(object no name) = %q (should fall back)", got)
	}
}
