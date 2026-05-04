package messages

import (
	"io"
	"log/slog"
	"testing"
	"time"
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
