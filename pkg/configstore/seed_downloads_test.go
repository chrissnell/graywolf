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
