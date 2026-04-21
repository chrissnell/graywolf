package txbackend

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
	"go.uber.org/goleak"
)

// fakeBackend is a controllable Backend for dispatcher tests.
type fakeBackend struct {
	name, instance string
	channels       []uint32
	submit         func(*pb.TransmitFrame) error
	submitted      atomic.Uint64
	closed         atomic.Bool
	closeOrder     *orderLog
}

func (f *fakeBackend) Submit(_ context.Context, tf *pb.TransmitFrame) error {
	f.submitted.Add(1)
	if f.submit == nil {
		return nil
	}
	return f.submit(tf)
}
func (f *fakeBackend) Name() string               { return f.name }
func (f *fakeBackend) InstanceID() string         { return f.instance }
func (f *fakeBackend) AttachedChannels() []uint32 { return f.channels }
func (f *fakeBackend) Close(ctx context.Context) error {
	f.closed.Store(true)
	if f.closeOrder != nil {
		f.closeOrder.record(f.instance)
	}
	return nil
}

type orderLog struct {
	mu  sync.Mutex
	log []string
}

func (o *orderLog) record(s string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.log = append(o.log, s)
}
func (o *orderLog) snapshot() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]string, len(o.log))
	copy(out, o.log)
	return out
}

type fakeMetrics struct {
	mu       sync.Mutex
	perInst  []instKey
	noBack   []uint32
	frameCtr []uint32
}

type instKey struct {
	channel          uint32
	backend, outcome string
	instance         string
}

func (fm *fakeMetrics) ObserveTxBackendSubmit(ch uint32, backend, instance, outcome string, _ time.Duration) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.perInst = append(fm.perInst, instKey{ch, backend, outcome, instance})
}
func (fm *fakeMetrics) ObserveTxNoBackend(ch uint32) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.noBack = append(fm.noBack, ch)
}
func (fm *fakeMetrics) ObserveTxFrame(ch uint32) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.frameCtr = append(fm.frameCtr, ch)
}

