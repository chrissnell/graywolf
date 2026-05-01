package igate

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/igate/filters"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	m := &dto.Metric{}
	if err := c.Write(m); err != nil {
		t.Fatalf("counter write: %v", err)
	}
	return m.GetCounter().GetValue()
}

// stubGovernor is a minimal txgovernor.TxSink whose Submit delegates
// to an embedded function, so each test can install its own behavior
// (accept, block forever, return an error).
type stubGovernor struct {
	fn func(ctx context.Context, channel uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error
}

func (s *stubGovernor) Submit(ctx context.Context, channel uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error {
	return s.fn(ctx, channel, frame, src)
}

// gateableLine is a TNC2-format APRS-IS line that will parse into a
// directed-message packet addressed to KE7XYZ. Tests that use this
// line call newTestIgate, which pre-seeds KE7XYZ in the heard-direct
// tracker so the spec gate lets the message through. The existing
// W5-prefix filter rule allows the source; together those match the
// real two-stage gating path (spec first, user filter second).
const gateableLine = "W5ABC-7>APRS,WIDE1-1::KE7XYZ   :hello{1"

func newTestIgate(t *testing.T, gov txgovernor.TxSink) *Igate {
	t.Helper()
	ig, err := New(Config{
		Server:          "127.0.0.1:1",
		StationCallsign: "KE7XYZ",
		Rules: []filters.Rule{
			{ID: 1, Type: filters.TypePrefix, Pattern: "W5", Action: filters.Allow},
		},
		Governor: gov,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The IS->RF spec gate requires the addressee to have been heard
	// directly on RF. Seed KE7XYZ so gateableLine survives the gate.
	ig.heard.Record("KE7XYZ")
	return ig
}

// setSessCtx replaces the iGate's session context, used by tests that
// need to exercise cancellation without calling Start.
func setSessCtx(ig *Igate, ctx context.Context) {
	ig.sessCtx.Store(&sessCtxHolder{ctx: ctx})
}

// TestHandleISLineSubmitHappyPath: when the governor accepts the frame,
// the gated counter increments and the drop counter stays at zero.
func TestHandleISLineSubmitHappyPath(t *testing.T) {
	var calls int32
	ig := newTestIgate(t, &stubGovernor{
		fn: func(ctx context.Context, channel uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error {
			atomic.AddInt32(&calls, 1)
			return nil
		},
	})

	ig.handleISLine(gateableLine)

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("Submit calls = %d, want 1", got)
	}
	st := ig.Status()
	if st.Downlinked != 1 {
		t.Fatalf("Downlinked = %d, want 1", st.Downlinked)
	}
	if got := counterValue(t, ig.mSubmitDropped); got != 0 {
		t.Fatalf("submit dropped counter = %v, want 0", got)
	}
}

// TestHandleISLineSubmitTimesOut: when Submit blocks forever,
// handleISLine must return within the timeout budget (igateSubmitTimeout
// = 2s) plus slack, the drop counter must increment, and the gated
// counter must stay at zero.
func TestHandleISLineSubmitTimesOut(t *testing.T) {
	block := make(chan struct{}) // never closed, no senders
	ig := newTestIgate(t, &stubGovernor{
		fn: func(ctx context.Context, channel uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error {
			select {
			case <-block:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})

	done := make(chan struct{})
	start := time.Now()
	go func() {
		ig.handleISLine(gateableLine)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("handleISLine did not return within 3s; read loop would be stalled")
	}
	elapsed := time.Since(start)
	if elapsed < igateSubmitTimeout-200*time.Millisecond {
		t.Fatalf("handleISLine returned too quickly (%s) — timeout not honored", elapsed)
	}

	st := ig.Status()
	if st.Downlinked != 0 {
		t.Fatalf("Downlinked = %d, want 0 (submit timed out)", st.Downlinked)
	}
	if got := counterValue(t, ig.mSubmitDropped); got != 1 {
		t.Fatalf("submit dropped counter = %v, want 1", got)
	}
}

// TestHandleISLineSessionCtxCancelled: when the session context is
// cancelled mid-submit, the caller must also observe a drop (not a
// silent return) and must unblock promptly.
func TestHandleISLineSessionCtxCancelled(t *testing.T) {
	block := make(chan struct{})
	ig := newTestIgate(t, &stubGovernor{
		fn: func(ctx context.Context, channel uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error {
			select {
			case <-block:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setSessCtx(ig, ctx)

	var wg sync.WaitGroup
	wg.Add(1)
	start := time.Now()
	go func() {
		defer wg.Done()
		ig.handleISLine(gateableLine)
	}()

	// Cancel the session context after a short delay. The derived
	// submit context must inherit the cancellation and unblock the
	// stub submit function immediately.
	time.AfterFunc(50*time.Millisecond, cancel)

	doneCh := make(chan struct{})
	go func() { wg.Wait(); close(doneCh) }()
	select {
	case <-doneCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("handleISLine did not return promptly after session ctx cancel")
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("handleISLine took %s after cancel; want <200ms", elapsed)
	}

	st := ig.Status()
	if st.Downlinked != 0 {
		t.Fatalf("Downlinked = %d, want 0 (submit was cancelled)", st.Downlinked)
	}
	if got := counterValue(t, ig.mSubmitDropped); got != 1 {
		t.Fatalf("submit dropped counter = %v, want 1 (cancelled path must count)", got)
	}
}

// TestHandleISLineSubmitErrorCountsDrop: a plain non-nil error from
// Submit (not context-related) must still bump the drop counter.
func TestHandleISLineSubmitErrorCountsDrop(t *testing.T) {
	ig := newTestIgate(t, &stubGovernor{
		fn: func(ctx context.Context, channel uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error {
			return errors.New("queue full")
		},
	})

	ig.handleISLine(gateableLine)

	if got := counterValue(t, ig.mSubmitDropped); got != 1 {
		t.Fatalf("submit dropped counter = %v, want 1", got)
	}
	if ig.Status().Downlinked != 0 {
		t.Fatalf("Downlinked must stay 0 on submit error")
	}
}

// TestHandleISLineFanoutDropCounted: the PacketInput fan-out drops
// frames when no consumer is draining; those drops must be counted.
func TestHandleISLineFanoutDropCounted(t *testing.T) {
	ig := newTestIgate(t, &stubGovernor{
		fn: func(ctx context.Context, channel uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error {
			return nil
		},
	})

	// inputCh has capacity 64 and no consumer. Send 65 gateable
	// message frames with distinct sources, all addressed to KE7XYZ
	// (the heard-direct recipient newTestIgate seeds). The first 64
	// fit in the buffer; the 65th must be counted as a fan-out drop.
	for i := 0; i < 65; i++ {
		line := makeGateableLine(byte('A'+i/26), byte('A'+i%26))
		ig.handleISLine(line)
	}

	if got := counterValue(t, ig.mFanoutDropped); got < 1 {
		t.Fatalf("fanout dropped counter = %v, want >=1", got)
	}
}

// makeGateableLine builds a TNC2 line whose source varies so each
// call is distinct. All frames are directed-message packets addressed
// to KE7XYZ (which newTestIgate pre-seeds as heard-direct) so they
// survive the spec gate and reach the fan-out path.
func makeGateableLine(a, b byte) string {
	return "W5" + string([]byte{a, b}) + ">APRS,WIDE1-1::KE7XYZ   :hello{1"
}

// TestIsRxHookCalledOnFilterAllow verifies that IsRxHook fires for
// packets that pass the filter, and receives the decoded packet and
// original line.
func TestIsRxHookCalledOnFilterAllow(t *testing.T) {
	var hookCalls int32
	var gotLine string
	ig, err := New(Config{
		Server:          "127.0.0.1:1",
		StationCallsign: "KE7XYZ",
		Rules: []filters.Rule{
			{ID: 1, Type: filters.TypePrefix, Pattern: "W5", Action: filters.Allow},
		},
		Governor: &stubGovernor{
			fn: func(ctx context.Context, channel uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error {
				return nil
			},
		},
		IsRxHook: func(pkt *aprs.DecodedAPRSPacket, line string) {
			atomic.AddInt32(&hookCalls, 1)
			gotLine = line
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Hooks fire before the IS->RF spec/filter gate, so we don't
	// need to seed the heard-direct tracker for this test — but
	// seed it anyway so the full pipeline runs and any regression
	// in the hook-ordering invariant is obvious.
	ig.heard.Record("KE7XYZ")

	ig.handleISLine(gateableLine)

	if got := atomic.LoadInt32(&hookCalls); got != 1 {
		t.Fatalf("IsRxHook calls = %d, want 1", got)
	}
	if gotLine != gateableLine {
		t.Fatalf("IsRxHook line = %q, want %q", gotLine, gateableLine)
	}
}

// TestIsRxHookFiresEvenWhenFilterRejects verifies that IsRxHook fires
// for every received APRS-IS packet regardless of whether the local
// IS->RF filter engine would allow it. The filter only gates RF
// transmission; map display must not be coupled to it.
func TestIsRxHookFiresEvenWhenFilterRejects(t *testing.T) {
	var hookCalls int32
	ig, err := New(Config{
		Server:          "127.0.0.1:1",
		StationCallsign: "KE7XYZ",
		Rules: []filters.Rule{
			{ID: 1, Type: filters.TypePrefix, Pattern: "W5", Action: filters.Allow},
		},
		IsRxHook: func(pkt *aprs.DecodedAPRSPacket, line string) {
			atomic.AddInt32(&hookCalls, 1)
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Send a line whose source does not match the W5 prefix rule; the
	// filter will reject it for transmission, but the hook must still
	// fire so the station reaches the map.
	ig.handleISLine("K0ABC>APRS,WIDE1-1:!3725.00N/12158.00W>hi")

	if got := atomic.LoadInt32(&hookCalls); got != 1 {
		t.Fatalf("IsRxHook calls = %d, want 1 (must fire regardless of filter)", got)
	}
	if got := atomic.LoadUint64(&ig.statFiltered); got != 1 {
		t.Fatalf("statFiltered = %d, want 1 (filter still counts the reject)", got)
	}
}

// TestIsRxHookFiresWithoutGovernor verifies that IsRxHook fires even
// when Governor is nil (IS->RF gating disabled).
func TestIsRxHookFiresWithoutGovernor(t *testing.T) {
	var hookCalls int32
	ig, err := New(Config{
		Server:          "127.0.0.1:1",
		StationCallsign: "KE7XYZ",
		Rules: []filters.Rule{
			{ID: 1, Type: filters.TypePrefix, Pattern: "W5", Action: filters.Allow},
		},
		Governor: nil, // IS->RF gating disabled
		IsRxHook: func(pkt *aprs.DecodedAPRSPacket, line string) {
			atomic.AddInt32(&hookCalls, 1)
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ig.handleISLine(gateableLine)

	if got := atomic.LoadInt32(&hookCalls); got != 1 {
		t.Fatalf("IsRxHook calls = %d, want 1 (must fire even without Governor)", got)
	}
}

// fakeChannelModeLookup satisfies configstore.ChannelModeLookup for tests.
type fakeChannelModeLookup struct{ modes map[uint32]string }

func (f *fakeChannelModeLookup) ModeForChannel(_ context.Context, id uint32) (string, error) {
	return f.modes[id], nil
}

// TestIgateSkipsPacketModeTxChannelAtRuntime verifies that when the
// iGate's TxChannel is packet-mode, handleISLine drops the packet and
// increments mSubmitDropped without calling Governor.Submit.
func TestIgateSkipsPacketModeTxChannelAtRuntime(t *testing.T) {
	t.Parallel()
	var submitCalls int32
	gov := &stubGovernor{
		fn: func(ctx context.Context, channel uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error {
			atomic.AddInt32(&submitCalls, 1)
			return nil
		},
	}
	modes := &fakeChannelModeLookup{modes: map[uint32]string{
		1: configstore.ChannelModePacket,
	}}
	ig, err := New(Config{
		Server:          "127.0.0.1:1",
		StationCallsign: "KE7XYZ",
		Rules: []filters.Rule{
			{ID: 1, Type: filters.TypePrefix, Pattern: "W5", Action: filters.Allow},
		},
		Governor:     gov,
		TxChannel:    1,
		ChannelModes: modes,
	})
	if err != nil {
		t.Fatal(err)
	}
	ig.heard.Record("KE7XYZ")

	ig.handleISLine(gateableLine)

	if got := atomic.LoadInt32(&submitCalls); got != 0 {
		t.Fatalf("Submit calls = %d, want 0 (packet-mode channel must be skipped)", got)
	}
	if got := counterValue(t, ig.mSubmitDropped); got != 1 {
		t.Fatalf("mSubmitDropped = %v, want 1", got)
	}
}
