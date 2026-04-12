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
// position packet the filter engine accepts under the prefix rule
// installed by newTestIgate.
const gateableLine = "W5ABC-7>APRS,WIDE1-1:!3725.00N/12158.00W>hi"

func newTestIgate(t *testing.T, gov txgovernor.TxSink) *Igate {
	t.Helper()
	ig, err := New(Config{
		Server:   "127.0.0.1:1",
		Callsign: "N0CALL",
		Rules: []filters.Rule{
			{ID: 1, Type: filters.TypePrefix, Pattern: "W5", Action: filters.Allow},
		},
		Governor: gov,
	})
	if err != nil {
		t.Fatal(err)
	}
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
	// frames with distinct sources so dedup does not swallow them.
	// The first 64 fit in the buffer; the 65th must be counted as
	// a fan-out drop.
	for i := 0; i < 65; i++ {
		line := makeGateableLine(byte('A' + i/26), byte('A'+i%26))
		ig.handleISLine(line)
	}

	if got := counterValue(t, ig.mFanoutDropped); got < 1 {
		t.Fatalf("fanout dropped counter = %v, want >=1", got)
	}
}

// makeGateableLine builds a TNC2 line whose source varies so dedup
// does not merge successive calls in TestHandleISLineFanoutDropCounted.
func makeGateableLine(a, b byte) string {
	return "W5" + string([]byte{a, b}) + ">APRS,WIDE1-1:!3725.00N/12158.00W>hi"
}

// TestIsToRfHookCalledOnFilterAllow verifies that IsToRfHook fires for
// packets that pass the filter, and receives the decoded packet and
// original line.
func TestIsToRfHookCalledOnFilterAllow(t *testing.T) {
	var hookCalls int32
	var gotLine string
	ig, err := New(Config{
		Server:   "127.0.0.1:1",
		Callsign: "N0CALL",
		Rules: []filters.Rule{
			{ID: 1, Type: filters.TypePrefix, Pattern: "W5", Action: filters.Allow},
		},
		Governor: &stubGovernor{
			fn: func(ctx context.Context, channel uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error {
				return nil
			},
		},
		IsToRfHook: func(pkt *aprs.DecodedAPRSPacket, line string) {
			atomic.AddInt32(&hookCalls, 1)
			gotLine = line
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ig.handleISLine(gateableLine)

	if got := atomic.LoadInt32(&hookCalls); got != 1 {
		t.Fatalf("IsToRfHook calls = %d, want 1", got)
	}
	if gotLine != gateableLine {
		t.Fatalf("IsToRfHook line = %q, want %q", gotLine, gateableLine)
	}
}

// TestIsToRfHookNotCalledOnFilterReject verifies that IsToRfHook does
// not fire when the filter rejects the packet.
func TestIsToRfHookNotCalledOnFilterReject(t *testing.T) {
	var hookCalls int32
	ig, err := New(Config{
		Server:   "127.0.0.1:1",
		Callsign: "N0CALL",
		Rules: []filters.Rule{
			{ID: 1, Type: filters.TypePrefix, Pattern: "W5", Action: filters.Allow},
		},
		IsToRfHook: func(pkt *aprs.DecodedAPRSPacket, line string) {
			atomic.AddInt32(&hookCalls, 1)
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Send a line whose source does not match the W5 prefix rule.
	ig.handleISLine("K0ABC>APRS,WIDE1-1:!3725.00N/12158.00W>hi")

	if got := atomic.LoadInt32(&hookCalls); got != 0 {
		t.Fatalf("IsToRfHook calls = %d, want 0 (filter rejected)", got)
	}
}

// TestIsToRfHookFiresWithoutGovernor verifies that IsToRfHook fires
// even when Governor is nil (IS->RF gating disabled).
func TestIsToRfHookFiresWithoutGovernor(t *testing.T) {
	var hookCalls int32
	ig, err := New(Config{
		Server:   "127.0.0.1:1",
		Callsign: "N0CALL",
		Rules: []filters.Rule{
			{ID: 1, Type: filters.TypePrefix, Pattern: "W5", Action: filters.Allow},
		},
		Governor: nil, // IS->RF gating disabled
		IsToRfHook: func(pkt *aprs.DecodedAPRSPacket, line string) {
			atomic.AddInt32(&hookCalls, 1)
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ig.handleISLine(gateableLine)

	if got := atomic.LoadInt32(&hookCalls); got != 1 {
		t.Fatalf("IsToRfHook calls = %d, want 1 (must fire even without Governor)", got)
	}
}
