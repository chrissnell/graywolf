package webapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// newExclusionsTestServer wires a Server with the real store and the
// exclusions routes registered, mirroring newFavoritesTestServer.
func newExclusionsTestServer(t *testing.T) (*Server, *http.ServeMux) {
	t.Helper()
	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	srv, err := NewServer(Config{
		Store:  store,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	return srv, mux
}

func TestExcludedStations_CreateListDelete(t *testing.T) {
	srv, mux := newExclusionsTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/stations/exclusions",
		strings.NewReader(`{"callsign":"w1digi","note":"local digi, no icon"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created dto.ExcludedStationResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Callsign != "W1DIGI" {
		t.Errorf("Callsign = %q, want uppercased W1DIGI", created.Callsign)
	}
	if created.Note != "local digi, no icon" {
		t.Errorf("unexpected row: %+v", created)
	}

	rows, err := srv.store.ListExcludedStations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Callsign != "W1DIGI" {
		t.Fatalf("expected one persisted W1DIGI row, got %+v", rows)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/stations/exclusions", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rec.Code)
	}
	var list []dto.ExcludedStationResponse
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}

	id := created.ID
	req = httptest.NewRequest(http.MethodDelete, "/api/stations/exclusions/"+itoa(id), nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	rows, _ = srv.store.ListExcludedStations(context.Background())
	if len(rows) != 0 {
		t.Fatalf("expected empty after delete, got %+v", rows)
	}
}

func TestExcludedStations_DeleteUnknownIsNotFound(t *testing.T) {
	_, mux := newExclusionsTestServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/stations/exclusions/999", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestExcludedStations_RejectsDuplicate(t *testing.T) {
	_, mux := newExclusionsTestServer(t)

	body := `{"callsign":"W1DIGI"}`
	for i, wantCode := range []int{http.StatusCreated, http.StatusConflict} {
		req := httptest.NewRequest(http.MethodPost, "/api/stations/exclusions", strings.NewReader(body))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != wantCode {
			t.Fatalf("post #%d: got %d, want %d: %s", i, rec.Code, wantCode, rec.Body.String())
		}
	}
}

func TestExcludedStations_RejectsBadFormat(t *testing.T) {
	_, mux := newExclusionsTestServer(t)

	cases := []string{
		`{"callsign":""}`,
		`{"callsign":"TOOLONGCALL"}`,
	}
	for _, body := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/stations/exclusions", strings.NewReader(body))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: got %d, want 400", body, rec.Code)
		}
	}
}
