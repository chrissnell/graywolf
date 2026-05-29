package digipeater

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/app/ingress"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/digipeater/blocklist"
	"github.com/chrissnell/graywolf/pkg/internal/testtx"
)

// fakeChannelModeLookup is a test stub for configstore.ChannelModeLookup.
type fakeChannelModeLookup struct{ modes map[uint32]string }

func (f *fakeChannelModeLookup) ModeForChannel(_ context.Context, id uint32) (string, error) {
	return f.modes[id], nil
}

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
	// MyCall is the per-digipeater override; StationCallsign is the
	// shared fallback. Passing the test's callsign as MyCall exercises
	// the override path, which is the intent of every test in this file.
	d, err := New(Config{
		MyCall:       mycall,
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

	// WIDE2-2 → mycall* should be inserted and WIDE2 decremented to
	// WIDE2-1 (not yet consumed). Per APRS New-N paradigm.
	rx := buildFrame(t, "KK6ABC", "APRS", []string{"WIDE2-2"}, "test")
	if !d.Handle(context.Background(), 1, rx, ingress.Modem()) {
		t.Fatalf("expected digi to repeat WIDE2-2")
	}
	cap := sink.Last()
	if cap == nil {
		t.Fatalf("no tx captured")
	}
	if len(cap.Frame.Path) != 2 {
		t.Fatalf("expected inserted mycall: %v", cap.Frame.Path)
	}
	if cap.Frame.Path[0].Call != "N0CAL" || cap.Frame.Path[0].SSID != 1 || !cap.Frame.Path[0].Repeated {
		t.Fatalf("mycall not inserted first+repeated: %+v", cap.Frame.Path[0])
	}
	slot := cap.Frame.Path[1]
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
	cap := sink.Last()
	if len(cap.Frame.Path) != 2 {
		t.Fatalf("expected inserted mycall: %v", cap.Frame.Path)
	}
	if cap.Frame.Path[0].Call != "N0CAL" || !cap.Frame.Path[0].Repeated {
		t.Fatalf("mycall not inserted first+repeated: %+v", cap.Frame.Path[0])
	}
	slot := cap.Frame.Path[1]
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

// TestDigipeaterChannelModeGating verifies that Handle respects the
// channel mode: packet-mode RX channels short-circuit before any rule
// evaluation; aprs and aprs+packet channels proceed normally; a nil
// ChannelModes lookup is treated as all-APRS (preserves the legacy
// any-channel-does-anything behavior).
func TestDigipeaterChannelModeGating(t *testing.T) {
	t.Parallel()

	rule := Rule{
		FromChannel: 3, ToChannel: 3,
		Alias: "WIDE", AliasType: "widen", MaxHops: 2, Action: "repeat",
	}

	cases := []struct {
		name       string
		modes      map[uint32]string // nil → use nil ChannelModes
		rxChannel  uint32
		wantRepeat bool
	}{
		{
			name:       "nil lookup permits (legacy behaviour)",
			modes:      nil,
			rxChannel:  3,
			wantRepeat: true,
		},
		{
			name:       "aprs mode permits",
			modes:      map[uint32]string{3: configstore.ChannelModeAPRS},
			rxChannel:  3,
			wantRepeat: true,
		},
		{
			name:       "aprs+packet mode permits",
			modes:      map[uint32]string{3: configstore.ChannelModeAPRSPacket},
			rxChannel:  3,
			wantRepeat: true,
		},
		{
			name:       "packet mode blocks",
			modes:      map[uint32]string{3: configstore.ChannelModePacket},
			rxChannel:  3,
			wantRepeat: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sink := testtx.NewRecorder()
			cfg := Config{
				MyCall:       "N0CAL-1",
				DedupeWindow: 500 * time.Millisecond,
				Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
				Submit:       sink.Submit,
			}
			if tc.modes != nil {
				cfg.ChannelModes = &fakeChannelModeLookup{modes: tc.modes}
			}
			d, err := New(cfg)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			d.SetEnabled(true)
			d.SetRules([]Rule{rule})

			rx := buildFrame(t, "KK6ABC", "APRS", []string{"WIDE2-2"}, "test")
			got := d.Handle(context.Background(), tc.rxChannel, rx, ingress.Modem())
			if got != tc.wantRepeat {
				t.Fatalf("Handle returned %v, want %v", got, tc.wantRepeat)
			}
			if tc.wantRepeat && sink.Len() == 0 {
				t.Fatal("expected TX frame but sink is empty")
			}
			if !tc.wantRepeat && sink.Len() != 0 {
				t.Fatalf("expected no TX but sink has %d frames", sink.Len())
			}
		})
	}
}

// TestDigipeaterSkipsRuleWhenFromChannelIsPacket is the canonical
// single-case version for CI clarity.
func TestDigipeaterSkipsRuleWhenFromChannelIsPacket(t *testing.T) {
	t.Parallel()
	modes := &fakeChannelModeLookup{modes: map[uint32]string{
		3: configstore.ChannelModePacket,
	}}
	sink := testtx.NewRecorder()
	d, err := New(Config{
		MyCall:       "N0CAL-1",
		DedupeWindow: 500 * time.Millisecond,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		ChannelModes: modes,
		Submit:       sink.Submit,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	d.SetEnabled(true)
	d.SetRules([]Rule{{
		FromChannel: 3, ToChannel: 3,
		Alias: "WIDE", AliasType: "widen", MaxHops: 1, Action: "repeat",
	}})
	rx := buildFrame(t, "KK6ABC", "APRS", []string{"WIDE2-2"}, "test")
	consumed := d.Handle(context.Background(), 3, rx, ingress.Modem())
	if consumed {
		t.Fatal("digipeater should skip rules on packet-mode channel")
	}
	if got := sink.Len(); got != 0 {
		t.Fatalf("sink got %d frames, want 0", got)
	}
}

// TestDigipeaterPerRulePacketModeToChannel verifies that a rule whose
// ToChannel is packet-mode is skipped while other rules still match.
func TestDigipeaterPerRulePacketModeToChannel(t *testing.T) {
	t.Parallel()
	// Channel 1 = aprs (rx), channel 2 = packet (tx-only rule), channel 3 = aprs (tx-ok rule).
	modes := &fakeChannelModeLookup{modes: map[uint32]string{
		1: configstore.ChannelModeAPRS,
		2: configstore.ChannelModePacket,
		3: configstore.ChannelModeAPRS,
	}}
	sink := testtx.NewRecorder()
	d, err := New(Config{
		MyCall:       "N0CAL-1",
		DedupeWindow: 500 * time.Millisecond,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		ChannelModes: modes,
		Submit:       sink.Submit,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	d.SetEnabled(true)
	// Two rules for the same RX channel: first routes to packet-mode ch2
	// (should be skipped), second routes to aprs ch3 (should match).
	d.SetRules([]Rule{
		{FromChannel: 1, ToChannel: 2, Alias: "WIDE", AliasType: "widen", MaxHops: 2, Action: "repeat"},
		{FromChannel: 1, ToChannel: 3, Alias: "WIDE", AliasType: "widen", MaxHops: 2, Action: "repeat"},
	})
	rx := buildFrame(t, "KK6ABC", "APRS", []string{"WIDE2-2"}, "test")
	if !d.Handle(context.Background(), 1, rx, ingress.Modem()) {
		t.Fatal("expected rule to ch3 to fire")
	}
	if sink.Len() == 0 {
		t.Fatal("no TX frame captured")
	}
	if ch := sink.Last().Channel; ch != 3 {
		t.Fatalf("TX channel = %d, want 3", ch)
	}
}

func TestHandle_BlockedSourceShortCircuits(t *testing.T) {
	t.Parallel()
	rules := []Rule{{
		FromChannel: 1, ToChannel: 1,
		Alias: "WIDE", AliasType: "widen", MaxHops: 2, Action: "repeat",
	}}
	d, sink := newTestDigi(t, rules, "N0CAL-1")
	d.SetBlocklist([]blocklist.Entry{{Pattern: "BADCAL-*", Reason: "test"}})

	rx := buildFrame(t, "BADCAL-9", "APRS", []string{"WIDE2-2"}, "blocked")
	if d.Handle(context.Background(), 1, rx, ingress.Modem()) {
		t.Fatal("Handle returned true for blocked source")
	}
	if sink.Len() != 0 {
		t.Fatalf("submit called: sink len=%d", sink.Len())
	}
	stats := d.Stats()
	if stats.Blocked != 1 {
		t.Fatalf("Blocked=%d, want 1", stats.Blocked)
	}
	if stats.Packets != 0 {
		t.Fatalf("Packets=%d, want 0", stats.Packets)
	}
	if stats.Deduped != 0 {
		t.Fatalf("Deduped=%d, want 0", stats.Deduped)
	}
}

func TestHandle_BlockedSourceBeforeDedup(t *testing.T) {
	// A blocked frame must not consume the dedup window. We send the
	// same blocked frame twice; if the block check ran AFTER dedup we
	// would see Deduped==1 on the second call.
	t.Parallel()
	rules := []Rule{{
		FromChannel: 1, ToChannel: 1,
		Alias: "WIDE", AliasType: "widen", MaxHops: 2, Action: "repeat",
	}}
	d, _ := newTestDigi(t, rules, "N0CAL-1")
	d.SetBlocklist([]blocklist.Entry{{Pattern: "BADCAL-*"}})

	rx1 := buildFrame(t, "BADCAL-9", "APRS", []string{"WIDE2-2"}, "same")
	rx2 := buildFrame(t, "BADCAL-9", "APRS", []string{"WIDE2-2"}, "same")
	d.Handle(context.Background(), 1, rx1, ingress.Modem())
	d.Handle(context.Background(), 1, rx2, ingress.Modem())

	stats := d.Stats()
	if stats.Blocked != 2 {
		t.Fatalf("Blocked=%d, want 2", stats.Blocked)
	}
	if stats.Deduped != 0 {
		t.Fatalf("Deduped=%d, want 0 (block check must precede dedup)", stats.Deduped)
	}
}

func TestHandle_NonBlockedFrameStillDigis(t *testing.T) {
	t.Parallel()
	rules := []Rule{{
		FromChannel: 1, ToChannel: 1,
		Alias: "WIDE", AliasType: "widen", MaxHops: 2, Action: "repeat",
	}}
	d, sink := newTestDigi(t, rules, "N0CAL-1")
	d.SetBlocklist([]blocklist.Entry{{Pattern: "OTHRCL-*"}})

	rx := buildFrame(t, "BADCAL-9", "APRS", []string{"WIDE2-2"}, "ok")
	if !d.Handle(context.Background(), 1, rx, ingress.Modem()) {
		t.Fatal("expected non-blocked frame to be digipeated")
	}
	if sink.Len() != 1 {
		t.Fatalf("sink len=%d, want 1", sink.Len())
	}
	if d.Stats().Blocked != 0 {
		t.Fatalf("Blocked=%d, want 0", d.Stats().Blocked)
	}
}

func TestSetBlocklist_LiveReconfig(t *testing.T) {
	t.Parallel()
	rules := []Rule{{
		FromChannel: 1, ToChannel: 1,
		Alias: "WIDE", AliasType: "widen", MaxHops: 2, Action: "repeat",
	}}
	d, sink := newTestDigi(t, rules, "N0CAL-1")
	d.SetBlocklist([]blocklist.Entry{{Pattern: "BADCAL-*"}})

	rx1 := buildFrame(t, "BADCAL-9", "APRS", []string{"WIDE2-2"}, "first")
	if d.Handle(context.Background(), 1, rx1, ingress.Modem()) {
		t.Fatal("first frame should be blocked")
	}
	if sink.Len() != 0 {
		t.Fatalf("sink len=%d after block, want 0", sink.Len())
	}

	d.SetBlocklist(nil)
	rx2 := buildFrame(t, "BADCAL-9", "APRS", []string{"WIDE2-2"}, "second")
	if !d.Handle(context.Background(), 1, rx2, ingress.Modem()) {
		t.Fatal("after SetBlocklist(nil) frame should digi")
	}
	if sink.Len() != 1 {
		t.Fatalf("sink len=%d after unblock, want 1", sink.Len())
	}

	d.SetBlocklist([]blocklist.Entry{{Pattern: "OTHRCL-*"}})
	rx3 := buildFrame(t, "BADCAL-9", "APRS", []string{"WIDE2-2"}, "third")
	if !d.Handle(context.Background(), 1, rx3, ingress.Modem()) {
		t.Fatal("frame with non-matching block list should digi")
	}

	d.SetBlocklist([]blocklist.Entry{{Pattern: "BADCAL-*"}})
	rx4 := buildFrame(t, "BADCAL-9", "APRS", []string{"WIDE2-2"}, "fourth")
	if d.Handle(context.Background(), 1, rx4, ingress.Modem()) {
		t.Fatal("frame should be re-blocked after re-adding pattern")
	}
}

func TestBlocklistFromStore_EnabledOnly(t *testing.T) {
	t.Parallel()
	rows := []configstore.DigipeaterBlocklist{
		{Pattern: "BADCAL-*", Reason: "noisy", Enabled: true},
		{Pattern: "OTHER-1", Reason: "disabled", Enabled: false},
		{Pattern: "THIRD", Reason: "", Enabled: true},
	}
	got := BlocklistFromStore(rows)
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2 (disabled row dropped)", len(got))
	}
	if got[0].Pattern != "BADCAL-*" || got[0].Reason != "noisy" {
		t.Fatalf("entry 0 = %+v", got[0])
	}
	if got[1].Pattern != "THIRD" {
		t.Fatalf("entry 1 = %+v", got[1])
	}
}
