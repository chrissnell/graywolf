package ax25conn

import (
	"context"
	"testing"
)

// connected drives a session through Connect+UA so it lands in
// StateConnected with sequence vars zeroed.
func connected(t *testing.T, sink *captureSink, opts ...func(*SessionConfig)) *Session {
	t.Helper()
	allOpts := append([]func(*SessionConfig){func(c *SessionConfig) { c.TxSink = sink }}, opts...)
	s := newTestSession(t, allOpts...)
	s.handle(context.Background(), Event{Kind: EventConnect})
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control: Control{Kind: FrameUA, PF: true},
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.state != StateConnected {
		t.Fatalf("setup: state=%v want CONNECTED", s.state)
	}
	sink.frames = sink.frames[:0]
	sink.chans = sink.chans[:0]
	sink.srcs = sink.srcs[:0]
	return s
}

func TestConnected_DataTXSendsIFrame(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink)
	s.handle(context.Background(), Event{Kind: EventDataTX, Data: []byte("abc")})
	if sink.count() != 1 {
		t.Fatalf("expected 1 I-frame, got %d", sink.count())
	}
	got := sink.frames[0]
	if !got.IsConnectedMode() || got.PID != 0xF0 {
		t.Fatalf("frame=%+v", got)
	}
	// I-frame mod-8: NS=0, NR=0, P=0 → 0x00
	if got.ConnectedControl[0] != 0x00 {
		t.Fatalf("expected I NS=0 NR=0 P=0 (0x00), got 0x%02x", got.ConnectedControl[0])
	}
	if string(got.Info) != "abc" {
		t.Fatalf("info=%q", got.Info)
	}
	if s.v.VS != 1 {
		t.Fatalf("VS=%d want 1", s.v.VS)
	}
	if !s.t1.running() {
		t.Fatal("T1 must run while I-frames outstanding")
	}
}

func TestConnected_DataTXSplitsByPaclen(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink, func(c *SessionConfig) { c.Paclen = 4 })
	s.handle(context.Background(), Event{Kind: EventDataTX, Data: []byte("abcdefg")})
	// Window=2 (mod-8 default). First two frames go out: "abcd" + "efg".
	if sink.count() != 2 {
		t.Fatalf("expected 2 I-frames, got %d", sink.count())
	}
	if string(sink.frames[0].Info) != "abcd" || string(sink.frames[1].Info) != "efg" {
		t.Fatalf("payloads: %q %q", sink.frames[0].Info, sink.frames[1].Info)
	}
}

func TestConnected_WindowFullStopsKick(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink, func(c *SessionConfig) {
		c.Paclen = 4
		c.Window = 2
	})
	s.handle(context.Background(), Event{Kind: EventDataTX, Data: []byte("aaaabbbbcccc")})
	if sink.count() != 2 {
		t.Fatalf("window=2 must clamp to 2 frames, got %d", sink.count())
	}
	if len(s.txBuf) != 4 || string(s.txBuf) != "cccc" {
		t.Fatalf("residual txBuf=%q", s.txBuf)
	}
}

func TestConnected_RxIFrameInSequenceDelivers(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink)
	emitted := make([]OutEvent, 0, 1)
	s.cfg.Observer = func(e OutEvent) { emitted = append(emitted, e) }

	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameI, NS: 0, NR: 0, PF: false},
		PID:       0xF0,
		Info:      []byte("hi"),
		IsCommand: true,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.v.VR != 1 {
		t.Fatalf("VR=%d want 1", s.v.VR)
	}
	if !s.v.Cond.Has(CondACKPending) {
		t.Fatal("CondACKPending must be set without poll bit")
	}
	if !s.t2.running() {
		t.Fatal("T2 must arm to coalesce ack")
	}
	if sink.count() != 0 {
		t.Fatalf("must not emit RR yet (P=0); got %d", sink.count())
	}
	var sawData bool
	for _, e := range emitted {
		if e.Kind == OutDataRX && string(e.Data) == "hi" {
			sawData = true
		}
	}
	if !sawData {
		t.Fatalf("expected OutDataRX with payload; got %+v", emitted)
	}
}

