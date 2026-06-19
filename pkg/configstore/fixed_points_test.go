package configstore

import (
	"context"
	"testing"
)

func TestFixedPointCRUDRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	fp := &FixedPoint{
		Name: "Aid Station 3", SymbolTable: "/", Symbol: "a",
		Overlay: "", Latitude: 37.5, Longitude: -122.0,
	}
	if err := s.CreateFixedPoint(ctx, fp); err != nil {
		t.Fatalf("create: %v", err)
	}
	if fp.ID == 0 {
		t.Fatalf("expected assigned id, got 0")
	}

	got, err := s.GetFixedPoint(ctx, fp.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Aid Station 3" || got.Latitude != 37.5 || got.Longitude != -122.0 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	got.Name = "Aid Station 4"
	if err := s.UpdateFixedPoint(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}

	all, err := s.ListFixedPoints(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 || all[0].Name != "Aid Station 4" {
		t.Fatalf("list mismatch: %+v", all)
	}

	if err := s.DeleteFixedPoint(ctx, fp.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	all, err = s.ListFixedPoints(ctx)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("expected empty after delete, got %+v", all)
	}
}