func TestDispatcher_ModemOnly(t *testing.T) {
	defer goleak.VerifyNone(t)

	modem := &fakeBackend{name: "modem", instance: "modem", channels: []uint32{1}}
	reg := NewRegistry()
	reg.Publish(&Snapshot{
		ByChannel: map[uint32][]Backend{1: {modem}},
		CsmaSkip:  map[uint32]bool{},
	})

	m := &fakeMetrics{}
	d := New(Config{Registry: reg, Metrics: m})

	if err := d.Send(&pb.TransmitFrame{Channel: 1, FrameId: 42}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := modem.submitted.Load(); got != 1 {
		t.Fatalf("modem submitted=%d, want 1", got)
	}
	if len(m.frameCtr) != 1 || m.frameCtr[0] != 1 {
		t.Fatalf("frameCtr=%v, want [1]", m.frameCtr)
	}
	if len(m.perInst) != 1 || m.perInst[0].outcome != OutcomeOK {
		t.Fatalf("perInst=%+v, want 1 ok", m.perInst)
	}
}

func TestDispatcher_KissMultiInstance_PartialFailure(t *testing.T) {
	defer goleak.VerifyNone(t)

	k1 := &fakeBackend{name: "kiss", instance: "kiss-1", channels: []uint32{11}, submit: func(*pb.TransmitFrame) error { return nil }}
	k2 := &fakeBackend{name: "kiss", instance: "kiss-2", channels: []uint32{11}, submit: func(*pb.TransmitFrame) error { return ErrBackendBusy }}
	reg := NewRegistry()
	reg.Publish(&Snapshot{
		ByChannel: map[uint32][]Backend{11: {k1, k2}},
		CsmaSkip:  map[uint32]bool{11: true},
	})
	m := &fakeMetrics{}
	d := New(Config{Registry: reg, Metrics: m})

	// Partial failure: one accepted → nil.
	if err := d.Send(&pb.TransmitFrame{Channel: 11, FrameId: 1}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if k1.submitted.Load() != 1 || k2.submitted.Load() != 1 {
		t.Fatalf("fanout counts: k1=%d k2=%d", k1.submitted.Load(), k2.submitted.Load())
	}
	if len(m.perInst) != 2 {
		t.Fatalf("perInst=%d, want 2", len(m.perInst))
	}
	gotOutcomes := map[string]int{}
	for _, p := range m.perInst {
		gotOutcomes[p.outcome]++
	}
	if gotOutcomes[OutcomeOK] != 1 || gotOutcomes[OutcomeBackendBusy] != 1 {
		t.Fatalf("outcomes=%v, want 1 ok + 1 backend_busy", gotOutcomes)
	}

	// All fail → errors.Join returned.
	k1.submit = func(*pb.TransmitFrame) error { return ErrBackendDown }
	if err := d.Send(&pb.TransmitFrame{Channel: 11, FrameId: 2}); err == nil {
		t.Fatal("Send: want error when all backends fail")
	}
}

func TestDispatcher_NoBackend(t *testing.T) {
	defer goleak.VerifyNone(t)

	reg := NewRegistry()
	m := &fakeMetrics{}
	d := New(Config{Registry: reg, Metrics: m})

	err := d.Send(&pb.TransmitFrame{Channel: 99})
	if !errors.Is(err, ErrNoBackend) {
		t.Fatalf("err=%v, want ErrNoBackend", err)
	}
	if len(m.noBack) != 1 || m.noBack[0] != 99 {
		t.Fatalf("noBack=%v", m.noBack)
	}
	if len(m.frameCtr) != 0 {
		t.Fatalf("frameCtr=%v, want empty (no backend means no accepted submit)", m.frameCtr)
	}
}

func TestDispatcher_Stopped(t *testing.T) {
	defer goleak.VerifyNone(t)

	reg := NewRegistry()
	d := New(Config{Registry: reg})
	d.StopAccepting()
	if err := d.Send(&pb.TransmitFrame{Channel: 1}); !errors.Is(err, ErrStopped) {
		t.Fatalf("err=%v, want ErrStopped", err)
	}
}

func TestDispatcher_RaceConcurrentPublishSend(t *testing.T) {
	defer goleak.VerifyNone(t)

	b1 := &fakeBackend{name: "modem", instance: "m1", channels: []uint32{1}}
	b2 := &fakeBackend{name: "modem", instance: "m2", channels: []uint32{1}}
	reg := NewRegistry()
	reg.Publish(&Snapshot{ByChannel: map[uint32][]Backend{1: {b1}}, CsmaSkip: map[uint32]bool{}})
	d := New(Config{Registry: reg})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var pubWG sync.WaitGroup
	pubWG.Add(1)
	go func() {
		defer pubWG.Done()
		toggle := false
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			toggle = !toggle
			var snap *Snapshot
			if toggle {
				snap = &Snapshot{ByChannel: map[uint32][]Backend{1: {b1}}, CsmaSkip: map[uint32]bool{}}
			} else {
				snap = &Snapshot{ByChannel: map[uint32][]Backend{1: {b1, b2}}, CsmaSkip: map[uint32]bool{}}
			}
			reg.Publish(snap)
		}
	}()

	var sendWG sync.WaitGroup
	for i := 0; i < 8; i++ {
		sendWG.Add(1)
		go func() {
			defer sendWG.Done()
			for j := 0; j < 500; j++ {
				_ = d.Send(&pb.TransmitFrame{Channel: 1, FrameId: uint64(j)})
			}
		}()
	}
	sendWG.Wait()
	cancel()
	pubWG.Wait()
}

func TestDispatcher_Watcher_ExitsOnCtx(t *testing.T) {
	defer goleak.VerifyNone(t)

	reg := NewRegistry()
	d := New(Config{Registry: reg})
	ctx, cancel := context.WithCancel(context.Background())
	signals := make(chan struct{})
	build := func() *Snapshot {
		return newSnapshot()
	}
	d.StartWatcher(ctx, signals, build)

	// Send a signal to prove the watcher is alive.
	select {
	case signals <- struct{}{}:
	case <-time.After(time.Second):
		t.Fatal("signal send timed out")
	}

	cancel()
	d.WaitWatcher()
}

func TestBuildSnapshot(t *testing.T) {
	modem := NewModemBackend(nil, []uint32{1, 2})
	k1 := NewKissTncBackend(nil, 10, 3)
	k2 := NewKissTncBackend(nil, 11, 3) // two kiss backends on same channel

	snap := BuildSnapshot(modem, []uint32{1, 2}, []*KissTncBackend{k1, k2})

	if got := len(snap.ByChannel[1]); got != 1 {
		t.Errorf("channel 1 backends=%d, want 1 (modem only)", got)
	}
	if got := len(snap.ByChannel[2]); got != 1 {
		t.Errorf("channel 2 backends=%d, want 1 (modem only)", got)
	}
	if got := len(snap.ByChannel[3]); got != 2 {
		t.Errorf("channel 3 backends=%d, want 2 (two kiss)", got)
	}
	if snap.CsmaSkip[1] || snap.CsmaSkip[2] {
		t.Errorf("CsmaSkip should be false for modem-backed channels")
	}
	if !snap.CsmaSkip[3] {
		t.Errorf("CsmaSkip should be true for kiss-only channel 3")
	}
}

func TestBuildSnapshot_NoModem_KissOnly(t *testing.T) {
	k := NewKissTncBackend(nil, 1, 5)
	snap := BuildSnapshot(nil, nil, []*KissTncBackend{k})
	if got := len(snap.ByChannel[5]); got != 1 {
		t.Errorf("channel 5 backends=%d, want 1", got)
	}
	if !snap.CsmaSkip[5] {
		t.Errorf("CsmaSkip should be true (no modem)")
	}
}

func TestBuildSnapshot_DualForbidden_StillRenders(t *testing.T) {
	// The validator is supposed to reject this combination, but the
	// snapshot builder should still degrade gracefully if one slips
	// through (e.g. via migrating from a pre-validator release).
	modem := NewModemBackend(nil, []uint32{7})
	k := NewKissTncBackend(nil, 42, 7)
	snap := BuildSnapshot(modem, []uint32{7}, []*KissTncBackend{k})
	if got := len(snap.ByChannel[7]); got != 2 {
		t.Errorf("channel 7 backends=%d, want 2", got)
	}
	if snap.CsmaSkip[7] {
		t.Errorf("CsmaSkip should be false when modem is present")
	}
}

func TestDispatcher_ShutdownOrder(t *testing.T) {
	defer goleak.VerifyNone(t)

	log := &orderLog{}
	b1 := &fakeBackend{name: "kiss", instance: "first", channels: []uint32{1}, closeOrder: log}
	b2 := &fakeBackend{name: "kiss", instance: "second", channels: []uint32{1}, closeOrder: log}
	reg := NewRegistry()
	reg.Publish(&Snapshot{ByChannel: map[uint32][]Backend{1: {b1, b2}}, CsmaSkip: map[uint32]bool{}})
	d := New(Config{Registry: reg})

	// Send a frame to both.
	if err := d.Send(&pb.TransmitFrame{Channel: 1, FrameId: 1}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	d.StopAccepting()
	// Shutdown happens outside the dispatcher; orchestrator calls
	// Close on each backend. Verify both closed, in some order.
	if err := b1.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := b2.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	closed := log.snapshot()
	if len(closed) != 2 {
		t.Fatalf("closeOrder=%v, want 2 entries", closed)
	}
}
