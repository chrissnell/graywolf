package configstore

import (
	"context"
	"testing"
)

func TestGetThemeConfig_DefaultsToGraywolfWhenMissing(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	got, err := s.GetThemeConfig(ctx)
	if err != nil {
		t.Fatalf("GetThemeConfig: %v", err)
	}
	if got.ID != 0 {
		t.Fatalf("expected no row (ID=0), got %+v", got)
	}
	if got.ThemeID != "graywolf" {
		t.Fatalf("expected ThemeID=graywolf on fresh install, got %+v", got)
	}
}

func TestUpsertThemeConfig_IdempotentSingleton(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.UpsertThemeConfig(ctx, ThemeConfig{ThemeID: "chonky"}); err != nil {
		t.Fatalf("first UpsertThemeConfig: %v", err)
	}
	first, err := s.GetThemeConfig(ctx)
	if err != nil {
		t.Fatalf("GetThemeConfig after first upsert: %v", err)
	}
	if first.ThemeID != "chonky" {
		t.Fatalf("expected ThemeID=chonky after first upsert, got %+v", first)
	}
	if first.ID == 0 {
		t.Fatalf("expected row to be assigned an ID, got %+v", first)
	}

	if err := s.UpsertThemeConfig(ctx, ThemeConfig{ThemeID: "grayscale-night"}); err != nil {
		t.Fatalf("second UpsertThemeConfig: %v", err)
	}
	second, err := s.GetThemeConfig(ctx)
	if err != nil {
		t.Fatalf("GetThemeConfig after second upsert: %v", err)
	}
	if second.ThemeID != "grayscale-night" {
		t.Fatalf("expected ThemeID=grayscale-night after second upsert, got %+v", second)
	}
	if second.ID != first.ID {
		t.Fatalf("expected singleton ID preserved, got %d vs %d", second.ID, first.ID)
	}

	var count int64
	if err := s.DB().Model(&ThemeConfig{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one theme_configs row, got %d", count)
	}
}

func TestUpsertThemeConfig_RejectsMalformedID(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	for _, bad := range []string{
		"",                     // empty
		"UPPERCASE",            // uppercase
		"under_score",          // underscore
		"has space",            // space
		"path/traversal",       // slash
		"../etc",               // traversal
		"a" + string(make([]byte, 64)), // >64 chars
	} {
		if err := s.UpsertThemeConfig(ctx, ThemeConfig{ThemeID: bad}); err == nil {
			t.Errorf("expected error for malformed id %q", bad)
		}
	}
}

func TestGetThemeConfig_AllowsUnknownButWellFormedIDs(t *testing.T) {
	// If a PR adds a theme, sets it, then is reverted, the DB still has
	// the id. We want the server to return it verbatim — the frontend
	// will handle fallback to default. Anything matching the regex is
	// legal to store and read.
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.UpsertThemeConfig(ctx, ThemeConfig{ThemeID: "field-day-2026"}); err != nil {
		t.Fatalf("UpsertThemeConfig for well-formed unknown id: %v", err)
	}
	got, err := s.GetThemeConfig(ctx)
	if err != nil {
		t.Fatalf("GetThemeConfig: %v", err)
	}
	if got.ThemeID != "field-day-2026" {
		t.Errorf("ThemeID = %q, want %q (server stores verbatim)", got.ThemeID, "field-day-2026")
	}
}
