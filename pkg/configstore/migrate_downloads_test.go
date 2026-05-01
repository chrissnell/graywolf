package configstore

import (
	"context"
	"testing"
	"time"
)

func TestMigrateMapsDownloadSlugs(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, slug := range []string{"colorado", "wyoming", "state/already-namespaced"} {
		if err := store.UpsertMapsDownload(ctx, MapsDownload{
			Slug:         slug,
			Status:       "complete",
			BytesTotal:   100,
			DownloadedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.MigrateMapsDownloadSlugs(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListMapsDownloads(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r.Slug] = true
	}
	for _, want := range []string{"state/colorado", "state/wyoming", "state/already-namespaced"} {
		if !got[want] {
			t.Errorf("missing slug %q in %v", want, got)
		}
	}
	for _, bad := range []string{"colorado", "wyoming"} {
		if got[bad] {
			t.Errorf("legacy slug %q still present", bad)
		}
	}
	if err := store.MigrateMapsDownloadSlugs(ctx); err != nil {
		t.Fatalf("second run: %v", err)
	}
}

// Collision: bare and namespaced rows for the same logical slug both
// exist. Migration must drop the legacy bare row and keep the
// namespaced row, not error on the unique index.
func TestMigrateMapsDownloadSlugs_CollisionDropsLegacy(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, slug := range []string{"colorado", "state/colorado"} {
		if err := store.UpsertMapsDownload(ctx, MapsDownload{
			Slug:         slug,
			Status:       "complete",
			BytesTotal:   100,
			DownloadedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.MigrateMapsDownloadSlugs(ctx); err != nil {
		t.Fatalf("migration must succeed under collision; got %v", err)
	}
	rows, err := store.ListMapsDownloads(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r.Slug] = true
	}
	if !got["state/colorado"] {
		t.Errorf("namespaced slug missing in %v", got)
	}
	if got["colorado"] {
		t.Errorf("legacy bare slug should have been deleted; rows=%v", got)
	}
}
