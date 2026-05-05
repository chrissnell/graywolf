package remoteactions

import (
	"context"
	"testing"
)

func TestMacroStoreCreateListByTarget(t *testing.T) {
	db := newTestDB(t)
	ms := NewMacroStore(db)
	ctx := context.Background()

	for i, tgt := range []string{"KK7XYZ-9", "W7ABC", "KK7XYZ-9"} {
		m := &RemoteActionMacro{
			TargetCall: tgt,
			Label:      "macro",
			ActionName: "act",
			Position:   i,
		}
		if err := ms.Create(ctx, m); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	got, err := ms.ListByTarget(ctx, "KK7XYZ-9")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 KK7XYZ-9 macros, got %d", len(got))
	}
	if got[0].Position > got[1].Position {
		t.Fatalf("not sorted by position: %+v", got)
	}
}

func TestMacroStoreUpdate(t *testing.T) {
	db := newTestDB(t)
	ms := NewMacroStore(db)
	ctx := context.Background()
	m := &RemoteActionMacro{TargetCall: "KK7XYZ-9", Label: "old", ActionName: "x"}
	if err := ms.Create(ctx, m); err != nil {
		t.Fatalf("create: %v", err)
	}
	m.Label = "new"
	m.ArgsString = "door=front"
	if err := ms.Update(ctx, m); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := ms.Get(ctx, m.ID)
	if got.Label != "new" || got.ArgsString != "door=front" {
		t.Fatalf("update did not persist: %+v", got)
	}
}

func TestMacroStoreReorder(t *testing.T) {
	db := newTestDB(t)
	ms := NewMacroStore(db)
	ctx := context.Background()
	var ids []uint
	for i := 0; i < 3; i++ {
		m := &RemoteActionMacro{TargetCall: "K", Label: "x", ActionName: "x", Position: i}
		if err := ms.Create(ctx, m); err != nil {
			t.Fatalf("create: %v", err)
		}
		ids = append(ids, m.ID)
	}
	// reverse
	if err := ms.Reorder(ctx, "K", []uint{ids[2], ids[1], ids[0]}); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	got, _ := ms.ListByTarget(ctx, "K")
	if got[0].ID != ids[2] || got[2].ID != ids[0] {
		t.Fatalf("reorder did not stick: %+v", got)
	}
}

// RemoteActionMacro.BeforeSave normalizes ActionName to uppercase so
// outbound fires send the canonical wire form. Mixed-case input must
// round-trip as uppercase regardless of how the operator typed it.
func TestMacroStoreUppercasesActionName(t *testing.T) {
	db := newTestDB(t)
	ms := NewMacroStore(db)
	ctx := context.Background()
	m := &RemoteActionMacro{TargetCall: "KK7XYZ-9", Label: "x", ActionName: "  Unlock  "}
	if err := ms.Create(ctx, m); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, _ := ms.Get(ctx, m.ID)
	if got.ActionName != "UNLOCK" {
		t.Fatalf("ActionName = %q, want UNLOCK", got.ActionName)
	}
}

func TestMacroStoreDelete(t *testing.T) {
	db := newTestDB(t)
	ms := NewMacroStore(db)
	ctx := context.Background()
	m := &RemoteActionMacro{TargetCall: "K", Label: "x", ActionName: "x"}
	if err := ms.Create(ctx, m); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := ms.Delete(ctx, m.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := ms.Get(ctx, m.ID); err == nil {
		t.Fatalf("expected not-found")
	}
}
