package bulletins

import (
	"context"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

func newTestBulletinStore(t *testing.T) (*Store, *configstore.Store) {
	t.Helper()
	cs, err := configstore.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return NewStore(cs.DB()), cs
}

func seedInbound(from, slot, text string) *configstore.Bulletin {
	exp := time.Now().UTC().Add(4 * time.Hour)
	return &configstore.Bulletin{
		Slot:      slot,
		FromCall:  from,
		Text:      text,
		Source:    "rf",
		ExpiresAt: &exp,
	}
}

// ---------------------------------------------------------------------------
// UpsertInbound
// ---------------------------------------------------------------------------

func TestUpsertInbound_Creates(t *testing.T) {
	store, _ := newTestBulletinStore(t)
	ctx := context.Background()

	b := seedInbound("W5X", "BLN0", "Net tonight 2000z")
	if err := store.UpsertInbound(ctx, b); err != nil {
		t.Fatalf("UpsertInbound: %v", err)
	}
	if b.ID == 0 {
		t.Fatal("expected ID to be set after insert")
	}
	if b.Direction != "in" {
		t.Errorf("Direction: got %q, want \"in\"", b.Direction)
	}
}

func TestUpsertInbound_UpdatesOnRehear(t *testing.T) {
	store, _ := newTestBulletinStore(t)
	ctx := context.Background()

	b1 := seedInbound("W5X", "BLN0", "first text")
	if err := store.UpsertInbound(ctx, b1); err != nil {
		t.Fatalf("UpsertInbound first: %v", err)
	}

	b2 := seedInbound("W5X", "BLN0", "updated text")
	if err := store.UpsertInbound(ctx, b2); err != nil {
		t.Fatalf("UpsertInbound second: %v", err)
	}

	rows, err := store.List(ctx, Filter{Direction: "in"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row after upsert, got %d", len(rows))
	}
	if rows[0].Text != "updated text" {
		t.Errorf("Text: got %q, want %q", rows[0].Text, "updated text")
	}
}

func TestUpsertInbound_DifferentSlotsAreDistinct(t *testing.T) {
	store, _ := newTestBulletinStore(t)
	ctx := context.Background()

	if err := store.UpsertInbound(ctx, seedInbound("W5X", "BLN0", "slot 0")); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertInbound(ctx, seedInbound("W5X", "BLN1", "slot 1")); err != nil {
		t.Fatal(err)
	}

	rows, err := store.List(ctx, Filter{Direction: "in"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows for different slots, got %d", len(rows))
	}
}

func TestUpsertInbound_DifferentCallsSameSlotAreDistinct(t *testing.T) {
	store, _ := newTestBulletinStore(t)
	ctx := context.Background()

	if err := store.UpsertInbound(ctx, seedInbound("W5X", "BLN0", "from W5X")); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertInbound(ctx, seedInbound("K9Y", "BLN0", "from K9Y")); err != nil {
		t.Fatal(err)
	}

	rows, err := store.List(ctx, Filter{Direction: "in"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows for different callers, got %d", len(rows))
	}
}

// ---------------------------------------------------------------------------
// Insert (outbound)
// ---------------------------------------------------------------------------

func TestInsert_CreatesOutbound(t *testing.T) {
	store, _ := newTestBulletinStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	b := &configstore.Bulletin{
		Slot:       "BLN0",
		Text:       "test bulletin",
		MaxSends:   12,
		NextSendAt: &now,
	}
	if err := store.Insert(ctx, b); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if b.ID == 0 {
		t.Fatal("expected ID to be set")
	}
	if b.Direction != "out" {
		t.Errorf("Direction: got %q, want \"out\"", b.Direction)
	}
}

// ---------------------------------------------------------------------------
// List / Filter
// ---------------------------------------------------------------------------

func TestList_DirectionFilter(t *testing.T) {
	store, _ := newTestBulletinStore(t)
	ctx := context.Background()

	// Insert one inbound and one outbound.
	if err := store.UpsertInbound(ctx, seedInbound("W5X", "BLN0", "in")); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.Insert(ctx, &configstore.Bulletin{
		Slot: "BLN1", Text: "out", MaxSends: 12, NextSendAt: &now,
	}); err != nil {
		t.Fatal(err)
	}

	inRows, err := store.List(ctx, Filter{Direction: "in"})
	if err != nil {
		t.Fatal(err)
	}
	if len(inRows) != 1 || inRows[0].Direction != "in" {
		t.Errorf("inbound filter: got %d rows", len(inRows))
	}

	outRows, err := store.List(ctx, Filter{Direction: "out"})
	if err != nil {
		t.Fatal(err)
	}
	if len(outRows) != 1 || outRows[0].Direction != "out" {
		t.Errorf("outbound filter: got %d rows", len(outRows))
	}

	all, err := store.List(ctx, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("no filter: expected 2, got %d", len(all))
	}
}

func TestList_UnreadFilter(t *testing.T) {
	store, _ := newTestBulletinStore(t)
	ctx := context.Background()

	if err := store.UpsertInbound(ctx, seedInbound("W5X", "BLN0", "unread")); err != nil {
		t.Fatal(err)
	}
	b2 := seedInbound("W5X", "BLN1", "read")
	if err := store.UpsertInbound(ctx, b2); err != nil {
		t.Fatal(err)
	}
	// Mark BLN1 read.
	if err := store.MarkRead(ctx, b2.ID); err != nil {
		t.Fatal(err)
	}

	unread, err := store.List(ctx, Filter{Direction: "in", UnreadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(unread) != 1 {
		t.Fatalf("expected 1 unread row, got %d", len(unread))
	}
	if unread[0].Slot != "BLN0" {
		t.Errorf("Slot: got %q, want BLN0", unread[0].Slot)
	}
}

// ---------------------------------------------------------------------------
// ListPendingSends
// ---------------------------------------------------------------------------

func TestListPendingSends(t *testing.T) {
	store, _ := newTestBulletinStore(t)
	ctx := context.Background()

	past := time.Now().UTC().Add(-time.Minute)
	future := time.Now().UTC().Add(time.Hour)

	// Due now.
	due := &configstore.Bulletin{
		Slot: "BLN0", Text: "due", MaxSends: 12, NextSendAt: &past, SendCount: 0,
	}
	// Not due yet.
	notDue := &configstore.Bulletin{
		Slot: "BLN1", Text: "future", MaxSends: 12, NextSendAt: &future, SendCount: 0,
	}
	// Exhausted (send_count == max_sends).
	exhausted := &configstore.Bulletin{
		Slot: "BLN2", Text: "done", MaxSends: 12, NextSendAt: &past, SendCount: 12,
	}

	for _, b := range []*configstore.Bulletin{due, notDue, exhausted} {
		if err := store.Insert(ctx, b); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := store.ListPendingSends(ctx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 pending row, got %d", len(rows))
	}
	if rows[0].Slot != "BLN0" {
		t.Errorf("expected BLN0, got %q", rows[0].Slot)
	}
}

// ---------------------------------------------------------------------------
// SoftDelete
// ---------------------------------------------------------------------------

func TestSoftDelete_ExcludesFromList(t *testing.T) {
	store, _ := newTestBulletinStore(t)
	ctx := context.Background()

	b := seedInbound("W5X", "BLN0", "to delete")
	if err := store.UpsertInbound(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err := store.SoftDelete(ctx, b.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	rows, err := store.List(ctx, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows after soft-delete, got %d", len(rows))
	}
}

func TestSoftDelete_ExcludesFromListPendingSends(t *testing.T) {
	store, _ := newTestBulletinStore(t)
	ctx := context.Background()

	past := time.Now().UTC().Add(-time.Minute)
	b := &configstore.Bulletin{
		Slot: "BLN0", Text: "x", MaxSends: 12, NextSendAt: &past,
	}
	if err := store.Insert(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err := store.SoftDelete(ctx, b.ID); err != nil {
		t.Fatal(err)
	}

	rows, err := store.ListPendingSends(ctx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 pending after soft-delete, got %d", len(rows))
	}
}

// ---------------------------------------------------------------------------
// MarkRead / MarkAllRead
// ---------------------------------------------------------------------------

func TestMarkRead(t *testing.T) {
	store, _ := newTestBulletinStore(t)
	ctx := context.Background()

	b := seedInbound("W5X", "BLN0", "unread")
	if err := store.UpsertInbound(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkRead(ctx, b.ID); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	rows, err := store.List(ctx, Filter{Direction: "in", UnreadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 unread after MarkRead, got %d", len(rows))
	}
}

func TestMarkAllRead_Store(t *testing.T) {
	store, _ := newTestBulletinStore(t)
	ctx := context.Background()

	for _, slot := range []string{"BLN0", "BLN1", "BLN2"} {
		if err := store.UpsertInbound(ctx, seedInbound("W5X", slot, slot+" text")); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.MarkAllRead(ctx); err != nil {
		t.Fatalf("MarkAllRead: %v", err)
	}

	rows, err := store.List(ctx, Filter{Direction: "in", UnreadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 unread after MarkAllRead, got %d", len(rows))
	}
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func TestUpdate_PersistsSendCount(t *testing.T) {
	store, _ := newTestBulletinStore(t)
	ctx := context.Background()

	past := time.Now().UTC().Add(-time.Minute)
	b := &configstore.Bulletin{
		Slot: "BLN0", Text: "x", MaxSends: 12, NextSendAt: &past,
	}
	if err := store.Insert(ctx, b); err != nil {
		t.Fatal(err)
	}

	b.SendCount = 3
	next := time.Now().UTC().Add(20 * time.Minute)
	b.NextSendAt = &next
	if err := store.Update(ctx, b); err != nil {
		t.Fatalf("Update: %v", err)
	}

	rows, err := store.ListPendingSends(ctx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	// next_send_at is in the future now; should not appear.
	if len(rows) != 0 {
		t.Errorf("expected 0 pending after Update, got %d", len(rows))
	}

	got, err := store.GetByID(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SendCount != 3 {
		t.Errorf("SendCount: got %d, want 3", got.SendCount)
	}
}
