package configstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestCreateAndListAX25SessionProfiles(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "ax25_profiles.db")
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.CreateAX25SessionProfile(ctx, &AX25SessionProfile{
		Name: "BBS-A", LocalCall: "K0SWE", DestCall: "W1AW",
	}); err != nil {
		t.Fatalf("create A: %v", err)
	}
	if err := store.CreateAX25SessionProfile(ctx, &AX25SessionProfile{
		Name: "BBS-B", LocalCall: "K0SWE", DestCall: "K0BBS", Pinned: true,
	}); err != nil {
		t.Fatalf("create B: %v", err)
	}
	rows, err := store.ListAX25SessionProfiles(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if !rows[0].Pinned {
		t.Fatalf("pinned must come first, got %+v", rows[0])
	}
}

func TestPinAX25SessionProfile_PromotesAndUnpinning(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "ax25_profile_pin.db")
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	p := &AX25SessionProfile{LocalCall: "K0SWE", DestCall: "K0BBS"}
	if err := store.CreateAX25SessionProfile(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.PinAX25SessionProfile(ctx, p.ID, true); err != nil {
		t.Fatalf("pin: %v", err)
	}
	got, err := store.GetAX25SessionProfile(ctx, p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.Pinned {
		t.Fatal("pin did not stick")
	}
	if err := store.PinAX25SessionProfile(ctx, p.ID, false); err != nil {
		t.Fatalf("unpin: %v", err)
	}
	got2, _ := store.GetAX25SessionProfile(ctx, p.ID)
	if got2.Pinned {
		t.Fatal("unpin did not stick")
	}
	if err := store.PinAX25SessionProfile(ctx, 999_999, true); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing row error: got %v want ErrRecordNotFound", err)
	}
}

func TestUpsertRecentAX25SessionProfile_DeduplicatesAndCaps(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "ax25_profile_recents.db")
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	cap := 3
	// Create three distinct recents.
	for i := 0; i < 3; i++ {
		dest := []string{"A", "B", "C"}[i]
		p := &AX25SessionProfile{LocalCall: "K0SWE", DestCall: dest}
		if err := store.UpsertRecentAX25SessionProfile(ctx, p, cap); err != nil {
			t.Fatalf("upsert %d: %v", i, err)
		}
		// Stagger LastUsed in the past so the natural order is A
		// (oldest) -> B -> C (newest). UpsertRecent stamps now() on
		// re-touch and on insert, which is newer than every staggered
		// timestamp here.
		then := time.Now().UTC().Add(-time.Duration(3-i) * time.Hour)
		if err := store.TouchAX25SessionProfileLastUsed(ctx, p.ID, then); err != nil {
			t.Fatalf("touch %d: %v", i, err)
		}
	}
	// Re-upsert "A" with the same fields: should NOT create a new row.
	dup := &AX25SessionProfile{LocalCall: "K0SWE", DestCall: "A"}
	if err := store.UpsertRecentAX25SessionProfile(ctx, dup, cap); err != nil {
		t.Fatalf("dup upsert: %v", err)
	}
	rows, err := store.ListAX25SessionProfiles(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("dedup failed, got %d", len(rows))
	}
	// Add a fourth recent: trim should drop the oldest LastUsed (B was stamped at i=1).
	p4 := &AX25SessionProfile{LocalCall: "K0SWE", DestCall: "D"}
	if err := store.UpsertRecentAX25SessionProfile(ctx, p4, cap); err != nil {
		t.Fatalf("4th upsert: %v", err)
	}
	rows2, _ := store.ListAX25SessionProfiles(ctx)
	if len(rows2) != cap {
		t.Fatalf("cap not enforced: %d rows", len(rows2))
	}
	// After dup upsert pushes A's LastUsed to now and D arrives with
	// now: order desc is D, A, C, B. Cap=3 keeps top three; B is the
	// oldest survivor and must be trimmed.
	for _, r := range rows2 {
		if r.DestCall == "B" {
			t.Fatal("expected oldest recent ('B', stamped 2h ago) to be trimmed")
		}
	}
}

func TestUpsertRecentAX25SessionProfile_PinnedSurvivesTrim(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "ax25_profile_pinned_survives.db")
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	pinned := &AX25SessionProfile{Name: "Pinned", LocalCall: "K0SWE", DestCall: "P", Pinned: true}
	if err := store.CreateAX25SessionProfile(ctx, pinned); err != nil {
		t.Fatalf("create pinned: %v", err)
	}
	// Stamp ancient LastUsed so it'd get trimmed if it weren't pinned.
	ancient := time.Unix(0, 0)
	if err := store.TouchAX25SessionProfileLastUsed(ctx, pinned.ID, ancient); err != nil {
		t.Fatalf("touch pinned: %v", err)
	}
	cap := 1
	for i := 0; i < 3; i++ {
		dest := []string{"A", "B", "C"}[i]
		p := &AX25SessionProfile{LocalCall: "K0SWE", DestCall: dest}
		if err := store.UpsertRecentAX25SessionProfile(ctx, p, cap); err != nil {
			t.Fatalf("upsert %d: %v", i, err)
		}
	}
	rows, err := store.ListAX25SessionProfiles(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// Expect the pinned row + cap unpinned rows.
	if len(rows) != 1+cap {
		t.Fatalf("pinned + cap should yield %d rows, got %d (%+v)", 1+cap, len(rows), rows)
	}
	gotPinned := false
	for _, r := range rows {
		if r.Pinned {
			gotPinned = true
		}
	}
	if !gotPinned {
		t.Fatal("pinned row was trimmed")
	}
}

func TestDeleteAX25SessionProfile_Idempotent(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "ax25_profile_delete.db")
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	p := &AX25SessionProfile{LocalCall: "K0SWE", DestCall: "K0BBS"}
	if err := store.CreateAX25SessionProfile(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.DeleteAX25SessionProfile(ctx, p.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := store.DeleteAX25SessionProfile(ctx, p.ID); err != nil {
		t.Fatalf("idempotent delete: %v", err)
	}
}
