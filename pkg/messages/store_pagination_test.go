package messages

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestListNewestWindowReturnsMostRecent guards the chat-window read path
// (GH #521 recurrence). The thread-detail view opens a conversation with
// a cursor-less, bounded List; it must get the NEWEST slice of the
// thread. The regression it protects: List's default forward page orders
// updated_at ASC, so a bounded read returns the LEAST-recently-updated
// rows and silently drops the newest sends/receives once a thread grows
// past the page size -- and outbound rows, whose updated_at is bumped
// repeatedly by sent/retry/ack bookkeeping, drift out of an
// updated_at-ordered window first (matching "incoming show, outgoing
// don't"). The tail window orders by id DESC, so it is immune to that
// churn.
func TestListNewestWindowReturnsMostRecent(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)

	const total = 300
	const window = 200
	base := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)

	idsInOrder := make([]uint64, 0, total)
	for i := 0; i < total; i++ {
		dir, from, to := "in", "W1ABC", "K0ABC"
		if i%2 == 1 {
			dir, from, to = "out", "K0ABC", "W1ABC"
		}
		m := seedMsg(dir, "K0ABC", from, to, fmt.Sprintf("msg-%d", i), fmt.Sprintf("%03d", i%1000))
		m.ThreadKind = ThreadKindDM
		if err := store.Insert(ctx, m); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
		idsInOrder = append(idsInOrder, m.ID)
		// updated_at strictly increasing with insertion so the default
		// forward (updated_at ASC) page is deterministic.
		ts := base.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano)
		if err := store.db.WithContext(ctx).
			Exec("UPDATE messages SET updated_at = ? WHERE id = ?", ts, m.ID).Error; err != nil {
			t.Fatalf("set updated_at: %v", err)
		}
	}
	newestID := idsInOrder[total-1]
	oldestID := idsInOrder[0]

	// Simulate ack/retry churn: the OLDEST row's updated_at jumps to the
	// most recent of all. id DESC must ignore this and still exclude it
	// from the newest window.
	churned := base.Add(time.Duration(total+1000) * time.Second).Format(time.RFC3339Nano)
	if err := store.db.WithContext(ctx).
		Exec("UPDATE messages SET updated_at = ? WHERE id = ?", churned, oldestID).Error; err != nil {
		t.Fatalf("churn updated_at: %v", err)
	}

	f := Filter{ThreadKind: ThreadKindDM, ThreadKey: "W1ABC", Newest: true, Limit: window}
	rows, cur, err := store.List(ctx, f)
	if err != nil {
		t.Fatalf("list newest: %v", err)
	}
	if cur != "" {
		t.Errorf("tail window should not return a paging cursor, got %q", cur)
	}
	if len(rows) != window {
		t.Fatalf("newest window size = %d, want %d", len(rows), window)
	}
	got := make(map[uint64]bool, len(rows))
	for _, r := range rows {
		got[r.ID] = true
	}
	// The window must be exactly the newest `window` ids by insertion.
	for _, id := range idsInOrder[total-window:] {
		if !got[id] {
			t.Errorf("newest window missing id %d (should be present)", id)
		}
	}
	// ...and must exclude older ids, including the churned oldest row.
	for _, id := range idsInOrder[:total-window] {
		if got[id] {
			t.Errorf("newest window unexpectedly contains old id %d", id)
		}
	}
	if !got[newestID] {
		t.Errorf("newest window dropped the most recent message id %d", newestID)
	}
	if got[oldestID] {
		t.Errorf("newest window included churned oldest id %d despite bumped updated_at", oldestID)
	}

	// Control: the default forward page (no Newest, no cursor) returns the
	// OLDEST-updated rows and drops the newest message -- the exact GH #521
	// symptom the tail window fixes.
	ctrl, _, err := store.List(ctx, Filter{ThreadKind: ThreadKindDM, ThreadKey: "W1ABC", Limit: window})
	if err != nil {
		t.Fatalf("list control: %v", err)
	}
	for _, r := range ctrl {
		if r.ID == newestID {
			t.Fatalf("control precondition broken: forward page already includes newest id %d", newestID)
		}
	}
}

// TestListCursorHighVolumeNoSkips drives a busy two-way thread through the
// polling client's forward-cursor loop and asserts every message is
// delivered exactly the way the chat UI would see it.
//
// The failure this guards against (GH #521): on high-traffic APRSOTA
// threads, messages eventually stop showing up in the chat window. Root
// cause was a keyset-pagination mismatch — List ordered rows by
// full-precision updated_at but the cursor predicate compared updated_at
// truncated to whole seconds. When an older row's updated_at was bumped
// into the same second as newer, higher-id inserts (routine ack
// correlation on a busy thread), the older row sorted after the cursor tip
// yet fell behind on the id tiebreak and was stranded forever.
//
// This reproduces that at volume: many rows share each whole second, and
// within every second the sub-second updated_at order is INVERTED relative
// to id — exactly what ack/retry churn produces. Before the fix the client
// silently dropped a large fraction of the thread.
func TestListCursorHighVolumeNoSkips(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)

	const total = 300 // messages in the thread
	const perSecond = 40

	want := make(map[uint64]bool, total)
	for i := 0; i < total; i++ {
		dir, from, to := "in", "W1ABC", "K0ABC"
		if i%2 == 1 { // alternate inbound/outbound like a real conversation
			dir, from, to = "out", "K0ABC", "W1ABC"
		}
		m := seedMsg(dir, "K0ABC", from, to, fmt.Sprintf("msg-%d", i), fmt.Sprintf("%02d", i%100))
		m.ThreadKind = ThreadKindDM
		if err := store.Insert(ctx, m); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
		want[m.ID] = true

		// Force updated_at: pack perSecond rows into each whole second, and
		// invert the sub-second order within a second relative to insert
		// (id) order so the lower-id row lands LATER in the second.
		sec := i / perSecond
		pos := i % perSecond
		frac := perSecond - pos // higher-id => smaller frac => earlier
		ts := fmt.Sprintf("2026-08-23T20:%02d:%02d.%09dZ", sec/60, sec%60, frac*1_000_000)
		if err := store.db.WithContext(ctx).
			Exec("UPDATE messages SET updated_at = ? WHERE id = ?", ts, m.ID).Error; err != nil {
			t.Fatalf("bump updated_at: %v", err)
		}
	}

	// Poll exactly like web/src/lib/messagesTransport.js fetchDelta: page
	// forward from an advancing cursor at the default page size until the
	// cursor stops moving.
	seen := make(map[uint64]bool, total)
	cursor := ""
	for iter := 0; iter < 1000; iter++ {
		rows, next, err := store.List(ctx, Filter{Cursor: cursor})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, r := range rows {
			seen[r.ID] = true
		}
		if next == cursor || len(rows) == 0 {
			break
		}
		cursor = next
	}

	if len(seen) != total {
		var missing []uint64
		for id := range want {
			if !seen[id] {
				missing = append(missing, id)
			}
		}
		t.Fatalf("polling client delivered %d/%d messages; %d stranded (e.g. %v)",
			len(seen), total, len(missing), firstN(missing, 10))
	}
}

func firstN(ids []uint64, n int) []uint64 {
	if len(ids) < n {
		return ids
	}
	return ids[:n]
}
