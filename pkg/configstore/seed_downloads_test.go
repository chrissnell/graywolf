package configstore

import (
	"context"
	"testing"
	"time"
)

func TestListMapsDownloads_EmptyByDefault(t *testing.T) {
	s := newTestStore(t)
	got, err := s.ListMapsDownloads(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 rows on fresh install, got %d", len(got))
	}
}

func TestUpsertMapsDownload_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	in := MapsDownload{
		Slug:            "georgia",
		Status:          "complete",
		BytesTotal:      52_000_000,
		BytesDownloaded: 52_000_000,
		DownloadedAt:    now,
	}
	if err := s.UpsertMapsDownload(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMapsDownload(ctx, "georgia")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "complete" || got.BytesTotal != 52_000_000 || got.Slug != "georgia" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestUpsertMapsDownload_RejectsBadStatus(t *testing.T) {
	s := newTestStore(t)
	err := s.UpsertMapsDownload(context.Background(), MapsDownload{
		Slug:   "georgia",
		Status: "weird",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestDeleteMapsDownload_RemovesRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_ = s.UpsertMapsDownload(ctx, MapsDownload{Slug: "texas", Status: "complete"})
	if err := s.DeleteMapsDownload(ctx, "texas"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetMapsDownload(ctx, "texas")
	if got.ID != 0 {
		t.Fatalf("expected row gone, got %+v", got)
	}
}

func TestUpsertMapsDownload_SecondCallUpdatesNotInserts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_ = s.UpsertMapsDownload(ctx, MapsDownload{Slug: "ohio", Status: "downloading", BytesDownloaded: 1024})
	first, _ := s.GetMapsDownload(ctx, "ohio")
	_ = s.UpsertMapsDownload(ctx, MapsDownload{Slug: "ohio", Status: "complete", BytesDownloaded: 99000})
	second, _ := s.GetMapsDownload(ctx, "ohio")
	if first.ID != second.ID {
		t.Fatalf("uniqueIndex on slug should have updated row, ID changed: %d -> %d", first.ID, second.ID)
	}
	if second.Status != "complete" {
		t.Fatalf("status not updated: %q", second.Status)
	}
}

// TestUpsertMapsDownload_NilBBoxPreservesExisting locks in the
// status-transition contract: once Start has snapshotted a bbox into
// the row, subsequent upserts that don't populate BBox (m.fail, the
// in-run "downloading" Content-Length update, and the final "complete"
// transition) must NOT wipe the bbox column. A regression here means
// every completed download serves a NULL bbox to /api/maps/local-bounds,
// silently breaking offline render across reboots.
func TestUpsertMapsDownload_NilBBoxPreservesExisting(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	bbox := `[-109.05,36.99,-102.04,41]`

	if err := s.UpsertMapsDownload(ctx, MapsDownload{
		Slug: "state/colorado", Status: "downloading", BBox: &bbox,
	}); err != nil {
		t.Fatalf("initial upsert: %v", err)
	}

	// Transition without populating BBox -- this is what manager.fail()
	// and the in-run "complete" path do.
	if err := s.UpsertMapsDownload(ctx, MapsDownload{
		Slug: "state/colorado", Status: "complete",
	}); err != nil {
		t.Fatalf("transition upsert: %v", err)
	}

	got, err := s.GetMapsDownload(ctx, "state/colorado")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != "complete" {
		t.Fatalf("status: got %q want complete", got.Status)
	}
	if got.BBox == nil {
		t.Fatalf("bbox was wiped by status transition")
	}
	if *got.BBox != bbox {
		t.Fatalf("bbox: got %q want %q", *got.BBox, bbox)
	}
}

// TestUpsertMapsDownload_NilBBoxOnFreshInsertStaysNull covers the
// converse: a slug whose first-ever Upsert has no BBox (Start was
// called with bbox=nil because the catalog had no bbox on that
// region) lands with BBox=NULL and stays NULL across subsequent
// transitions. The startup backfill is the only thing allowed to
// populate it from there.
func TestUpsertMapsDownload_NilBBoxOnFreshInsertStaysNull(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.UpsertMapsDownload(ctx, MapsDownload{
		Slug: "state/wyoming", Status: "downloading",
	}); err != nil {
		t.Fatalf("initial upsert: %v", err)
	}
	if err := s.UpsertMapsDownload(ctx, MapsDownload{
		Slug: "state/wyoming", Status: "complete",
	}); err != nil {
		t.Fatalf("transition upsert: %v", err)
	}

	got, _ := s.GetMapsDownload(ctx, "state/wyoming")
	if got.BBox != nil {
		t.Fatalf("expected NULL bbox throughout, got %q", *got.BBox)
	}
}

func TestUpsertMapsDownloadMaxZoom(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.UpsertMapsDownload(ctx, MapsDownload{
		Slug: "world", Status: "complete", MaxZoom: 7,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMapsDownload(ctx, "world")
	if err != nil {
		t.Fatal(err)
	}
	if got.MaxZoom != 7 {
		t.Fatalf("MaxZoom = %d, want 7", got.MaxZoom)
	}
	// A later status-transition upsert (fresh row, MaxZoom==0) must NOT
	// wipe the snapshotted cap.
	if err := s.UpsertMapsDownload(ctx, MapsDownload{
		Slug: "world", Status: "downloading",
	}); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetMapsDownload(ctx, "world")
	if err != nil {
		t.Fatal(err)
	}
	if got.MaxZoom != 7 {
		t.Fatalf("MaxZoom after status transition = %d, want 7 (must not be wiped)", got.MaxZoom)
	}
}
