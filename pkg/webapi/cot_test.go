package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// seedCotTarget creates a CoT target through the API so delete tests
// have a real row to act on.
func seedCotTarget(t *testing.T, mux *http.ServeMux) uint32 {
	t.Helper()
	body := `{"object_name":"WOOFWOOF","comment":"test object","latitude":37.5,"longitude":-122.0}`
	req := httptest.NewRequest(http.MethodPost, "/api/cot-targets", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID uint32 `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	return resp.ID
}

func seedStationCallsign(t *testing.T, srv *Server) {
	t.Helper()
	if err := srv.store.UpsertStationConfig(context.Background(), configstore.StationConfig{Callsign: "N0CAL-9"}); err != nil {
		t.Fatal(err)
	}
}

// TestDeleteCotTarget_SendsKillBeforeRemoving verifies the delete
// handler transmits the object-kill report while the row (and its
// channel/destination/path snapshot) is still readable, not after.
func TestDeleteCotTarget_SendsKillBeforeRemoving(t *testing.T) {
	srv, _ := newTestServer(t)
	seedStationCallsign(t, srv)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	id := seedCotTarget(t, mux)

	var killedID uint32
	var rowStillPresentDuringKill bool
	srv.SetCotSendKill(func(_ context.Context, gotID uint32) error {
		killedID = gotID
		if _, err := srv.store.GetCotTarget(context.Background(), gotID); err == nil {
			rowStillPresentDuringKill = true
		}
		return nil
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/cot-targets/"+strconv.FormatUint(uint64(id), 10), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if killedID != id {
		t.Fatalf("SetCotSendKill called with id %d, want %d", killedID, id)
	}
	if !rowStillPresentDuringKill {
		t.Fatal("kill was sent after the row was already deleted")
	}
	if _, err := srv.store.GetCotTarget(context.Background(), id); err == nil {
		t.Fatal("target still exists after delete")
	}
}

// TestDeleteCotTarget_DeletesEvenWhenKillFails guards the best-effort
// contract: an unreachable channel must not trap the operator's delete.
func TestDeleteCotTarget_DeletesEvenWhenKillFails(t *testing.T) {
	srv, _ := newTestServer(t)
	seedStationCallsign(t, srv)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	id := seedCotTarget(t, mux)

	srv.SetCotSendKill(func(context.Context, uint32) error {
		return errors.New("channel down")
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/cot-targets/"+strconv.FormatUint(uint64(id), 10), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 even when kill fails, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := srv.store.GetCotTarget(context.Background(), id); err == nil {
		t.Fatal("target still exists after delete")
	}
}

// TestDeleteInactiveCotTargets_SendsKillForEachInactiveTarget verifies
// the bulk-delete path kills every exhausted row before removing it,
// and leaves active rows (and their kill callback) untouched.
func TestDeleteInactiveCotTargets_SendsKillForEachInactiveTarget(t *testing.T) {
	srv, _ := newTestServer(t)
	seedStationCallsign(t, srv)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	inactiveID := seedCotTarget(t, mux)
	activeID := seedCotTarget(t, mux)

	// Force inactiveID exhausted (Active = TxCount < NumTransmits).
	target, err := srv.store.GetCotTarget(context.Background(), inactiveID)
	if err != nil {
		t.Fatal(err)
	}
	target.TxCount = target.NumTransmits
	if err := srv.store.DB().Save(target).Error; err != nil {
		t.Fatal(err)
	}

	var killedIDs []uint32
	srv.SetCotSendKill(func(_ context.Context, gotID uint32) error {
		killedIDs = append(killedIDs, gotID)
		return nil
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/cot-targets/inactive", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(killedIDs) != 1 || killedIDs[0] != inactiveID {
		t.Fatalf("killedIDs = %v, want [%d]", killedIDs, inactiveID)
	}
	if _, err := srv.store.GetCotTarget(context.Background(), inactiveID); err == nil {
		t.Fatal("inactive target still exists after bulk delete")
	}
	if _, err := srv.store.GetCotTarget(context.Background(), activeID); err != nil {
		t.Fatal("active target was removed by the inactive bulk delete")
	}
}
