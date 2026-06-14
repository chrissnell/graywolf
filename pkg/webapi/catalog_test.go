package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/mapscatalog"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func TestGetCatalog_OK(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(mapscatalog.Catalog{
			SchemaVersion: 1,
			GeneratedAt:   "2026-04-30T00:00:00Z",
			Countries:     []mapscatalog.Country{{ISO2: "de", Name: "Germany", SizeBytes: 1, SHA256: "x"}},
		})
	}))
	defer upstream.Close()

	srv, _ := newTestServer(t)
	srv.catalog = mapscatalog.New(upstream.URL, func(_ context.Context) string { return "tok" }, time.Hour)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/maps/catalog", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out dto.Catalog
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.SchemaVersion != 1 || len(out.Countries) != 1 {
		t.Fatalf("unexpected: %+v", out)
	}
}

func TestGetCatalog_ServiceUnavailable(t *testing.T) {
	srv, _ := newTestServer(t)
	// no catalog injected
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/maps/catalog", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestGetCatalog_Demo(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.demo = true

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/maps/catalog", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out dto.Catalog
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.SchemaVersion != 1 {
		t.Fatalf("expected SchemaVersion=1, got %d", out.SchemaVersion)
	}
	if len(out.Countries) != 0 {
		t.Fatalf("expected empty countries, got %d", len(out.Countries))
	}
	if len(out.Provinces) != 0 {
		t.Fatalf("expected empty provinces, got %d", len(out.Provinces))
	}
	if len(out.States) != 0 {
		t.Fatalf("expected empty states, got %d", len(out.States))
	}
}

// TestToCatalogDTO_PassesWorldThrough guards the bug where the world
// entry was present in mapscatalog.Catalog but dropped by toCatalogDTO,
// so the picker (which reads /api/maps/catalog) never saw it.
func TestToCatalogDTO_PassesWorldThrough(t *testing.T) {
	in := mapscatalog.Catalog{
		SchemaVersion: 1,
		World: &mapscatalog.WorldMap{
			Name: "World (low detail)", SizeBytes: 314572800, SHA256: "abc",
			BBox: &[4]float64{-180, -85.05, 180, 85.05}, MaxZoom: 7,
		},
	}
	out := toCatalogDTO(in)
	if out.World == nil {
		t.Fatal("toCatalogDTO dropped World; picker would never render the world row")
	}
	if out.World.Name != "World (low detail)" || out.World.MaxZoom != 7 || out.World.SizeBytes != 314572800 {
		t.Fatalf("world fields not carried: %+v", *out.World)
	}
	// And it must survive JSON to the exact key the frontend reads.
	b, _ := json.Marshal(out)
	var wire map[string]json.RawMessage
	_ = json.Unmarshal(b, &wire)
	if _, ok := wire["world"]; !ok {
		t.Fatalf("serialized catalog has no \"world\" key: %s", b)
	}
	// Absent world stays omitted.
	if toCatalogDTO(mapscatalog.Catalog{SchemaVersion: 1}).World != nil {
		t.Fatal("World should be nil when absent")
	}
}
