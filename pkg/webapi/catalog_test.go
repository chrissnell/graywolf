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
