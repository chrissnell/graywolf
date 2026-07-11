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
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/messages"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// newBlocklistTestServer wires a Server with the real store, a fake
// messages service (so we can observe ReloadBlockedCallsigns), and the
// blocklist routes registered.
func newBlocklistTestServer(t *testing.T) (*Server, *http.ServeMux, *fakeMessagesSvc) {
	t.Helper()
	ctx := context.Background()
	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.UpsertStationConfig(ctx, configstore.StationConfig{Callsign: "N0CALL"}); err != nil {
		t.Fatal(err)
	}
	msgStore := messages.NewStore(store.DB())
	svc := &fakeMessagesSvc{}
	srv, err := NewServer(Config{
		Store:  store,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.SetMessagesService(svc)
	srv.SetMessagesStore(msgStore)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	return srv, mux, svc
}

func TestBlocklist_CreateListDelete(t *testing.T) {
	srv, mux, svc := newBlocklistTestServer(t)

	reloaded := make(chan struct{}, 4)
	svc.reloadBlockedFn = func(ctx context.Context) error {
		select {
		case reloaded <- struct{}{}:
		default:
		}
		return nil
	}

	// Create.
	req := httptest.NewRequest(http.MethodPost, "/api/messages/blocklist",
		strings.NewReader(`{"callsign":"w1abc","note":"cert spam","enabled":true}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created dto.BlockedCallsignResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Callsign != "W1ABC" {
		t.Errorf("Callsign = %q, want uppercased W1ABC", created.Callsign)
	}
	if !created.Enabled || created.Note != "cert spam" {
		t.Errorf("unexpected row: %+v", created)
	}
	select {
	case <-reloaded:
	case <-time.After(time.Second):
		t.Error("ReloadBlockedCallsigns not called on create")
	}

	// Persisted + listed.
	rows, err := srv.store.ListBlockedCallsigns(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Callsign != "W1ABC" {
		t.Fatalf("expected one persisted W1ABC row, got %+v", rows)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/messages/blocklist", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rec.Code)
	}
	var list []dto.BlockedCallsignResponse
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}

	// Delete.
	id := created.ID
	req = httptest.NewRequest(http.MethodDelete, "/api/messages/blocklist/"+itoa(id), nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	rows, _ = srv.store.ListBlockedCallsigns(context.Background())
	if len(rows) != 0 {
		t.Fatalf("expected empty after delete, got %+v", rows)
	}
}

func TestBlocklist_RejectsDuplicate(t *testing.T) {
	_, mux, _ := newBlocklistTestServer(t)

	body := `{"callsign":"W1ABC","enabled":true}`
	for i, wantCode := range []int{http.StatusCreated, http.StatusConflict} {
		req := httptest.NewRequest(http.MethodPost, "/api/messages/blocklist", strings.NewReader(body))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != wantCode {
			t.Fatalf("post #%d: got %d, want %d: %s", i, rec.Code, wantCode, rec.Body.String())
		}
	}
}

func TestBlocklist_RejectsOwnCallAndBadFormat(t *testing.T) {
	_, mux, _ := newBlocklistTestServer(t)

	cases := []string{
		`{"callsign":"N0CALL","enabled":true}`,   // own call
		`{"callsign":"N0CALL-7","enabled":true}`, // own base call, any SSID
		`{"callsign":"","enabled":true}`,         // empty
		`{"callsign":"TOOLONGCALL","enabled":true}`,
	}
	for _, body := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/messages/blocklist", strings.NewReader(body))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: got %d, want 400", body, rec.Code)
		}
	}
}

func TestBlocklist_ToggleEnabledViaPut(t *testing.T) {
	srv, mux, _ := newBlocklistTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/messages/blocklist",
		strings.NewReader(`{"callsign":"W1ABC","enabled":true}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var created dto.BlockedCallsignResponse
	_ = json.NewDecoder(rec.Body).Decode(&created)

	req = httptest.NewRequest(http.MethodPut, "/api/messages/blocklist/"+itoa(created.ID),
		strings.NewReader(`{"callsign":"W1ABC","enabled":false}`))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	row, err := srv.store.GetBlockedCallsign(context.Background(), created.ID)
	if err != nil || row == nil {
		t.Fatalf("lookup after put: %v", err)
	}
	if row.Enabled {
		t.Error("Enabled should be false after PUT toggle")
	}
}
