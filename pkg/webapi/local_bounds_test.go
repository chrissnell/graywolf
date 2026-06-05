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

	var got map[string][4]float64
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len: got %d want 1; payload=%v", len(got), got)
	}
	want := [4]float64{-109.05, 36.99, -102.04, 41}
	if got["state/colorado"] != want {
		t.Fatalf("bbox: got %v want %v", got["state/colorado"], want)
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
