package configstore

import (
	"context"
	"testing"
)

// TestMessagePreferences_PreUpgradeRowKeepsDefaultCap asserts that a
// row written before the MaxMessageTextOverride column existed (i.e.
// a row where the new column reads zero after GORM AutoMigrate adds
// it) keeps enforcing the default 67-char cap. The webapi-layer
// helper `MessagePreferencesFromModel` also normalizes out-of-range
// stored values, but the bedrock invariant is "zero == default" at
// the storage layer.
func TestMessagePreferences_PreUpgradeRowKeepsDefaultCap(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Simulate a pre-upgrade row: FallbackPolicy/DefaultPath set but
	// MaxMessageTextOverride untouched (zero, as AutoMigrate's column
	// default is 0). Use raw Create rather than UpsertMessagePreferences
	// so we write exactly the columns a legacy row would have carried.
	if err := s.db.WithContext(ctx).Create(&MessagePreferences{
		FallbackPolicy:   "is_fallback",
		DefaultPath:      "WIDE1-1,WIDE2-1",
		RetryMaxAttempts: 4,
		RetentionDays:    0,
	}).Error; err != nil {
		t.Fatalf("seed pre-upgrade row: %v", err)
	}

	got, err := s.GetMessagePreferences(ctx)
	if err != nil {
		t.Fatalf("GetMessagePreferences: %v", err)
	}
	if got == nil {
		t.Fatal("expected a row, got nil")
	}
	if got.MaxMessageTextOverride != 0 {
		t.Errorf("pre-upgrade MaxMessageTextOverride = %d, want 0 (default)",
			got.MaxMessageTextOverride)
	}
}

// TestMessagePreferences_UpsertRoundTripsOverride guards against a
// silent drop of the new column in the Upsert path.
func TestMessagePreferences_UpsertRoundTripsOverride(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	cfg := &MessagePreferences{
		FallbackPolicy:         "is_fallback",
		DefaultPath:            "WIDE1-1,WIDE2-1",
		RetryMaxAttempts:       4,
		MaxMessageTextOverride: 200,
	}
	if err := s.UpsertMessagePreferences(ctx, cfg); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := s.GetMessagePreferences(ctx)
	if err != nil {
		t.Fatalf("GetMessagePreferences: %v", err)
	}
	if got == nil {
		t.Fatal("expected singleton row, got nil")
	}
	if got.MaxMessageTextOverride != 200 {
		t.Errorf("round-trip MaxMessageTextOverride = %d, want 200",
			got.MaxMessageTextOverride)
	}

	// Flip back to default (0) — the Save path must persist the zero,
	// not silently preserve the prior value.
	got.MaxMessageTextOverride = 0
	if err := s.UpsertMessagePreferences(ctx, got); err != nil {
		t.Fatalf("Upsert back to default: %v", err)
	}
	after, err := s.GetMessagePreferences(ctx)
	if err != nil {
		t.Fatalf("GetMessagePreferences after reset: %v", err)
	}
	if after.MaxMessageTextOverride != 0 {
		t.Errorf("after reset MaxMessageTextOverride = %d, want 0",
			after.MaxMessageTextOverride)
	}
}
