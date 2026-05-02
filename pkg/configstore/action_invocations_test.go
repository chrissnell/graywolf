package configstore

import (
	"context"
	"testing"
	"time"
)

func TestInvocationInsertAndList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		row := &ActionInvocation{
			ActionNameAt: "X", SenderCall: "NW5W-7", Source: "rf",
			Status: "ok", CreatedAt: time.Now().UTC(),
		}
		if err := s.InsertActionInvocation(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.ListActionInvocations(ctx, ActionInvocationFilter{Limit: 10})
	if err != nil || len(rows) != 5 {
		t.Fatalf("list: %d %v", len(rows), err)
	}
}

func TestInvocationListFilters(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	rows := []*ActionInvocation{
		{ActionNameAt: "fire-the-laser", SenderCall: "NW5W-7", Source: "rf", Status: "ok", CreatedAt: now},
		{ActionNameAt: "fire-the-laser", SenderCall: "K0XYZ", Source: "is", Status: "ratelimited", CreatedAt: now},
		{ActionNameAt: "open-the-door", SenderCall: "NW5W-7", Source: "rf", Status: "ok", StatusDetail: "matched allowlist", CreatedAt: now},
	}
	for _, r := range rows {
		if err := s.InsertActionInvocation(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.ListActionInvocations(ctx, ActionInvocationFilter{Status: "ratelimited", Limit: 10})
	if err != nil || len(got) != 1 || got[0].Status != "ratelimited" {
		t.Fatalf("status filter: %+v err=%v", got, err)
	}

	got, err = s.ListActionInvocations(ctx, ActionInvocationFilter{Source: "rf", Limit: 10})
	if err != nil || len(got) != 2 {
		t.Fatalf("source filter: %d %v", len(got), err)
	}

	got, err = s.ListActionInvocations(ctx, ActionInvocationFilter{SenderCall: "NW5W-7", Limit: 10})
	if err != nil || len(got) != 2 {
		t.Fatalf("sender filter: %d %v", len(got), err)
	}

	got, err = s.ListActionInvocations(ctx, ActionInvocationFilter{Search: "allowlist", Limit: 10})
	if err != nil || len(got) != 1 || got[0].ActionNameAt != "open-the-door" {
		t.Fatalf("search filter: %+v %v", got, err)
	}
}

func TestInvocationPrune(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	old := time.Now().UTC().Add(-40 * 24 * time.Hour)
	recent := time.Now().UTC()
	for i := 0; i < 3; i++ {
		_ = s.InsertActionInvocation(ctx, &ActionInvocation{
			ActionNameAt: "old", Status: "ok", CreatedAt: old,
		})
	}
	for i := 0; i < 3; i++ {
		_ = s.InsertActionInvocation(ctx, &ActionInvocation{
			ActionNameAt: "new", Status: "ok", CreatedAt: recent,
		})
	}
	n, err := s.PruneActionInvocations(ctx, 1000, 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("expected 3 pruned by age, got %d", n)
	}
	all, _ := s.ListActionInvocations(ctx, ActionInvocationFilter{Limit: 100})
	if len(all) != 3 {
		t.Fatalf("expected 3 remaining, got %d", len(all))
	}

	for i := 0; i < 1010; i++ {
		_ = s.InsertActionInvocation(ctx, &ActionInvocation{
			ActionNameAt: "burst", Status: "ok", CreatedAt: recent,
		})
	}
	if _, err := s.PruneActionInvocations(ctx, 1000, 30*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	all, _ = s.ListActionInvocations(ctx, ActionInvocationFilter{Limit: 2000})
	if len(all) > 1000 {
		t.Fatalf("count cap not enforced: %d", len(all))
	}
}
