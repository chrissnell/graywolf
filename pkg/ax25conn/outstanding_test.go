package ax25conn

import (
	"bytes"
	"context"
	"testing"
)

func TestOutstanding_ZeroOnIdleLink(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink)
	if got := s.Outstanding(); got != 0 {
		t.Fatalf("Outstanding() = %d on an idle link, want 0", got)
	}
}

func TestOutstanding_CountsUnackedIFrames(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink)
	s.handle(context.Background(), Event{Kind: EventDataTX, Data: []byte("abc")})
	if got := s.Outstanding(); got != 1 {
		t.Fatalf("Outstanding() = %d after one unacked I-frame, want 1", got)
	}
}

// Data beyond the transmit window stays in txBuf. Reporting only the
// window would under-count exactly when a client polling 'Y' most needs
// to be told to back off.
func TestOutstanding_CountsQueuedBytesBeyondTheWindow(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink)
	// Five full frames' worth against the default mod-8 window of two:
	// two go out, three wait in txBuf.
	data := bytes.Repeat([]byte("x"), 5*DefaultPaclen)
	s.handle(context.Background(), Event{Kind: EventDataTX, Data: data})
	if sink.count() != DefaultWindowMod8 {
		t.Fatalf("sent %d I-frames, want the window of %d", sink.count(), DefaultWindowMod8)
	}
	if got := s.Outstanding(); got != 5 {
		t.Fatalf("Outstanding() = %d, want 5 (2 in flight + 3 queued)", got)
	}
}

// A partial frame's worth of queued bytes still costs one frame to send.
func TestOutstanding_RoundsPartialFrameUp(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink)
	data := bytes.Repeat([]byte("x"), 2*DefaultPaclen+1)
	s.handle(context.Background(), Event{Kind: EventDataTX, Data: data})
	if got := s.Outstanding(); got != 3 {
		t.Fatalf("Outstanding() = %d, want 3 (2 in flight + 1 partial queued)", got)
	}
}

func TestOutstanding_DropsOnAck(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink)
	s.handle(context.Background(), Event{Kind: EventDataTX, Data: []byte("abc")})
	if got := s.Outstanding(); got != 1 {
		t.Fatalf("setup: Outstanding() = %d, want 1", got)
	}
	// Peer acknowledges through N(S)=0 by asking for N(R)=1.
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: &Frame{
		Source:  s.cfg.Peer,
		Dest:    s.cfg.Local,
		Control: Control{Kind: FrameRR, NR: 1},
	}})
	if got := s.Outstanding(); got != 0 {
		t.Fatalf("Outstanding() = %d after the frame was acked, want 0", got)
	}
}