func TestConnected_RxIFrameWithPollEmitsRRImmediately(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameI, NS: 0, NR: 0, PF: true},
		PID:       0xF0,
		Info:      []byte("hi"),
		IsCommand: true,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if sink.count() != 1 {
		t.Fatalf("expected RR(F=1), got %d", sink.count())
	}
	got := sink.frames[0].ConnectedControl[0]
	// RR rsp NR=1 F=1 → 0x20 | 0x10 | 0x01 = 0x31
	if got != 0x31 {
		t.Fatalf("expected RR(F=1) NR=1 (0x31), got 0x%02x", got)
	}
	if s.v.Cond.Has(CondACKPending) {
		t.Fatal("CondACKPending must clear after enquiry response")
	}
}

func TestConnected_RxIFrameOutOfSequenceSendsREJ(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameI, NS: 1, NR: 0, PF: false},
		PID:       0xF0,
		Info:      []byte("oos"),
		IsCommand: true,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if !s.v.Cond.Has(CondReject) {
		t.Fatal("CondReject must set on out-of-seq")
	}
	if sink.count() != 1 {
		t.Fatalf("expected REJ, got %d", sink.count())
	}
	got := sink.frames[0].ConnectedControl[0]
	// REJ rsp NR=0 F=0 → 0x09
	if got != 0x09 {
		t.Fatalf("expected REJ(F=0) NR=0 (0x09), got 0x%02x", got)
	}
}

func TestConnected_RxIFrameOutOfSequenceWithRejectAlreadyDrops(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink)
	s.v.Cond.Set(CondReject)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameI, NS: 1, NR: 0, PF: false},
		PID:       0xF0,
		Info:      []byte("oos2"),
		IsCommand: true,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if sink.count() != 0 {
		t.Fatalf("CondReject set: must not emit another REJ; got %d", sink.count())
	}
}

func TestConnected_RxIFrameInvalidNRReestablishes(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink)
	s.v.VS = 2
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameI, NS: 0, NR: 5, PF: false},
		PID:       0xF0,
		Info:      []byte("x"),
		IsCommand: true,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.state != StateAwaitingConnection {
		t.Fatalf("invalid N(R) must re-establish; state=%v", s.state)
	}
}

func TestConnected_RxRRAdvancesVAAndStartsT3(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink, func(c *SessionConfig) { c.Paclen = 2; c.Window = 2 })
	s.handle(context.Background(), Event{Kind: EventDataTX, Data: []byte("abcd")})
	if s.v.VS != 2 {
		t.Fatalf("setup: VS=%d", s.v.VS)
	}
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameRR, NR: 2, PF: false},
		IsCommand: false,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.v.VA != 2 {
		t.Fatalf("VA=%d want 2", s.v.VA)
	}
	if s.t1.running() {
		t.Fatal("T1 must stop when all I-frames acked")
	}
	if !s.t3.running() {
		t.Fatal("T3 must start when fully acked")
	}
}

func TestConnected_RxRRCmdPollEmitsEnquiryResponse(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink)
	s.v.VR = 3
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameRR, NR: 0, PF: true},
		IsCommand: true,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if sink.count() != 1 {
		t.Fatalf("expected enquiry response, got %d", sink.count())
	}
	// RR rsp NR=3 F=1 → 0x71
	if sink.frames[0].ConnectedControl[0] != 0x71 {
		t.Fatalf("expected 0x71, got 0x%02x", sink.frames[0].ConnectedControl[0])
	}
}

func TestConnected_RxRNRSetsPeerBusyAndSuspendsKick(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameRNR, NR: 0, PF: false},
		IsCommand: false,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if !s.v.Cond.Has(CondPeerRxBusy) {
		t.Fatal("CondPeerRxBusy must set on RNR")
	}
	s.handle(context.Background(), Event{Kind: EventDataTX, Data: []byte("nope")})
	if sink.count() != 0 {
		t.Fatalf("kick suspended by peer-busy; got %d frames", sink.count())
	}
	if len(s.txBuf) != 4 {
		t.Fatalf("txBuf must hold queued bytes: %q", s.txBuf)
	}
}

func TestConnected_RxRRClearsPeerBusy(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink)
	s.v.Cond.Set(CondPeerRxBusy)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameRR, NR: 0, PF: false},
		IsCommand: false,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.v.Cond.Has(CondPeerRxBusy) {
		t.Fatal("RR must clear CondPeerRxBusy")
	}
}

