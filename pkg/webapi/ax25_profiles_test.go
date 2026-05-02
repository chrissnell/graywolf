package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func TestListAX25Profiles_PinnedFirst(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ctx := context.Background()

	if err := srv.store.CreateAX25SessionProfile(ctx, &configstore.AX25SessionProfile{
		Name: "recent", LocalCall: "K0SWE", DestCall: "W1AW",
	}); err != nil {
		t.Fatalf("create recent: %v", err)
	}
	if err := srv.store.CreateAX25SessionProfile(ctx, &configstore.AX25SessionProfile{
		Name: "pinned", LocalCall: "K0SWE", DestCall: "K0BBS", Pinned: true,
	}); err != nil {
		t.Fatalf("create pinned: %v", err)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ax25/profiles", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got []dto.AX25SessionProfile
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if !got[0].Pinned {
		t.Fatalf("expected pinned first, got %+v", got[0])
	}
}

func TestCreateAndPinAX25Profile(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"local_call":"K0SWE","dest_call":"K0BBS","name":"BBS"}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/ax25/profiles", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created dto.AX25SessionProfile
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("missing id on created profile")
	}

	// Pin via dedicated endpoint.
	pinBody := `{"pinned": true}`
	pinURL := "/api/ax25/profiles/" + itoa(created.ID) + "/pin"
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, pinURL, strings.NewReader(pinBody)))
	if rec2.Code != http.StatusOK {
		t.Fatalf("pin status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	var pinned dto.AX25SessionProfile
	if err := json.NewDecoder(rec2.Body).Decode(&pinned); err != nil {
		t.Fatalf("pin decode: %v", err)
	}
	if !pinned.Pinned {
		t.Fatal("pin endpoint did not flip pinned flag")
	}
}

func TestUpdateAndDeleteAX25Profile(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ctx := context.Background()
	p := &configstore.AX25SessionProfile{LocalCall: "K0SWE", DestCall: "K0BBS"}
	if err := srv.store.CreateAX25SessionProfile(ctx, p); err != nil {
		t.Fatalf("seed: %v", err)
	}
	url := "/api/ax25/profiles/" + itoa(p.ID)

	put := `{"local_call":"K0SWE","dest_call":"W1AW","name":"renamed"}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, url, strings.NewReader(put)))
	if rec.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", rec.Code, rec.Body.String())
	}
	var updated dto.AX25SessionProfile
	if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.DestCall != "W1AW" || updated.Name != "renamed" {
		t.Fatalf("update did not stick: %+v", updated)
	}

	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, httptest.NewRequest(http.MethodDelete, url, nil))
	if rec2.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", rec2.Code, rec2.Body.String())
	}
}

func TestCreateAX25Profile_RejectsMissingFields(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/ax25/profiles", strings.NewReader(`{}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

