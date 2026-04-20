package digipeater

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/app/ingress"
	"github.com/chrissnell/graywolf/pkg/internal/testtx"
)

// TestOnDedupFiresOnDedupHit asserts that the OnDedup callback runs
// exactly when a duplicate frame is suppressed by the dedup window —
// which is the hook pkg/app uses to increment
// graywolf_digipeater_deduped_total. This test exists to pin that
// contract: if a refactor ever moves the dedup check around, the
// counter must still fire for each suppressed frame and must NOT
// fire for the first (accepted) copy.
//
// The audit for work-order 11.8 confirmed the counter already exists
// and is wired via OnDedup; this test documents that wiring in code.
func TestOnDedupFiresOnDedupHit(t *testing.T) {
	var dedupCount atomic.Int64
	sink := testtx.NewRecorder()
	rules := []Rule{{
		FromChannel: 1, ToChannel: 1,
		Alias: "WIDE", AliasType: "widen", MaxHops: 2, Action: "repeat",
	}}
	d, err := New(Config{
		MyCall:       mustAddr(t, "N0CAL"),
		DedupeWindow: 500 * time.Millisecond,
		Rules:        rules,
		Submit:       sink.Submit,
		Logger:       slog.Default(),
		OnDedup:      func() { dedupCount.Add(1) },
	})
	if err != nil {
		t.Fatal(err)
	}
	d.SetEnabled(true)

	// First copy must digipeat and must NOT fire OnDedup.
	first := buildFrame(t, "KK6ABC", "APRS", []string{"WIDE2-2"}, "same")
	if !d.Handle(context.Background(), 1, first, ingress.Modem()) {
		t.Fatal("first frame should digipeat")
	}
	if got := dedupCount.Load(); got != 0 {
		t.Errorf("after first: dedupCount = %d, want 0", got)
	}

	// Second copy within the window must be suppressed; OnDedup fires.
	second := buildFrame(t, "KK6ABC", "APRS", []string{"WIDE2-2"}, "same")
	if d.Handle(context.Background(), 1, second, ingress.Modem()) {
		t.Fatal("duplicate frame should be suppressed")
	}
	if got := dedupCount.Load(); got != 1 {
		t.Errorf("after duplicate: dedupCount = %d, want 1", got)
	}

	// Third copy still suppressed → count goes to 2.
	third := buildFrame(t, "KK6ABC", "APRS", []string{"WIDE2-2"}, "same")
	if d.Handle(context.Background(), 1, third, ingress.Modem()) {
		t.Fatal("second duplicate should also be suppressed")
	}
	if got := dedupCount.Load(); got != 2 {
		t.Errorf("after third: dedupCount = %d, want 2", got)
	}
}