func TestConnected_RxREJRequeuesOutstanding(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink, func(c *SessionConfig) { c.Paclen = 2; c.Window = 3 })
	s.handle(context.Background(), Event{Kind: EventDataTX, Data: []byte("ababab")})
	if sink.count() != 3 {
		t.Fatalf("setup: expected 3 I-frames, got %d", sink.count())
	}
	sink.frames = sink.frames[:0]
	sink.chans = sink.chans[:0]
	sink.srcs = sink.srcs[:0]
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameREJ, NR: 1, PF: false},
		IsCommand: false,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.v.VA != 1 {
		t.Fatalf("REJ NR=1 must ack frame 0: VA=%d", s.v.VA)
	}
	// kick must re-send NS=1 and NS=2 with fresh sequence numbers.
	if sink.count() != 2 {
		t.Fatalf("expected 2 retransmits, got %d", sink.count())
	}
	if string(sink.frames[0].Info) != "ab" || string(sink.frames[1].Info) != "ab" {
		t.Fatalf("retransmit payloads: %q %q", sink.frames[0].Info, sink.frames[1].Info)
	}
}

func TestConnected_RxSABMRebindsInPlace(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink, func(c *SessionConfig) { c.Paclen = 2 })
	s.handle(context.Background(), Event{Kind: EventDataTX, Data: []byte("abcd")})
	preFrames := sink.count()
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameSABM, PF: true},
		IsCommand: true,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.state != StateConnected {
		t.Fatalf("SABM rebind must keep state=CONNECTED, got %v", s.state)
	}
	if s.v.VS != 2 || s.v.VR != 0 || s.v.VA != 0 {
		t.Fatalf("rebind must reset VS=VR=VA=0 and re-kick: %+v", s.v)
	}
	// kick re-sends pending I-frames; total frames sent should be preFrames + UA + 2 retries.
	if sink.count() < preFrames+3 {
		t.Fatalf("expected UA + retransmits after rebind, got %d frames total", sink.count())
	}
}

func TestConnected_RxDISCDisconnects(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink)
	emits := make([]OutEvent, 0, 2)
	s.cfg.Observer = func(e OutEvent) { emits = append(emits, e) }
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameDISC, PF: true},
		IsCommand: true,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.state != StateDisconnected {
		t.Fatalf("DISC must disconnect; state=%v", s.state)
	}
	if sink.count() != 1 {
		t.Fatalf("expected UA reply, got %d", sink.count())
	}
	if sink.frames[0].ConnectedControl[0] != 0x73 { // UA F=1
		t.Fatalf("expected UA(F=1), got 0x%02x", sink.frames[0].ConnectedControl[0])
	}
	var sawErr bool
	for _, e := range emits {
		if e.Kind == OutError && e.ErrCode == "peer-disconnected" {
			sawErr = true
		}
	}
	if !sawErr {
		t.Fatalf("expected peer-disconnected emit; got %+v", emits)
	}
}

func TestConnected_RxDMResetsLink(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink)
	emits := make([]OutEvent, 0)
	s.cfg.Observer = func(e OutEvent) { emits = append(emits, e) }
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control: Control{Kind: FrameDM, PF: true},
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.state != StateDisconnected {
		t.Fatalf("DM must disconnect; state=%v", s.state)
	}
	var sawErr bool
	for _, e := range emits {
		if e.Kind == OutError && e.ErrCode == "peer-reset" {
			sawErr = true
		}
	}
	if !sawErr {
		t.Fatalf("expected peer-reset emit")
	}
}

func TestConnected_RxFRMRReestablishes(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control: Control{Kind: FrameFRMR, PF: false},
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.state != StateAwaitingConnection {
		t.Fatalf("FRMR must trigger establishDataLink; state=%v", s.state)
	}
}

func TestConnected_T1ExpiryEntersTimerRecovery(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink)
	s.handle(context.Background(), Event{Kind: EventT1Expiry})
	if s.state != StateTimerRecovery {
		t.Fatalf("T1 expiry must enter TIMER_RECOVERY, got %v", s.state)
	}
	if s.v.N2Count != 1 {
		t.Fatalf("n2count must seed 1 from T1, got %d", s.v.N2Count)
	}
	if sink.count() != 1 {
		t.Fatalf("expected enquiry, got %d", sink.count())
	}
	// RR cmd NR=0 P=1 → 0x11
	if sink.frames[0].ConnectedControl[0] != 0x11 {
		t.Fatalf("expected RR(P=1) cmd, got 0x%02x", sink.frames[0].ConnectedControl[0])
	}
}

