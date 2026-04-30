package configstore

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestGetMapsConfig_DefaultsWhenEmpty(t *testing.T) {
	s := newTestStore(t)
	c, err := s.GetMapsConfig(context.Background())
	if err != nil {
		t.Fatalf("GetMapsConfig: %v", err)
	}
	if c.Source != "graywolf" {
		t.Fatalf("default source = %q, want %q", c.Source, "graywolf")
	}
	if c.Callsign != "" || c.Token != "" {
		t.Fatalf("expected empty callsign/token on fresh install, got %+v", c)
	}
}

func TestUpsertMapsConfig_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	in := MapsConfig{
		Source:       "graywolf",
		Callsign:     "N5XXX",
		Token:        "GKHkfi0a51nVZbiu_eJ7AqZ3YFvZY43Pvq4jOFZWDf0",
		RegisteredAt: now,
	}
	if err := s.UpsertMapsConfig(ctx, in); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := s.GetMapsConfig(ctx)
	if err != nil {
		t.Fatalf("Get after upsert: %v", err)
	}
	if got.Source != "graywolf" || got.Callsign != "N5XXX" || got.Token != in.Token {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if !got.RegisteredAt.Equal(now) {
		t.Fatalf("RegisteredAt mismatch: got %v, want %v", got.RegisteredAt, now)
	}
}

func TestUpsertMapsConfig_RejectsBadSource(t *testing.T) {
	s := newTestStore(t)
	err := s.UpsertMapsConfig(context.Background(), MapsConfig{Source: "google"})
	if err == nil {
		t.Fatal("expected error for unknown source, got nil")
	}
	if !strings.Contains(err.Error(), "source must be") {
		t.Fatalf("error = %v, want contains 'source must be'", err)
	}
}

func TestUpsertMapsConfig_PreservesIDAcrossUpserts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.UpsertMapsConfig(ctx, MapsConfig{Source: "osm"}); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	first, _ := s.GetMapsConfig(ctx)
	if err := s.UpsertMapsConfig(ctx, MapsConfig{Source: "graywolf", Callsign: "N5XXX", Token: "abc"}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	second, _ := s.GetMapsConfig(ctx)
	if second.ID != first.ID {
		t.Fatalf("singleton invariant broken: id %d -> %d", first.ID, second.ID)
	}
	var count int64
	if err := s.DB().Model(&MapsConfig{}).Count(&count).Error; err != nil {
		t.Fatalf("count maps_configs: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 row after two upserts, got %d", count)
	}
}
