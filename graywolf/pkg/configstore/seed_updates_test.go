package configstore

import (
	"context"
	"testing"
)

// TestGetUpdatesConfig_DefaultsToEnabledWhenMissing asserts the
// defaults-to-on contract: on a fresh install with no row, GetUpdatesConfig
// returns Enabled=true (not the Go zero value) so the feature is on
// out of the box without a separate seed step.
func TestGetUpdatesConfig_DefaultsToEnabledWhenMissing(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	got, err := s.GetUpdatesConfig(ctx)
	if err != nil {
		t.Fatalf("GetUpdatesConfig: %v", err)
	}
	if got.ID != 0 {
		t.Fatalf("expected no row (ID=0), got %+v", got)
	}
	if !got.Enabled {
		t.Fatalf("expected Enabled=true on fresh install, got %+v", got)
	}
}

// TestUpsertUpdatesConfig_IdempotentSingleton asserts the singleton
// contract: two upserts with different values leave exactly one row,
// and the stored value is the second upsert's value (not the first).
func TestUpsertUpdatesConfig_IdempotentSingleton(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.UpsertUpdatesConfig(ctx, UpdatesConfig{Enabled: false}); err != nil {
		t.Fatalf("first UpsertUpdatesConfig: %v", err)
	}
	first, err := s.GetUpdatesConfig(ctx)
	if err != nil {
		t.Fatalf("GetUpdatesConfig after first upsert: %v", err)
	}
	if first.Enabled {
		t.Fatalf("expected Enabled=false after first upsert, got %+v", first)
	}
	if first.ID == 0 {
		t.Fatalf("expected row to be assigned an ID, got %+v", first)
	}

	// Second upsert with ID=0: must adopt existing row rather than
	// insert a duplicate.
	if err := s.UpsertUpdatesConfig(ctx, UpdatesConfig{Enabled: true}); err != nil {
		t.Fatalf("second UpsertUpdatesConfig: %v", err)
	}
	second, err := s.GetUpdatesConfig(ctx)
	if err != nil {
		t.Fatalf("GetUpdatesConfig after second upsert: %v", err)
	}
	if !second.Enabled {
		t.Fatalf("expected Enabled=true after second upsert, got %+v", second)
	}
	if second.ID != first.ID {
		t.Fatalf("expected singleton ID preserved, got %d vs %d", second.ID, first.ID)
	}

	var count int64
	if err := s.DB().Model(&UpdatesConfig{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one updates_configs row, got %d", count)
	}
}

// TestUpsertUpdatesConfig_TrueToFalseRoundTrip exercises the exact
// scenario that motivated the map-based UpdateColumns approach in
// UpsertUpdatesConfig: writing Enabled=true first, then toggling to
// Enabled=false. GORM's default handling silently rewrites a bool
// zero-value to the column default (here: true), so a naive Save(&c)
// on the second upsert would persist Enabled=true rather than false.
// This test guards against any future refactor that drops the
// map-based write.
func TestUpsertUpdatesConfig_TrueToFalseRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.UpsertUpdatesConfig(ctx, UpdatesConfig{Enabled: true}); err != nil {
		t.Fatalf("first UpsertUpdatesConfig: %v", err)
	}
	if err := s.UpsertUpdatesConfig(ctx, UpdatesConfig{Enabled: false}); err != nil {
		t.Fatalf("second UpsertUpdatesConfig: %v", err)
	}
	got, err := s.GetUpdatesConfig(ctx)
	if err != nil {
		t.Fatalf("GetUpdatesConfig: %v", err)
	}
	if got.Enabled {
		t.Fatalf("expected Enabled=false after true->false round-trip, got %+v", got)
	}
}
