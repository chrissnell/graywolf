package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// seedAction inserts a stub action row so child invocation rows can
// satisfy the FK. Returns the assigned id.
func seedAction(t *testing.T, store *configstore.Store, name string) uint {
	t.Helper()
	a := &configstore.Action{
		Name: name, Type: "command", CommandPath: "/bin/sh",
		WebhookHeaders: "{}", ArgSchema: "[]", TimeoutSec: 5,
	}
	if err := store.CreateAction(context.Background(), a); err != nil {
		t.Fatalf("create action: %v", err)
	}
	return a.ID
}

// seedInvocations writes n rows with the given action_id and a couple
// of fields varied so the filter tests can target specific subsets.
func seedInvocations(t *testing.T, store *configstore.Store, actionID uint, sender, status, source string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		id := actionID
		row := &configstore.ActionInvocation{
			ActionID: &id, ActionNameAt: fmt.Sprintf("a%d", actionID),
			SenderCall: sender, Source: source, Status: status,
			RawArgsJSON: `{"k":"v"}`,
			CreatedAt:   time.Now().UTC().Add(time.Duration(i) * time.Millisecond),
		}
		if err := store.InsertActionInvocation(context.Background(), row); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
}

func TestInvocations_ListAndFilter(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	id1 := seedAction(t, srv.store, "a1")
	id2 := seedAction(t, srv.store, "a2")
	seedInvocations(t, srv.store, id1, "K7AAA", "ok", "rf", 3)
	seedInvocations(t, srv.store, id2, "K7BBB", "denied", "is", 2)

	cases := []struct {
		query   string
		want    int
		wantOne string // if non-empty, every row.ActionName must equal this
	}{
		{"", 5, ""},
		{fmt.Sprintf("?action_id=%d", id1), 3, "a1"},
		{"?sender_call=K7BBB", 2, "a2"},
		{"?status=denied", 2, "a2"},
		{"?source=is", 2, "a2"},
		{"?q=K7AAA", 3, "a1"},
		{"?limit=2", 2, ""},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/actions/invocations"+tc.query, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("query %q: status=%d body=%s", tc.query, rec.Code, rec.Body.String())
			continue
		}
		var got []dto.ActionInvocation
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Errorf("query %q: decode: %v", tc.query, err)
			continue
		}
		if len(got) != tc.want {
			t.Errorf("query %q: got %d rows, want %d", tc.query, len(got), tc.want)
		}
		if tc.wantOne != "" {
			for _, r := range got {
				if r.ActionName != tc.wantOne {
					t.Errorf("query %q: row %+v has wrong action_name", tc.query, r)
				}
			}
		}
	}
}

func TestInvocations_BadParam(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	for _, q := range []string{"?action_id=abc", "?limit=-1", "?limit=99999", "?offset=-1"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/actions/invocations"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q: status=%d, want 400", q, rec.Code)
		}
	}
}

func TestInvocations_Clear(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	id := seedAction(t, srv.store, "a1")
	seedInvocations(t, srv.store, id, "K7AAA", "ok", "rf", 5)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/actions/invocations", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("clear: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/actions/invocations", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var got []dto.ActionInvocation
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty list, got %d rows", len(got))
	}
}

func TestInvocations_ArgsRoundtrip(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	id := seedAction(t, srv.store, "a")
	row := &configstore.ActionInvocation{
		ActionID: &id, ActionNameAt: "a", SenderCall: "K7XYZ",
		Source: "rf", Status: "ok", RawArgsJSON: `{"freq":"146520","mode":"fm"}`,
	}
	if err := srv.store.InsertActionInvocation(context.Background(), row); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/actions/invocations", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var got []dto.ActionInvocation
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Args["freq"] != "146520" || got[0].Args["mode"] != "fm" {
		t.Fatalf("args lost on read: %+v", got)
	}
}
