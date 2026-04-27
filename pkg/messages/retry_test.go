package messages

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// retryRig bundles the sender + retry manager for tests.
type retryRig struct {
	*senderRig
	mgr *RetryManager
}

func buildRetry(t *testing.T, policy string) *retryRig {
	t.Helper()
	sr := buildSender(t, policy, true)
	mgr, err := NewRetryManager(RetryManagerConfig{
		Store:       sr.store,
		Sender:      sr.sender,
		Preferences: sr.prefs,
		EventHub:    sr.hub,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:       sr.clock,
		Rand:        rand.New(rand.NewSource(1)),
	})
	if err != nil {
		t.Fatalf("NewRetryManager: %v", err)
	}
	return &retryRig{senderRig: sr, mgr: mgr}
}

func TestRetry_BackoffLadder(t *testing.T) {
	rig := buildRetry(t, FallbackPolicyRFOnly)
	defer rig.close()
	// Single-entry ladder: every attempt resolves to 30s ± 10% jitter.
	// attempt 1 → 30s ± 10%
	d := rig.mgr.backoffFor(1)
	if d < 27*time.Second || d > 33*time.Second {
		t.Errorf("attempt 1 backoff = %v, want 27-33s", d)
	}
	// attempt 3 (final retry before budget exhaustion) → still 30s ± 10%.
	d3 := rig.mgr.backoffFor(3)
	if d3 < 27*time.Second || d3 > 33*time.Second {
		t.Errorf("attempt 3 backoff = %v, want 27-33s", d3)
	}
	// attempt beyond ladder reuses final entry — same 30s.
	d99 := rig.mgr.backoffFor(99)
	if d99 < 27*time.Second || d99 > 33*time.Second {
		t.Errorf("attempt 99 backoff = %v, want 27-33s", d99)
	}
}

func TestRetry_JitterBounds(t *testing.T) {
	rig := buildRetry(t, FallbackPolicyRFOnly)
	defer rig.close()
	base := 30 * time.Second
	// Sample many attempts; all should fall in ±10%.
	for i := 0; i < 100; i++ {
		d := rig.mgr.backoffFor(2) // 30s base (single-entry ladder)
		if d < time.Duration(float64(base)*0.9) || d > time.Duration(float64(base)*1.1) {
			t.Errorf("sample %d: backoff %v out of bounds", i, d)
		}
	}
}

func TestRetry_AttemptCap(t *testing.T) {
	rig := buildRetry(t, FallbackPolicyRFOnly)
	defer rig.close()
	// Override preferences cap to 2 for speed.
	rig.prefs.current.Store(&configstore.MessagePreferences{
		FallbackPolicy:   FallbackPolicyRFOnly,
		RetryMaxAttempts: 2,
	})
	row := newOutboundDM(t, rig.senderRig, "N0CALL", "W1ABC", "hi")
	// Pretend we've already used 2 attempts.
	row.Attempts = 2
	if err := rig.store.Update(context.Background(), row); err != nil {
		t.Fatalf("Update: %v", err)
	}
	// Force a send error so processDue doesn't short-circuit.
	rig.sink.setErr(errors.New("transport blew up"))
	rig.mgr.retryOne(context.Background(), *row)
	reloaded, _ := rig.store.GetByID(context.Background(), row.ID)
	if reloaded.AckState != AckStateRejected {
		t.Errorf("AckState = %q, want rejected", reloaded.AckState)
	}
	if !contains(reloaded.FailureReason, "exhausted") {
		t.Errorf("FailureReason = %q, want mention of exhausted", reloaded.FailureReason)
	}
}

func TestRetry_KickChannelWakesTimer(t *testing.T) {
	rig := buildRetry(t, FallbackPolicyRFOnly)
	defer rig.close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rig.mgr.Start(ctx)
	defer rig.mgr.Stop()

	// Insert a row whose NextRetryAt is already past — a kick should
	// cause the loop to process it.
	row := newOutboundDM(t, rig.senderRig, "N0CALL", "W1ABC", "hi")
	past := rig.clock.Now().Add(-1 * time.Second)
	row.NextRetryAt = &past
	row.Attempts = 1
	if err := rig.store.Update(context.Background(), row); err != nil {
		t.Fatalf("Update: %v", err)
	}
	rig.mgr.Kick()
	// Wait up to 1s for submit to happen.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(rig.sink.list()) > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("retry loop did not process kicked row")
}

