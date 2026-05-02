package ax25conn

import (
	"context"
	"testing"
)

// inAwaitingRelease drives a session to AWAITING_RELEASE via Connect+UA+Disconnect.
func inAwaitingRelease(t *testing.T, sink *captureSink, opts ...func(*SessionConfig)) *Session {
	t.Helper()
	s := connected(t, sink, opts...)
	s.handle(context.Background(), Event{Kind: EventDisconnect})
	if s.state != StateAwaitingRelease {
		t.Fatalf("setup: state=%v want AWAITING_RELEASE", s.state)
	}
	sink.frames = sink.frames[:0]
	sink.chans = sink.chans[:0]
	sink.srcs = sink.srcs[:0]
	return s
}

func TestAwaitingRelease_RxSABMRespondsDM(t *testing.T) {
	sink := newCaptureSink()
	s := inAwaitingRelease(t, sink)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameSABM, PF: true},
		IsCommand: true,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.state != StateAwaitingRelease {
		t.Fatalf("must stay AWAITING_RELEASE; state=%v", s.state)
	}
	if sink.count() != 1 || sink.frames[0].ConnectedControl[0] != 0x1F {
		t.Fatalf("expected DM(F=1) reply; got %d frames", sink.count())
	}
}

func TestAwaitingRelease_RxDISCRepliesUAAndDisconnects(t *testing.T) {
	sink := newCaptureSink()
	s := inAwaitingRelease(t, sink)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameDISC, PF: true},
		IsCommand: true,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.state != StateDisconnected {
		t.Fatalf("DISC must disconnect; state=%v", s.state)
	}
	if sink.count() != 1 || sink.frames[0].ConnectedControl[0] != 0x73 {
		t.Fatalf("expected UA(F=1); got 0x%02x", sink.frames[0].ConnectedControl[0])
	}
}

func TestAwaitingRelease_RxUAFinalDisconnects(t *testing.T) {
	sink := newCaptureSink()
	s := inAwaitingRelease(t, sink)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control: Control{Kind: FrameUA, PF: true},
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.state != StateDisconnected {
		t.Fatalf("UA(F=1) must disconnect; state=%v", s.state)
	}
}

func TestAwaitingRelease_RxUANotFinalIgnored(t *testing.T) {
	sink := newCaptureSink()
	s := inAwaitingRelease(t, sink)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control: Control{Kind: FrameUA, PF: false},
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.state != StateAwaitingRelease {
		t.Fatalf("UA F=0 must be ignored; state=%v", s.state)
	}
}

func TestAwaitingRelease_RxDMFinalDisconnects(t *testing.T) {
	sink := newCaptureSink()
	s := inAwaitingRelease(t, sink)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control: Control{Kind: FrameDM, PF: true},
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.state != StateDisconnected {
		t.Fatalf("DM(F=1) must disconnect; state=%v", s.state)
	}
}

func TestAwaitingRelease_RxICmdPollEmitsDM(t *testing.T) {
	sink := newCaptureSink()
	s := inAwaitingRelease(t, sink)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameI, NS: 0, NR: 0, PF: true},
		PID:       0xF0, Info: []byte("late"),
		IsCommand: true,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if sink.count() != 1 || sink.frames[0].ConnectedControl[0] != 0x1F {
		t.Fatalf("expected DM(F=1) discouragement; got %d", sink.count())
	}
}

func TestAwaitingRelease_RxIPollZeroDropped(t *testing.T) {
	sink := newCaptureSink()
	s := inAwaitingRelease(t, sink)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameI, NS: 0, NR: 0, PF: false},
		PID:       0xF0, Info: []byte("late"),
		IsCommand: true,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if sink.count() != 0 {
		t.Fatalf("P=0 I-frame must drop silently; got %d", sink.count())
	}
}

func TestAwaitingRelease_T1RetransmitsDISCUntilN2(t *testing.T) {
	sink := newCaptureSink()
	s := inAwaitingRelease(t, sink, func(c *SessionConfig) { c.N2 = 3 })
	for i := 0; i < 3; i++ {
		s.handle(context.Background(), Event{Kind: EventT1Expiry})
		if s.state != StateAwaitingRelease {
			t.Fatalf("retry %d: state=%v want AWAITING_RELEASE", i, s.state)
		}
	}
	// Fourth → cap → DISC + disconnect.
	s.handle(context.Background(), Event{Kind: EventT1Expiry})
	if s.state != StateDisconnected {
		t.Fatalf("N2 cap must disconnect; state=%v", s.state)
	}
	last := sink.frames[len(sink.frames)-1]
	if last.ConnectedControl[0] != 0x53 { // DISC P=1
		t.Fatalf("expected DISC(P=1) on cap; got 0x%02x", last.ConnectedControl[0])
	}
}
