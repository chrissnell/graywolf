package bulletins

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// waitFor polls cond every 5 ms until it returns true or timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("timed out waiting for: %s", msg)
}

func buildSchedulerRig(t *testing.T) (*Scheduler, *Store, *fakeBulletinTxSink, *configstore.Store) {
	t.Helper()
	cs, err := configstore.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	sink := &fakeBulletinTxSink{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := NewStore(cs.DB())
	sender := NewSender(sink, nil, func() string { return "W5X-9" }, "", logger)
	sc := NewScheduler(store, sender, 1, logger)
	return sc, store, sink, cs
}

func TestScheduler_ProcessesDueRows(t *testing.T) {
	sc, store, sink, _ := buildSchedulerRig(t)
	ctx := context.Background()

	past := time.Now().UTC().Add(-time.Second)
	b := &configstore.Bulletin{
		Slot:       "BLN0",
		Text:       "scheduled bulletin",
		MaxSends:   12,
		NextSendAt: &past,
		SendCount:  0,
	}
	if err := store.Insert(ctx, b); err != nil {
		t.Fatal(err)
	}

	sc.Start(ctx)
	t.Cleanup(sc.Stop)

	waitFor(t, 3*time.Second, func() bool { return sink.count() > 0 }, "bulletin to be sent")

	got, err := store.GetByID(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SendCount != 1 {
		t.Errorf("SendCount: got %d, want 1", got.SendCount)
	}
	if got.NextSendAt == nil || !got.NextSendAt.After(time.Now().UTC()) {
		t.Error("expected NextSendAt to be advanced into the future")
	}
	// First send is still in the burst window: next interval should be
	// BulletinBurstInterval, not BulletinInterval.
	if got.NextSendAt.After(time.Now().UTC().Add(BulletinBurstInterval + 5*time.Second)) {
		t.Errorf("expected burst interval (~%s), got NextSendAt far in the future: %s",
			BulletinBurstInterval, got.NextSendAt)
	}
}

func TestScheduler_BurstThenStableRate(t *testing.T) {
	sc, store, sink, _ := buildSchedulerRig(t)
	ctx := context.Background()

	past := time.Now().UTC().Add(-time.Second)
	b := &configstore.Bulletin{
		Slot:         "BLN0",
		Text:         "burst test",
		MaxSends:     12,
		NextSendAt:   &past,
		SendCount:    BulletinBurstCount - 1, // one send left in burst window
		IntervalMins: 20,
	}
	if err := store.Insert(ctx, b); err != nil {
		t.Fatal(err)
	}

	sc.Start(ctx)
	t.Cleanup(sc.Stop)

	waitFor(t, 3*time.Second, func() bool { return sink.count() > 0 }, "final burst send")

	got, err := store.GetByID(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	// After this send SendCount == BulletinBurstCount, so next interval
	// should be BulletinInterval (stable rate), not BulletinBurstInterval.
	minExpected := time.Now().UTC().Add(BulletinInterval - 5*time.Second)
	if got.NextSendAt == nil || got.NextSendAt.Before(minExpected) {
		t.Errorf("expected stable interval (~%s) after burst, got NextSendAt: %v",
			BulletinInterval, got.NextSendAt)
	}
}

func TestScheduler_Kick_TriggersImmediateSend(t *testing.T) {
	sc, store, sink, _ := buildSchedulerRig(t)
	ctx := context.Background()

	sc.Start(ctx)
	t.Cleanup(sc.Stop)

	// Insert a due row AFTER starting the scheduler (so the initial processDue
	// at startup runs before it exists) then kick.
	past := time.Now().UTC().Add(-time.Second)
	b := &configstore.Bulletin{
		Slot:       "BLN1",
		Text:       "kicked",
		MaxSends:   12,
		NextSendAt: &past,
	}
	if err := store.Insert(ctx, b); err != nil {
		t.Fatal(err)
	}
	sc.Kick()

	waitFor(t, 3*time.Second, func() bool { return sink.count() > 0 }, "kicked bulletin to be sent")
}

func TestScheduler_DoesNotSendExhausted(t *testing.T) {
	sc, store, sink, _ := buildSchedulerRig(t)
	ctx := context.Background()

	past := time.Now().UTC().Add(-time.Second)
	b := &configstore.Bulletin{
		Slot:       "BLN2",
		Text:       "exhausted",
		MaxSends:   3,
		NextSendAt: &past,
		SendCount:  3, // already at max
	}
	if err := store.Insert(ctx, b); err != nil {
		t.Fatal(err)
	}

	sc.Start(ctx)
	t.Cleanup(sc.Stop)
	sc.Kick()

	// Give it time to process.
	time.Sleep(200 * time.Millisecond)

	if sink.count() != 0 {
		t.Errorf("expected 0 sends for exhausted bulletin, got %d", sink.count())
	}
}

func TestScheduler_BurstOnly_StopsAfterBurst(t *testing.T) {
	sc, store, sink, _ := buildSchedulerRig(t)
	ctx := context.Background()

	past := time.Now().UTC().Add(-time.Second)
	b := &configstore.Bulletin{
		Slot:         "BLN3",
		Text:         "burst only",
		MaxSends:     12,
		NextSendAt:   &past,
		SendCount:    BulletinBurstCount - 1, // one burst send left
		IntervalMins: 0,                      // burst-only: stop after burst phase
	}
	if err := store.Insert(ctx, b); err != nil {
		t.Fatal(err)
	}

	sc.Start(ctx)
	t.Cleanup(sc.Stop)

	waitFor(t, 3*time.Second, func() bool { return sink.count() > 0 }, "burst send")

	got, err := store.GetByID(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	// With interval=0 the scheduler should clear NextSendAt so the row
	// never fires again.
	if got.NextSendAt != nil {
		t.Errorf("expected NextSendAt=nil for burst-only bulletin, got %v", got.NextSendAt)
	}
}

func TestScheduler_CustomInterval(t *testing.T) {
	sc, store, sink, _ := buildSchedulerRig(t)
	ctx := context.Background()

	past := time.Now().UTC().Add(-time.Second)
	b := &configstore.Bulletin{
		Slot:         "BLN4",
		Text:         "custom interval",
		MaxSends:     12,
		NextSendAt:   &past,
		SendCount:    BulletinBurstCount, // burst phase complete
		IntervalMins: 5,                  // 5-minute stable interval
	}
	if err := store.Insert(ctx, b); err != nil {
		t.Fatal(err)
	}

	sc.Start(ctx)
	t.Cleanup(sc.Stop)

	waitFor(t, 3*time.Second, func() bool { return sink.count() > 0 }, "stable-rate send")

	got, err := store.GetByID(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	// NextSendAt should be ~5 minutes from now.
	minExpected := time.Now().UTC().Add(4 * time.Minute)
	maxExpected := time.Now().UTC().Add(6 * time.Minute)
	if got.NextSendAt == nil || got.NextSendAt.Before(minExpected) || got.NextSendAt.After(maxExpected) {
		t.Errorf("expected NextSendAt ~5 min from now, got %v", got.NextSendAt)
	}
}

func TestScheduler_Stop_ExitsCleanly(t *testing.T) {
	sc, _, _, _ := buildSchedulerRig(t)
	ctx := context.Background()
	sc.Start(ctx)
	done := make(chan struct{})
	go func() {
		sc.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("Stop() did not return within 2s")
	}
}

func TestScheduler_AnnouncementUsesAnnouncementInterval(t *testing.T) {
	sc, store, sink, _ := buildSchedulerRig(t)
	ctx := context.Background()

	past := time.Now().UTC().Add(-time.Second)
	b := &configstore.Bulletin{
		Slot:           "BLNA",
		Text:           "Club net every Tuesday 2000z",
		MaxSends:       AnnouncementMaxSends,
		NextSendAt:     &past,
		SendCount:      0,
		IsAnnouncement: true,
	}
	if err := store.Insert(ctx, b); err != nil {
		t.Fatal(err)
	}

	sc.Start(ctx)
	t.Cleanup(sc.Stop)

	waitFor(t, 3*time.Second, func() bool { return sink.count() > 0 }, "announcement send")

	got, err := store.GetByID(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Announcements skip the burst phase and always use AnnouncementInterval (1 h).
	minExpected := time.Now().UTC().Add(AnnouncementInterval - 10*time.Second)
	if got.NextSendAt == nil || got.NextSendAt.Before(minExpected) {
		t.Errorf("expected NextSendAt ~%s from now (AnnouncementInterval), got %v",
			AnnouncementInterval, got.NextSendAt)
	}
}

func TestScheduler_SoftDeletedRowNotSent(t *testing.T) {
	sc, store, sink, _ := buildSchedulerRig(t)
	ctx := context.Background()

	past := time.Now().UTC().Add(-time.Second)
	b := &configstore.Bulletin{
		Slot:       "BLN5",
		Text:       "should not send",
		MaxSends:   12,
		NextSendAt: &past,
	}
	if err := store.Insert(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err := store.SoftDelete(ctx, b.ID); err != nil {
		t.Fatal(err)
	}

	sc.Start(ctx)
	t.Cleanup(sc.Stop)
	sc.Kick()

	time.Sleep(300 * time.Millisecond)
	if sink.count() != 0 {
		t.Errorf("expected 0 sends for soft-deleted bulletin, got %d", sink.count())
	}
}
