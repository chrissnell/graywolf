package digipeater

import (
	"context"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/app/ingress"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/internal/testtx"
)

func mustAddr(t *testing.T, s string) ax25.Address {
	t.Helper()
	a, err := ax25.ParseAddress(s)
	if err != nil {
		t.Fatalf("ParseAddress(%q): %v", s, err)
	}
	return a
}

func newTestDigi(t *testing.T, rules []Rule, mycall string) (*Digipeater, *testtx.Recorder) {
	sink := testtx.NewRecorder()
	d, err := New(Config{
		MyCall:       mustAddr(t, mycall),
		DedupeWindow: 500 * time.Millisecond,
		Rules:        rules,
		Submit:       sink.Submit,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// New() now defaults to disabled; tests want a live engine.
	d.SetEnabled(true)
	return d, sink
}

func buildFrame(t *testing.T, src, dest string, path []string, info string) *ax25.Frame {
	t.Helper()
	addrs := make([]ax25.Address, 0, len(path))
	for _, p := range path {
		addrs = append(addrs, mustAddr(t, p))
	}
	f, err := ax25.NewUIFrame(mustAddr(t, src), mustAddr(t, dest), addrs, []byte(info))
	if err != nil {
		t.Fatalf("NewUIFrame: %v", err)
	}
	return f
}

func TestWIDEnNDecrementing(t *testing.T) {
	rules := []Rule{{
		FromChannel: 1, ToChannel: 1,
		Alias: "WIDE", AliasType: "widen", MaxHops: 3,
		Action: "repeat",
	}}
	d, sink := newTestDigi(t, rules, "N0CAL-1")

	// WIDE2-2 → should become WIDE2-1 (SSID decremented, not yet consumed).
	rx := buildFrame(t, "KK6ABC", "APRS", []string{"WIDE2-2"}, "test")
	if !d.Handle(context.Background(), 1, rx, ingress.Modem()) {
		t.Fatalf("expected digi to repeat WIDE2-2")
	}
	cap := sink.Last()
	if cap == nil {
		t.Fatalf("no tx captured")
	}
	if len(cap.Frame.Path) != 1 {
		t.Fatalf("path len %d", len(cap.Frame.Path))
	}
	slot := cap.Frame.Path[0]
	if slot.Call != "WIDE2" || slot.SSID != 1 || slot.Repeated {
		t.Fatalf("expected WIDE2-1 unconsumed, got %s repeated=%v", slot.String(), slot.Repeated)
	}
	// RX frame must be untouched.
	if rx.Path[0].SSID != 2 || rx.Path[0].Repeated {
		t.Fatalf("rx frame was mutated: %+v", rx.Path[0])
	}
}

func TestWIDE1_1Consumed(t *testing.T) {
	rules := []Rule{{
		FromChannel: 1, ToChannel: 1,
		Alias: "WIDE", AliasType: "widen", MaxHops: 2,
		Action: "repeat",
	}}
	d, sink := newTestDigi(t, rules, "N0CAL")
	rx := buildFrame(t, "KK6ABC", "APRS", []string{"WIDE1-1"}, "x")
	if !d.Handle(context.Background(), 1, rx, ingress.Modem()) {
		t.Fatalf("expected repeat")
	}
	slot := sink.Last().Frame.Path[0]
	if !slot.Repeated || slot.SSID != 0 {
		t.Fatalf("WIDE1-1 should be consumed (H=1, SSID=0): %+v", slot)
	}
}

func TestWIDE7_7Rejected(t *testing.T) {
	rules := []Rule{{
		FromChannel: 1, ToChannel: 1,
		Alias: "WIDE", AliasType: "widen", MaxHops: 2, // max 2
		Action: "repeat",
	}}
	d, _ := newTestDigi(t, rules, "N0CAL")
	rx := buildFrame(t, "KK6ABC", "APRS", []string{"WIDE7-7"}, "x")
	if d.Handle(context.Background(), 1, rx, ingress.Modem()) {
		t.Fatalf("WIDE7-7 should not be repeated with MaxHops=2")
	}
}

func TestPreemptiveDigiOnLocalCall(t *testing.T) {
	rules := []Rule{{
		FromChannel: 1, ToChannel: 1,
		Alias: "WIDE", AliasType: "widen", MaxHops: 2,
		Action: "repeat",
	}}
	d, sink := newTestDigi(t, rules, "N0CAL-3")
	rx := buildFrame(t, "KK6ABC", "APRS", []string{"N0CAL-3", "WIDE2-2"}, "hi")
	if !d.Handle(context.Background(), 1, rx, ingress.Modem()) {
		t.Fatalf("expected preemptive digi")
	}
	cap := sink.Last()
	// First slot should be marked repeated (preempted).
	if !cap.Frame.Path[0].Repeated {
		t.Fatalf("preemptive slot not marked repeated: %+v", cap.Frame.Path[0])
	}
}

func TestDedupWindow(t *testing.T) {
	rules := []Rule{{FromChannel: 1, ToChannel: 1, Alias: "WIDE", AliasType: "widen", MaxHops: 2, Action: "repeat"}}
	d, sink := newTestDigi(t, rules, "N0CAL")
	rx := buildFrame(t, "KK6ABC", "APRS", []string{"WIDE2-2"}, "same")
	if !d.Handle(context.Background(), 1, rx, ingress.Modem()) {
		t.Fatalf("first handle should succeed")
	}
	// Same frame within window → deduped. Use a fresh identical copy
	// because Handle stores the outgoing path as the dedup key.
	rx2 := buildFrame(t, "KK6ABC", "APRS", []string{"WIDE2-2"}, "same")
	if d.Handle(context.Background(), 1, rx2, ingress.Modem()) {
		t.Fatalf("second identical frame should be deduped")
	}
	if d.Stats().Deduped == 0 {
		t.Fatalf("deduped counter not incremented")
	}
	// After the window, the same frame is accepted again.
	time.Sleep(600 * time.Millisecond)
	rx3 := buildFrame(t, "KK6ABC", "APRS", []string{"WIDE2-2"}, "same")
	if !d.Handle(context.Background(), 1, rx3, ingress.Modem()) {
		t.Fatalf("post-window frame should be accepted")
	}
	_ = sink
}

func TestCrossChannelDigi(t *testing.T) {
	rules := []Rule{{
		FromChannel: 1, ToChannel: 2, // RX on 1, TX on 2
		Alias: "WIDE", AliasType: "widen", MaxHops: 2,
		Action: "repeat",
	}}
	d, sink := newTestDigi(t, rules, "N0CAL")
	rx := buildFrame(t, "KK6ABC", "APRS", []string{"WIDE2-2"}, "x")
	if !d.Handle(context.Background(), 1, rx, ingress.Modem()) {
		t.Fatalf("expected cross-channel digi")
	}
	cap := sink.Last()
	if cap.Channel != 2 {
		t.Fatalf("TX channel = %d want 2", cap.Channel)
	}
	// RX on channel 2 with FromChannel=1 rule should not match.
	rx2 := buildFrame(t, "W1AW", "APRS", []string{"WIDE2-2"}, "y")
	if d.Handle(context.Background(), 2, rx2, ingress.Modem()) {
		t.Fatalf("RX channel 2 should not match FromChannel=1 rule")
	}
}

func TestFullyConsumedFrameIgnored(t *testing.T) {
	rules := []Rule{{FromChannel: 1, ToChannel: 1, Alias: "WIDE", AliasType: "widen", MaxHops: 2, Action: "repeat"}}
	d, _ := newTestDigi(t, rules, "N0CAL")
	rx := buildFrame(t, "KK6ABC", "APRS", []string{"WIDE1*"}, "x")
	// ParseAddress("WIDE1*") sets Repeated=true and SSID=0.
	if d.Handle(context.Background(), 1, rx, ingress.Modem()) {
		t.Fatalf("fully-consumed frame should be ignored")
	}
}

func TestDoNotDigiOwnTransmissions(t *testing.T) {
	rules := []Rule{{FromChannel: 1, ToChannel: 1, Alias: "WIDE", AliasType: "widen", MaxHops: 2, Action: "repeat"}}
	d, _ := newTestDigi(t, rules, "N0CAL")
	rx := buildFrame(t, "N0CAL", "APRS", []string{"WIDE2-2"}, "loopback")
	if d.Handle(context.Background(), 1, rx, ingress.Modem()) {
		t.Fatalf("own source should not be digipeated")
	}
}

func TestDropAction(t *testing.T) {
	rules := []Rule{{
		FromChannel: 1, ToChannel: 1,
		Alias: "RFONLY", AliasType: "exact", Action: "drop",
	}}
	d, sink := newTestDigi(t, rules, "N0CAL")
	rx := buildFrame(t, "KK6ABC", "APRS", []string{"RFONLY"}, "x")
	if d.Handle(context.Background(), 1, rx, ingress.Modem()) {
		t.Fatalf("drop rule should not submit")
	}
	if sink.Last() != nil {
		t.Fatalf("drop rule should not produce TX")
	}
}

func TestTRACEInsertsMyCall(t *testing.T) {
	rules := []Rule{{
		FromChannel: 1, ToChannel: 1,
		Alias: "TRACE", AliasType: "trace", MaxHops: 3,
		Action: "repeat",
	}}
	d, sink := newTestDigi(t, rules, "N0CAL-7")
	rx := buildFrame(t, "KK6ABC", "APRS", []string{"TRACE2-2"}, "x")
	if !d.Handle(context.Background(), 1, rx, ingress.Modem()) {
		t.Fatalf("trace should repeat")
	}
	cap := sink.Last()
	if len(cap.Frame.Path) != 2 {
		t.Fatalf("expected inserted mycall: %v", cap.Frame.Path)
	}
	if cap.Frame.Path[0].Call != "N0CAL" || !cap.Frame.Path[0].Repeated {
		t.Fatalf("mycall not inserted first+repeated: %+v", cap.Frame.Path[0])
	}
	if cap.Frame.Path[1].Call != "TRACE2" || cap.Frame.Path[1].SSID != 1 {
		t.Fatalf("trace slot not decremented: %+v", cap.Frame.Path[1])
	}
}
