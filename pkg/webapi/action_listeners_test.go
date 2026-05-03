package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chrissnell/graywolf/pkg/actions"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// fakeActionsService records ReloadListeners and TestFire calls so
// tests can assert the listener mutations signaled the runtime and
// the test-fire endpoint dispatches through the runtime path.
type fakeActionsService struct {
	reloads   atomic.Int32
	testFires atomic.Int32
	last      actions.Result
}

func (f *fakeActionsService) ReloadListeners(_ context.Context) error {
	f.reloads.Add(1)
	return nil
}

func (f *fakeActionsService) TestFire(_ context.Context, _ *configstore.Action, _ []actions.KeyValue) (actions.Result, uint) {
	f.testFires.Add(1)
	return f.last, 0
}

func TestListeners_CreateNormalizesUppercase(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	fake := &fakeActionsService{}
	srv.SetActionsService(fake)

	body := strings.NewReader(`{"addressee":"gwact"}`)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/actions/listeners", body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got dto.ActionListenerAddressee
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Addressee != "GWACT" {
		t.Fatalf("addressee=%q, want GWACT", got.Addressee)
	}
	if fake.reloads.Load() == 0 {
		t.Fatalf("ReloadListeners was not called")
	}
}

func TestListeners_LengthCap(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	body := strings.NewReader(`{"addressee":"WAYTOOLONG"}`)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/actions/listeners", body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestListeners_RefusesEmpty(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	body := strings.NewReader(`{"addressee":""}`)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/actions/listeners", body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestListeners_RefusesTacticalCollision(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	if err := srv.store.CreateTacticalCallsign(context.Background(), &configstore.TacticalCallsign{Callsign: "OPS"}); err != nil {
		t.Fatalf("seed tactical: %v", err)
	}

	body := strings.NewReader(`{"addressee":"ops"}`)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/actions/listeners", body))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s, want 409", rec.Code, rec.Body.String())
	}
}

func TestListeners_DuplicateConflict(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"addressee":"DUP"}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/actions/listeners", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("first: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/actions/listeners", strings.NewReader(body)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("second: %d, want 409", rec.Code)
	}
}

func TestListeners_ListAndDelete(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	fake := &fakeActionsService{}
	srv.SetActionsService(fake)

	for _, name := range []string{"AAA", "BBB"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/actions/listeners", strings.NewReader(`{"addressee":"`+name+`"}`)))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", name, rec.Code, rec.Body.String())
		}
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/actions/listeners", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var got []dto.ActionListenerAddressee
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2", len(got))
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/actions/listeners/AAA", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if fake.reloads.Load() < 3 {
		t.Fatalf("expected at least 3 reloads (2 creates + 1 delete), got %d", fake.reloads.Load())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/actions/listeners", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	got = nil
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Addressee != "BBB" {
		t.Fatalf("post-delete list: %+v", got)
	}
}