func TestRetry_RestartRecoversPendingDM(t *testing.T) {
	rig := buildRetry(t, FallbackPolicyRFOnly)
	defer rig.close()
	// Plant a DM row awaiting ack with NextRetryAt in the past,
	// simulating a crash-recovery scenario.
	row := newOutboundDM(t, rig.senderRig, "N0CALL", "W1ABC", "recover me")
	past := rig.clock.Now().Add(-1 * time.Minute)
	row.NextRetryAt = &past
	row.Attempts = 1
	if err := rig.store.Update(context.Background(), row); err != nil {
		t.Fatalf("Update: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rig.mgr.Start(ctx)
	defer rig.mgr.Stop()

	// Wait for the bootstrap kick to drive the loop.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(rig.sink.list()) > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("bootstrap did not re-submit pending DM")
}

func TestRetry_ResendResetsCounters(t *testing.T) {
	rig := buildRetry(t, FallbackPolicyRFOnly)
	defer rig.close()
	row := newOutboundDM(t, rig.senderRig, "N0CALL", "W1ABC", "retry me")
	row.Attempts = 3
	row.FailureReason = "earlier failure"
	past := rig.clock.Now().Add(30 * time.Second)
	row.NextRetryAt = &past
	if err := rig.store.Update(context.Background(), row); err != nil {
		t.Fatalf("Update: %v", err)
	}
	res, err := rig.mgr.Resend(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("Resend: %v", err)
	}
	if res.Err != nil {
		t.Errorf("Resend result Err: %v", res.Err)
	}
	reloaded, _ := rig.store.GetByID(context.Background(), row.ID)
	if reloaded.FailureReason != "" {
		t.Errorf("FailureReason after Resend = %q, want empty", reloaded.FailureReason)
	}
	// Attempts reset to 0 and then incremented to 1 by scheduleNext
	// path (or left at 0 if sender didn't re-enroll). We primarily
	// assert the "reset" happened.
	if reloaded.Attempts > 1 {
		t.Errorf("Attempts after Resend = %d, want 0 or 1", reloaded.Attempts)
	}
}

func TestRetry_SoftDeleteMidRetryCancels(t *testing.T) {
	rig := buildRetry(t, FallbackPolicyRFOnly)
	defer rig.close()
	row := newOutboundDM(t, rig.senderRig, "N0CALL", "W1ABC", "cancel me")
	row.Attempts = 1
	past := rig.clock.Now().Add(-1 * time.Second)
	row.NextRetryAt = &past
	if err := rig.store.Update(context.Background(), row); err != nil {
		t.Fatalf("Update: %v", err)
	}
	// Put row in inFlight to simulate a concurrent retry.
	rig.mgr.inFlight[row.ID] = struct{}{}
	if err := rig.mgr.CancelRetry(context.Background(), row.ID); err != nil {
		t.Fatalf("CancelRetry: %v", err)
	}
	// inFlight entry removed.
	if _, ok := rig.mgr.inFlight[row.ID]; ok {
		t.Error("inFlight entry not cleared by CancelRetry")
	}
	reloaded, _ := rig.store.GetByID(context.Background(), row.ID)
	if reloaded.NextRetryAt != nil {
		t.Error("NextRetryAt not cleared")
	}
}

func TestRetry_TacticalResendSingleShot(t *testing.T) {
	rig := buildRetry(t, FallbackPolicyRFOnly)
	defer rig.close()
	row := newOutboundTactical(t, rig.senderRig, "N0CALL", "NET", "check in")
	row.SentAt = nil // never sent
	if err := rig.store.Update(context.Background(), row); err != nil {
		t.Fatalf("Update: %v", err)
	}
	res, err := rig.mgr.Resend(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("Resend: %v", err)
	}
	if res.Err != nil {
		t.Errorf("Resend err: %v", res.Err)
	}
	if res.Retryable {
		t.Error("tactical resend must NOT be retryable")
	}
	// NextRetryAt should remain nil (no enrollment).
	reloaded, _ := rig.store.GetByID(context.Background(), row.ID)
	if reloaded.NextRetryAt != nil {
		t.Error("tactical row enrolled in retry ladder")
	}
}

func TestRetry_ProcessDue_SkipsAlreadyAcked(t *testing.T) {
	rig := buildRetry(t, FallbackPolicyRFOnly)
	defer rig.close()
	row := newOutboundDM(t, rig.senderRig, "N0CALL", "W1ABC", "acked already")
	past := rig.clock.Now().Add(-1 * time.Second)
	row.NextRetryAt = &past
	row.Attempts = 1
	row.AckState = AckStateAcked
	if err := rig.store.Update(context.Background(), row); err != nil {
		t.Fatalf("Update: %v", err)
	}
	rig.mgr.processDue(context.Background())
	// No submit should have happened.
	if got := rig.sink.list(); len(got) != 0 {
		t.Errorf("submits = %d, want 0 (row already acked)", len(got))
	}
}

func TestRetry_QueueFullDoesntCountAgainstBudget(t *testing.T) {
	rig := buildRetry(t, FallbackPolicyRFOnly)
	defer rig.close()
	row := newOutboundDM(t, rig.senderRig, "N0CALL", "W1ABC", "hi")
	row.Attempts = 2
	past := rig.clock.Now().Add(-1 * time.Second)
	row.NextRetryAt = &past
	if err := rig.store.Update(context.Background(), row); err != nil {
		t.Fatalf("Update: %v", err)
	}
	rig.sink.setErrOnce(txgovernor.ErrQueueFull)
	rig.mgr.retryOne(context.Background(), *row)
	reloaded, _ := rig.store.GetByID(context.Background(), row.ID)
	// Attempts should NOT have incremented (queue-full rolls it back).
	if reloaded.Attempts != 2 {
		t.Errorf("Attempts after queue-full = %d, want 2 (budget preserved)", reloaded.Attempts)
	}
	// NextRetryAt scheduled ShortRetryDelay ahead.
	if reloaded.NextRetryAt == nil {
		t.Fatal("NextRetryAt nil after queue-full")
	}
	delta := reloaded.NextRetryAt.Sub(rig.clock.Now())
	// Allow for a small amount of scheduler wiggle room.
	if delta < 4*time.Second || delta > 6*time.Second {
		t.Errorf("short retry delta = %v, want ~5s", delta)
	}
}

// contains is a small helper to avoid importing strings in tests that
// already pull in so many packages.
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
