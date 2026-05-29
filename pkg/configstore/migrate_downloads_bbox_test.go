package configstore

import (
	"context"
	"testing"
)

func TestMigrateMapsDownloadBBox_AddsColumnAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	if err := store.MigrateMapsDownloadBBox(ctx); err != nil {
		t.Fatalf("first migration: %v", err)
	}
	if err := store.MigrateMapsDownloadBBox(ctx); err != nil {
		t.Fatalf("second migration: %v", err)
	}

	bbox := `[-109.05,36.99,-102.04,41.0]`
	if err := store.UpsertMapsDownload(ctx, MapsDownload{
		Slug: "state/colorado", Status: "complete", BBox: &bbox,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := store.GetMapsDownload(ctx, "state/colorado")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.BBox == nil || *got.BBox != bbox {
		t.Fatalf("bbox: got %v want %q", got.BBox, bbox)
	}
}

func TestMigrateMapsDownloadBBox_LegacyRowsRemainNull(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	// Insert a pre-bbox row by going through Upsert without setting BBox.
	if err := store.UpsertMapsDownload(ctx, MapsDownload{
		Slug: "state/utah", Status: "complete",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := store.MigrateMapsDownloadBBox(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	got, err := store.GetMapsDownload(ctx, "state/utah")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.BBox != nil {
		t.Fatalf("expected NULL bbox for legacy row, got %q", *got.BBox)
	}
}
