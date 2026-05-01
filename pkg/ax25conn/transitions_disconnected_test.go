package ax25conn

import (
	"context"
	"testing"
)

func TestDisconnected_ConnectSendsSABMAndTransitions(t *testing.T) {
	sink := newCaptureSink()
	s := newTestSession(t, func(c *SessionConfig) { c.TxSink = sink })
	s.handle(context.Background(), Event{Kind: EventConnect})
	if got := s.state; got != StateAwaitingConnection {
		t.Fatalf("state=%v want AWAITING_CONNECTION", got)
	}
	if sink.count() != 1 {
		t.Fatalf("expected 1 frame submitted, got %d", sink.count())
	}
	got := sink.frames[0]
	if !got.IsConnectedMode() {
		t.Fatal("expected connected-mode frame")
	}
	if got.ConnectedControl[0] != 0x3F { // SABM cmd, P=1
		t.Fatalf("expected SABM(P=1) byte 0x3F, got 0x%02x", got.ConnectedControl[0])
	}
	if !got.CommandResp {
		t.Fatal("SABM must be command-polarity")
	}
	if !s.t1.running() {
		t.Fatal("T1 must be running after Connect")
	}
	if s.v.T1Started.IsZero() {
		t.Fatal("T1Started must be stamped")
	}
	if sink.srcs[0].Priority == 0 || !sink.srcs[0].SkipDedup {
		t.Fatalf("submit source: %+v", sink.srcs[0])
	}
}

func TestDisconnected_ConnectMod128SendsSABME(t *testing.T) {
	sink := newCaptureSink()
	s := newTestSession(t, func(c *SessionConfig) {
		c.TxSink = sink
		c.Mod128 = true
	})
	s.handle(context.Background(), Event{Kind: EventConnect})
	got := sink.frames[0]
	if got.ConnectedControl[0] != 0x7F { // SABME cmd, P=1
		t.Fatalf("expected SABME(P=1) byte 0x7F, got 0x%02x", got.ConnectedControl[0])
	}
}

func TestDisconnected_InboundSABMRespondsDM(t *testing.T) {
	sink := newCaptureSink()
	s := newTestSession(t, func(c *SessionConfig) { c.TxSink = sink })
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameSABM, PF: true},
		IsCommand: true,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if got := s.state; got != StateDisconnected {
		t.Fatalf("must remain DISCONNECTED, got %v", got)
	}
	if sink.count() != 1 {
		t.Fatalf("expected 1 DM, got %d frames", sink.count())
	}
	got := sink.frames[0]
	// DM = 0x0F | F-bit (peer's P=1) → 0x1F.
	if got.ConnectedControl[0] != 0x1F {
		t.Fatalf("expected DM(F=1) 0x1F, got 0x%02x", got.ConnectedControl[0])
	}
	if got.CommandResp {
		t.Fatal("DM must be response-polarity")
	}
}

func TestDisconnected_InboundSABMResponsePolarityDropped(t *testing.T) {
	// Malformed C-bit polarity: SABM-as-response. Drop silently.
	sink := newCaptureSink()
	s := newTestSession(t, func(c *SessionConfig) { c.TxSink = sink })
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameSABM, PF: true},
		IsCommand: false,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if sink.count() != 0 {
		t.Fatalf("malformed SABM must be dropped silently; got %d frames", sink.count())
	}
}

func TestDisconnected_DropsDM(t *testing.T) {
	sink := newCaptureSink()
	s := newTestSession(t, func(c *SessionConfig) { c.TxSink = sink })
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameDM, PF: true},
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if sink.count() != 0 {
		t.Fatal("must never reply to a DM in DISCONNECTED")
	}
	if s.state != StateDisconnected {
		t.Fatalf("must stay DISCONNECTED, got %v", s.state)
	}
}

func TestDisconnected_NilFrameNoOp(t *testing.T) {
	sink := newCaptureSink()
	s := newTestSession(t, func(c *SessionConfig) { c.TxSink = sink })
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: nil})
	if sink.count() != 0 {
		t.Fatal("nil frame must not emit")
	}
}

func TestDisconnected_ShutdownExits(t *testing.T) {
	s := newTestSession(t)
	if cont := s.handle(context.Background(), Event{Kind: EventShutdown}); cont {
		t.Fatal("EventShutdown must return false")
	}
}

func TestSubmit_ToAX25FramePopulatesConnectedControl(t *testing.T) {
	sink := newCaptureSink()
	s := newTestSession(t, func(c *SessionConfig) { c.TxSink = sink })
	s.sendDM(true)
	if sink.count() != 1 {
		t.Fatalf("expected one frame, got %d", sink.count())
	}
	f := sink.frames[0]
	if !f.IsConnectedMode() {
		t.Fatal("connected-mode flag must be set")
	}
	if len(f.ConnectedControl) != 1 || f.ConnectedControl[0] != 0x1F {
		t.Fatalf("ConnectedControl=% x", f.ConnectedControl)
	}
	if f.ConnectedHasInfo {
		t.Fatal("DM has no info field")
	}
	if len(f.Info) != 0 || f.PID != 0 {
		t.Fatalf("DM should have no PID/Info: PID=0x%02x Info=% x", f.PID, f.Info)
	}
}

func TestSubmit_IFrameCarriesPIDAndInfo(t *testing.T) {
	sink := newCaptureSink()
	s := newTestSession(t, func(c *SessionConfig) { c.TxSink = sink })
	f := &Frame{
		Source: s.cfg.Local, Dest: s.cfg.Peer,
		Control:   Control{Kind: FrameI, NS: 0, NR: 0},
		PID:       0xF0,
		Info:      []byte("hello"),
		IsCommand: true,
	}
	s.submit(f)
	if sink.count() != 1 {
		t.Fatal("submit must enqueue exactly one frame")
	}
	wrap := sink.frames[0]
	if !wrap.ConnectedHasInfo {
		t.Fatal("I-frame must carry info flag")
	}
	if wrap.PID != 0xF0 {
		t.Fatalf("PID drifted: 0x%02x", wrap.PID)
	}
	if string(wrap.Info) != "hello" {
		t.Fatalf("info drift: %q", wrap.Info)
	}
	// Encoded wire bytes must round-trip through ax25conn.Decode.
	raw, err := wrap.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	dec, err := Decode(raw, false)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dec.Control.Kind != FrameI || dec.Control.NS != 0 || string(dec.Info) != "hello" {
		t.Fatalf("round-trip drift: %+v", dec)
	}
}
