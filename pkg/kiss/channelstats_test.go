package kiss

import (
	"io"
	"log/slog"
	"testing"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"

	"github.com/chrissnell/graywolf/pkg/app/ingress"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	return NewManager(ManagerConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

// TestManager_ChannelStats_CountsRxTx verifies the per-channel counter
// primitive: unseen channels report ok=false, RX/TX accumulate
// independently per channel, and snapshots are by value (issue #132).
func TestManager_ChannelStats_CountsRxTx(t *testing.T) {
	m := newTestManager(t)

	if _, ok := m.ChannelStats(1); ok {
		t.Fatalf("unseen channel: ok=true, want false")
	}

	m.countRx(1)
	m.countRx(1)
	m.countTx(1)
	m.countTx(1)
	m.countTx(1)
	m.countTx(2)

	got, ok := m.ChannelStats(1)
	if !ok {
		t.Fatalf("channel 1: ok=false, want true")
	}
	if got.RxFrames != 2 || got.TxFrames != 3 {
		t.Errorf("channel 1 = %+v, want {RxFrames:2 TxFrames:3}", got)
	}

	got, ok = m.ChannelStats(2)
	if !ok || got.RxFrames != 0 || got.TxFrames != 1 {
		t.Errorf("channel 2 = %+v ok=%v, want {0 1} ok=true", got, ok)
	}

	// Snapshot must be a copy: mutating it does not corrupt the store.
	got.RxFrames = 999
	if again, _ := m.ChannelStats(1); again.RxFrames != 2 {
		t.Errorf("snapshot aliased store: RxFrames=%d, want 2", again.RxFrames)
	}

	if _, ok := m.ChannelStats(9); ok {
		t.Errorf("channel 9: ok=true, want false")
	}
}

// TestManager_WrapRxIngress verifies the RX-ingress decorator counts
// against rf.Channel, still delegates to base with the same args,
// tolerates a nil frame (no count, base still called), and tolerates a
// nil base (no panic).
func TestManager_WrapRxIngress(t *testing.T) {
	m := newTestManager(t)

	var gotRF *pb.ReceivedFrame
	var gotSrc ingress.Source
	calls := 0
	base := func(rf *pb.ReceivedFrame, src ingress.Source) {
		calls++
		gotRF = rf
		gotSrc = src
	}

	wrapped := m.wrapRxIngress(base)
	src := ingress.KissTnc(7)
	rf := &pb.ReceivedFrame{Channel: 5}
	wrapped(rf, src)

	if calls != 1 || gotRF != rf || gotSrc != src {
		t.Errorf("base not called transparently: calls=%d rf=%v src=%v", calls, gotRF, gotSrc)
	}
	if st, ok := m.ChannelStats(5); !ok || st.RxFrames != 1 {
		t.Errorf("channel 5 after wrapped(rf): %+v ok=%v, want RxFrames=1", st, ok)
	}

	// nil frame: no count, base still invoked (matches server contract
	// where RxIngress may be handed a nil in defensive paths).
	wrapped(nil, src)
	if calls != 2 {
		t.Errorf("base not called for nil frame: calls=%d, want 2", calls)
	}
	if st, _ := m.ChannelStats(5); st.RxFrames != 1 {
		t.Errorf("nil frame bumped RX: RxFrames=%d, want 1", st.RxFrames)
	}

	// nil base must not panic.
	m.wrapRxIngress(nil)(&pb.ReceivedFrame{Channel: 6}, src)
	if st, ok := m.ChannelStats(6); !ok || st.RxFrames != 1 {
		t.Errorf("nil-base wrapper did not count: %+v ok=%v", st, ok)
	}
}
