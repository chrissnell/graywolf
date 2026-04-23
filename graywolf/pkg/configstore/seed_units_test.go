package configstore

import (
	"context"
	"testing"
)

func TestGetUnitsConfig_DefaultsToImperialWhenMissing(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	got, err := s.GetUnitsConfig(ctx)
	if err != nil {
		t.Fatalf("GetUnitsConfig: %v", err)
	}
	if got.ID != 0 {
		t.Fatalf("expected no row (ID=0), got %+v", got)
	}
	if got.System != "imperial" {
		t.Fatalf("expected System=imperial on fresh install, got %+v", got)
	}
}

func TestUpsertUnitsConfig_IdempotentSingleton(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.UpsertUnitsConfig(ctx, UnitsConfig{System: "metric"}); err != nil {
		t.Fatalf("first UpsertUnitsConfig: %v", err)
	}
	first, err := s.GetUnitsConfig(ctx)
	if err != nil {
		t.Fatalf("GetUnitsConfig after first upsert: %v", err)
	}
	if first.System != "metric" {
		t.Fatalf("expected System=metric after first upsert, got %+v", first)
	}
	if first.ID == 0 {
		t.Fatalf("expected row to be assigned an ID, got %+v", first)
	}

	if err := s.UpsertUnitsConfig(ctx, UnitsConfig{System: "imperial"}); err != nil {
		t.Fatalf("second UpsertUnitsConfig: %v", err)
	}
	second, err := s.GetUnitsConfig(ctx)
	if err != nil {
		t.Fatalf("GetUnitsConfig after second upsert: %v", err)
	}
	if second.System != "imperial" {
		t.Fatalf("expected System=imperial after second upsert, got %+v", second)
	}
	if second.ID != first.ID {
		t.Fatalf("expected singleton ID preserved, got %d vs %d", second.ID, first.ID)
	}

	var count int64
	if err := s.DB().Model(&UnitsConfig{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one units_configs row, got %d", count)
	}
}

func TestUpsertUnitsConfig_RejectsUnknownSystem(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.UpsertUnitsConfig(ctx, UnitsConfig{System: "parsecs"}); err == nil {
		t.Fatalf("expected error for unknown system value")
	}
}

func TestGetUnitsConfig_NormalizesUnknownStoredValue(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Bypass Upsert validation so we can exercise the read-side fallback.
	if err := s.DB().Model(&UnitsConfig{}).Create(map[string]any{
		"system": "furlongs",
	}).Error; err != nil {
		t.Fatalf("seed invalid row: %v", err)
	}
	got, err := s.GetUnitsConfig(ctx)
	if err != nil {
		t.Fatalf("GetUnitsConfig: %v", err)
	}
	if got.System != "imperial" {
		t.Fatalf("expected fallback to imperial, got %+v", got)
	}
}