func TestConnected_T3ExpiryEntersTimerRecoveryWithFreshN2(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink)
	s.handle(context.Background(), Event{Kind: EventT3Expiry})
	if s.state != StateTimerRecovery {
		t.Fatalf("T3 expiry must enter TIMER_RECOVERY, got %v", s.state)
	}
	if s.v.N2Count != 0 {
		t.Fatalf("n2count must seed 0 from T3, got %d", s.v.N2Count)
	}
}

func TestConnected_T2ExpiryFlushesACKPending(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink)
	s.v.Cond.Set(CondACKPending)
	s.v.VR = 1
	s.handle(context.Background(), Event{Kind: EventT2Expiry})
	if s.v.Cond.Has(CondACKPending) {
		t.Fatal("T2 must clear CondACKPending")
	}
	if sink.count() != 1 {
		t.Fatalf("expected RR(F=0,rsp), got %d", sink.count())
	}
	// RR rsp NR=1 F=0 → 0x21
	if sink.frames[0].ConnectedControl[0] != 0x21 {
		t.Fatalf("expected RR(F=0,rsp) NR=1, got 0x%02x", sink.frames[0].ConnectedControl[0])
	}
}

func TestConnected_T2ExpiryNoOpWhenNotPending(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink)
	s.handle(context.Background(), Event{Kind: EventT2Expiry})
	if sink.count() != 0 {
		t.Fatalf("T2 with no ACK_PENDING must be silent; got %d", sink.count())
	}
}

func TestConnected_DisconnectEntersAwaitingRelease(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink)
	s.handle(context.Background(), Event{Kind: EventDataTX, Data: []byte("xx")})
	sink.frames = sink.frames[:0]

	s.handle(context.Background(), Event{Kind: EventDisconnect})
	if s.state != StateAwaitingRelease {
		t.Fatalf("EventDisconnect must transition to AWAITING_RELEASE; got %v", s.state)
	}
	if len(s.txBuf) != 0 {
		t.Fatalf("queues must drop on disconnect, got %q", s.txBuf)
	}
	if sink.count() != 1 || sink.frames[0].ConnectedControl[0] != 0x53 {
		t.Fatalf("expected DISC(P=1) 0x53, got %d frames first 0x%02x",
			sink.count(), sink.frames[0].ConnectedControl[0])
	}
	if !s.t1.running() {
		t.Fatal("T1 must restart for DISC retry")
	}
}

func TestConnected_KickResumesAfterPeerBusyClears(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink, func(c *SessionConfig) { c.Paclen = 4 })
	s.v.Cond.Set(CondPeerRxBusy)
	s.handle(context.Background(), Event{Kind: EventDataTX, Data: []byte("hold")})
	if sink.count() != 0 {
		t.Fatal("must not send while peer-busy")
	}
	rr := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control: Control{Kind: FrameRR, NR: 0, PF: false},
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: rr})
	if sink.count() != 1 {
		t.Fatalf("after RR clearing busy, kick must drain txBuf: got %d", sink.count())
	}
}

func TestValidNR_Range(t *testing.T) {
	s := newTestSession(t)
	s.v.VA = 3
	s.v.VS = 6
	cases := []struct {
		nr   uint8
		want bool
	}{
		{3, true},
		{4, true},
		{5, true},
		{6, true},
		{7, false},
		{2, false},
	}
	for _, c := range cases {
		if got := s.validNR(c.nr); got != c.want {
			t.Errorf("validNR(%d) = %v, want %v (VA=%d VS=%d)", c.nr, got, c.want, s.v.VA, s.v.VS)
		}
	}
}

func TestValidNR_WrapAround(t *testing.T) {
	s := newTestSession(t)
	s.v.VA = 6
	s.v.VS = 2 // wrapped 6,7,0,1,2
	cases := []struct {
		nr   uint8
		want bool
	}{
		{6, true}, {7, true}, {0, true}, {1, true}, {2, true},
		{3, false}, {5, false},
	}
	for _, c := range cases {
		if got := s.validNR(c.nr); got != c.want {
			t.Errorf("validNR(%d) = %v, want %v (VA=%d VS=%d)", c.nr, got, c.want, s.v.VA, s.v.VS)
		}
	}
}
