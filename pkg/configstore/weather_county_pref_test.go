package configstore

import (
	"context"
	"testing"
)

func TestUpsertWeatherCountyPref(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Enable a county.
	if err := s.UpsertWeatherCountyPref(ctx, "51191", true); err != nil {
		t.Fatalf("UpsertWeatherCountyPref(true): %v", err)
	}
	prefs, err := s.ListWeatherCountyPrefs(ctx)
	if err != nil {
		t.Fatalf("ListWeatherCountyPrefs: %v", err)
	}
	if !prefs["51191"] {
		t.Error("expected 51191 to have AllowTX=true")
	}

	// Re-enabling must be idempotent (ON CONFLICT path).
	if err := s.UpsertWeatherCountyPref(ctx, "51191", true); err != nil {
		t.Fatalf("UpsertWeatherCountyPref(true) idempotent: %v", err)
	}

	// Disable: should delete the row.
	if err := s.UpsertWeatherCountyPref(ctx, "51191", false); err != nil {
		t.Fatalf("UpsertWeatherCountyPref(false): %v", err)
	}
	prefs2, _ := s.ListWeatherCountyPrefs(ctx)
	if prefs2["51191"] {
		t.Error("expected 51191 to be removed after AllowTX=false")
	}
}
