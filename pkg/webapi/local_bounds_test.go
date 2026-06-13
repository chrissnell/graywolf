package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

func TestLocalBounds_ReturnsCompletedSlugs(t *testing.T) {
	ctx := context.Background()
	srv, _ := newTestServer(t)
	store := srv.store

	bboxCO := `[-109.05,36.99,-102.04,41]`
	if err := store.UpsertMapsDownload(ctx, configstore.MapsDownload{
		Slug: "state/colorado", Status: "complete", BBox: &bboxCO,
	}); err != nil {
		t.Fatalf("upsert co: %v", err)
	}
	// World archive — completed, capped at z7.
	bboxWorld := `[-180,-85.05,180,85.05]`
	if err := store.UpsertMapsDownload(ctx, configstore.MapsDownload{
		Slug: "world", Status: "complete", BBox: &bboxWorld, MaxZoom: 7,
	}); err != nil {
		t.Fatalf("upsert world: %v", err)
	}
	// In-progress row — excluded.
	if err := store.UpsertMapsDownload(ctx, configstore.MapsDownload{
		Slug: "state/utah", Status: "downloading",
	}); err != nil {
		t.Fatalf("upsert ut: %v", err)
	}
	// Complete-but-null-bbox row — excluded (legacy row whose backfill
	// hasn't run yet).
	if err := store.UpsertMapsDownload(ctx, configstore.MapsDownload{
		Slug: "state/wyoming", Status: "complete",
	}); err != nil {
		t.Fatalf("upsert wy: %v", err)
	}

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/maps/local-bounds", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200; body=%s", rec.Code, rec.Body.String())
	}

	var got map[string]struct {
		BBox    [4]float64 `json:"bbox"`
		MaxZoom int        `json:"maxZoom"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len: got %d want 2; payload=%v", len(got), got)
	}
	want := [4]float64{-109.05, 36.99, -102.04, 41}
	if got["state/colorado"].BBox != want {
		t.Fatalf("bbox: got %v want %v", got["state/colorado"].BBox, want)
	}
	if got["state/colorado"].MaxZoom != 0 {
		t.Fatalf("regional maxZoom: got %d want 0", got["state/colorado"].MaxZoom)
	}
	if got["world"].MaxZoom != 7 {
		t.Fatalf("world maxZoom: got %d want 7", got["world"].MaxZoom)
	}
}

func TestLocalBounds_EmptyDatabaseReturnsEmptyMap(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/maps/local-bounds", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rec.Code)
	}
	body := strings.TrimSpace(rec.Body.String())
	if body != "{}" {
		t.Fatalf("body: got %q want \"{}\"", body)
	}
}
