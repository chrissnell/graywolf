package messages

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

func newPreflightForTest(t *testing.T) (*Preflight, *fakeTxSink, *fakeIGateSender, *fakeClock) {
	t.Helper()
	sink := &fakeTxSink{}
	igs := &fakeIGateSender{}
	clock := &fakeClock{now: time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p, err := NewPreflight(PreflightConfig{
		OurCall:        func() string { return "N0CALL" },
		TxSink:         sink,
		IGateSender:    igs,
		Clock:          clock,
		Logger:         logger,
		AutoAckChannel: 1,
	})
	if err != nil {
		t.Fatalf("NewPreflight: %v", err)
	}
	return p, sink, igs, clock
}

func TestNewPreflightRequiresOurCall(t *testing.T) {
	if _, err := NewPreflight(PreflightConfig{
		TxSink: &fakeTxSink{},
	}); err == nil {
		t.Fatal("NewPreflight without OurCall must error")
	}
}

func TestNewPreflightRequiresTxSink(t *testing.T) {
	if _, err := NewPreflight(PreflightConfig{
		OurCall: func() string { return "N0CALL" },
	}); err == nil {
		t.Fatal("NewPreflight without TxSink must error")
	}
}

func TestPreflightAutoAckChannelDefaultOne(t *testing.T) {
	p, _, _, _ := newPreflightForTest(t)
	if got := p.AutoAckChannel(); got != 1 {
		t.Fatalf("AutoAckChannel default = %d, want 1", got)
	}
	p.SetAutoAckChannel(5)
	if got := p.AutoAckChannel(); got != 5 {
		t.Fatalf("AutoAckChannel after Set = %d, want 5", got)
	}
	p.SetAutoAckChannel(0)
	if got := p.AutoAckChannel(); got != 5 {
		t.Fatalf("SetAutoAckChannel(0) must be ignored, got %d", got)
	}
}

func TestPreflightCheckDedupFirstCallMisses(t *testing.T) {
	p, _, _, _ := newPreflightForTest(t)
	if hit := p.CheckDedup("W1ABC", "001", "hello"); hit {
		t.Fatal("first call must not be a dedup hit")
	}
}

func TestPreflightCheckDedupSecondCallHits(t *testing.T) {
	p, _, _, _ := newPreflightForTest(t)
	_ = p.CheckDedup("W1ABC", "001", "hello")
	if hit := p.CheckDedup("W1ABC", "001", "hello"); !hit {
		t.Fatal("second identical call must hit")
	}
}

func TestPreflightCheckDedupExpiresAfterWindow(t *testing.T) {
	p, _, _, clock := newPreflightForTest(t)
	_ = p.CheckDedup("W1ABC", "001", "hello")
	clock.now = clock.now.Add(DefaultRouterDedupWindow + time.Second)
	if hit := p.CheckDedup("W1ABC", "001", "hello"); hit {
		t.Fatal("expired entry must miss")
	}
}

func TestPreflightCheckDedupKeyDistinct(t *testing.T) {
	p, _, _, _ := newPreflightForTest(t)
	_ = p.CheckDedup("W1ABC", "001", "hello")
	if hit := p.CheckDedup("W1ABC", "002", "hello"); hit {
		t.Fatal("different msgid must miss")
	}
	if hit := p.CheckDedup("W2XYZ", "001", "hello"); hit {
		t.Fatal("different sender must miss")
	}
	if hit := p.CheckDedup("W1ABC", "001", "world"); hit {
		t.Fatal("different text-hash must miss")
	}
}

func TestPreflightSendAutoAckRFSubmitsFrame(t *testing.T) {
	p, sink, _, _ := newPreflightForTest(t)
	pkt := &aprs.DecodedAPRSPacket{Direction: aprs.DirectionRF, Channel: 3}
	p.SendAutoAck(context.Background(), pkt, "W1ABC", "001")

	subs := sink.list()
	if len(subs) != 1 {
		t.Fatalf("want 1 RF submit, got %d", len(subs))
	}
	if subs[0].Channel != 3 {
		t.Fatalf("RF auto-ACK channel = %d, want pkt.Channel=3", subs[0].Channel)
	}
	if !subs[0].Src.SkipDedup {
		t.Fatal("auto-ACK must SkipDedup")
	}
}

func TestPreflightSendAutoAckISMirrorsViaIGate(t *testing.T) {
	p, sink, igs, _ := newPreflightForTest(t)
	pkt := &aprs.DecodedAPRSPacket{Direction: aprs.DirectionIS}
	p.SendAutoAck(context.Background(), pkt, "W1ABC", "001")

	if got := len(sink.list()); got != 0 {
		t.Fatalf("IS auto-ACK must not submit RF, got %d", got)
	}
	lines := igs.list()
	if len(lines) != 1 {
		t.Fatalf("want 1 IS line, got %d", len(lines))
	}
	if !strings.Contains(lines[0], ":ack001") {
		t.Fatalf("IS line missing ack token: %q", lines[0])
	}
}

func TestPreflightSendAutoAckEmptyMsgIDNoOp(t *testing.T) {
	p, sink, igs, _ := newPreflightForTest(t)
	pkt := &aprs.DecodedAPRSPacket{Direction: aprs.DirectionRF, Channel: 1}
	p.SendAutoAck(context.Background(), pkt, "W1ABC", "")
	if got := len(sink.list()); got != 0 {
		t.Fatalf("empty msgID must not emit RF: %d", got)
	}
	if got := len(igs.list()); got != 0 {
		t.Fatalf("empty msgID must not emit IS: %d", got)
	}
}

func TestPreflightSendAutoAckRFFallsBackToConfiguredChannel(t *testing.T) {
	p, sink, _, _ := newPreflightForTest(t)
	p.SetAutoAckChannel(7)
	pkt := &aprs.DecodedAPRSPacket{Direction: aprs.DirectionRF, Channel: 0}
	p.SendAutoAck(context.Background(), pkt, "W1ABC", "001")

	subs := sink.list()
	if len(subs) != 1 || subs[0].Channel != 7 {
		t.Fatalf("RF fallback channel: got %+v, want channel=7", subs)
	}
}
