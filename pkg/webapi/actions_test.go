package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// writeExecScript drops a shebang shell script in t.TempDir, marks it
// 0755, and returns the absolute path. Used by tests that need
// validateCommandPath to find a real executable on disk.
func writeExecScript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "echo.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return p
}

func newActionRequest(name, cmd string) dto.Action {
	return dto.Action{
		Name:        name,
		Type:        "command",
		CommandPath: cmd,
		TimeoutSec:  5,
		OTPRequired: false,
		Enabled:     true,
		ArgSchema:   []dto.ArgSpec{},
	}
}

func TestActionsCRUD_Create(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	cmd := writeExecScript(t)

	body, _ := json.Marshal(newActionRequest("foo", cmd))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/actions", bytes.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got dto.Action
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ID == 0 || got.Name != "foo" || got.Type != "command" {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestActionsCRUD_NameValidation(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	cmd := writeExecScript(t)

	for _, name := range []string{"", "with space", "way-too-long-action-name-exceeds-32"} {
		body, _ := json.Marshal(newActionRequest(name, cmd))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/actions", bytes.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("name %q: status=%d, want 400 (body=%s)", name, rec.Code, rec.Body.String())
		}
	}
}

func TestActionsCRUD_NameUniqueness(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	cmd := writeExecScript(t)

	body, _ := json.Marshal(newActionRequest("dup", cmd))
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/actions", bytes.NewReader(body)))
		want := http.StatusCreated
		if i == 1 {
			want = http.StatusConflict
		}
		if rec.Code != want {
			t.Fatalf("attempt %d: status=%d body=%s want %d", i, rec.Code, rec.Body.String(), want)
		}
	}
}

func TestActionsCRUD_CommandPathValidation(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	cases := map[string]string{
		"empty":    "",
		"relative": "echo",
		"missing":  "/no/such/path/here",
		"dir":      t.TempDir(),
	}
	for name, p := range cases {
		body, _ := json.Marshal(newActionRequest("a", p))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/actions", bytes.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d, want 400", name, rec.Code)
		}
	}
}

func TestActionsCRUD_TypeValidation(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	cases := []struct {
		name string
		in   dto.Action
	}{
		{"unknown type", dto.Action{Name: "x", Type: "telegram", TimeoutSec: 5, ArgSchema: []dto.ArgSpec{}}},
		{"webhook missing url", dto.Action{Name: "x", Type: "webhook", WebhookMethod: "POST", TimeoutSec: 5, ArgSchema: []dto.ArgSpec{}}},
		{"webhook bad method", dto.Action{Name: "x", Type: "webhook", WebhookMethod: "PATCH", WebhookURL: "https://example.test/", TimeoutSec: 5, ArgSchema: []dto.ArgSpec{}}},
		{"webhook bad scheme", dto.Action{Name: "x", Type: "webhook", WebhookMethod: "GET", WebhookURL: "file:///etc/passwd", TimeoutSec: 5, ArgSchema: []dto.ArgSpec{}}},
		{"webhook unparseable", dto.Action{Name: "x", Type: "webhook", WebhookMethod: "GET", WebhookURL: "://not a url", TimeoutSec: 5, ArgSchema: []dto.ArgSpec{}}},
		{"webhook with userinfo", dto.Action{Name: "x", Type: "webhook", WebhookMethod: "GET", WebhookURL: "https://user:pass@example.test/", TimeoutSec: 5, ArgSchema: []dto.ArgSpec{}}},
		{"webhook missing host", dto.Action{Name: "x", Type: "webhook", WebhookMethod: "GET", WebhookURL: "https:///path", TimeoutSec: 5, ArgSchema: []dto.ArgSpec{}}},
	}
	for _, tc := range cases {
		body, _ := json.Marshal(tc.in)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/actions", bytes.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d, want 400 (body=%s)", tc.name, rec.Code, rec.Body.String())
		}
	}
}

func TestActionsCRUD_OTPRequiredNeedsCredential(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	cmd := writeExecScript(t)

	in := newActionRequest("o", cmd)
	in.OTPRequired = true
	// OTPCredentialID intentionally nil — the runner would surface
	// StatusNoCredential at dispatch; the API rejects on save.
	body, _ := json.Marshal(in)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/actions", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}
}

func TestActionsCRUD_ArgSchemaRoundtrip(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	cmd := writeExecScript(t)

	in := newActionRequest("with-args", cmd)
	in.WebhookHeaders = nil
	in.ArgSchema = []dto.ArgSpec{
		{Key: "freq", Regex: `^[0-9]+$`, MaxLen: 10, Required: true},
		{Key: "mode"},
	}
	body, _ := json.Marshal(in)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/actions", bytes.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created dto.Action
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if len(created.ArgSchema) != 2 || created.ArgSchema[0].Key != "freq" {
		t.Fatalf("schema not echoed: %+v", created.ArgSchema)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/actions/%d", created.ID), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("get: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got dto.Action
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.ArgSchema) != 2 || got.ArgSchema[0].Regex != `^[0-9]+$` || got.ArgSchema[0].MaxLen != 10 || !got.ArgSchema[0].Required {
		t.Fatalf("schema lost on get: %+v", got.ArgSchema)
	}
}

func TestActionsCRUD_GetNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/actions/999", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}

func TestActionsCRUD_UpdateAndList(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	cmd := writeExecScript(t)

	// Create one.
	body, _ := json.Marshal(newActionRequest("alpha", cmd))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/actions", bytes.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created dto.Action
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	// Update it. The configstore model has gorm default OTPRequired=true
	// on Create, so the persisted row reads back true even though we
	// posted false; override on the update so the validateAction
	// cross-field check (OTPRequired ⇒ credential ref) passes.
	created.Description = "edited"
	created.TimeoutSec = 30
	created.OTPRequired = false
	body, _ = json.Marshal(created)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/actions/"+strconv.FormatUint(uint64(created.ID), 10), bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("update: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var updated dto.Action
	if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	if updated.Description != "edited" || updated.TimeoutSec != 30 {
		t.Fatalf("update did not persist: %+v", updated)
	}

	// List.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/actions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list []dto.Action
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Description != "edited" {
		t.Fatalf("list mismatch: %+v", list)
	}
}

func TestActionsCRUD_Delete(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	cmd := writeExecScript(t)

	body, _ := json.Marshal(newActionRequest("d", cmd))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/actions", bytes.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created dto.Action
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/actions/"+strconv.FormatUint(uint64(created.ID), 10), nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/actions/"+strconv.FormatUint(uint64(created.ID), 10), nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("post-delete get: status=%d, want 404", rec.Code)
	}
}

func TestActionsCRUD_OTPCredentialRefValidation(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	cmd := writeExecScript(t)

	in := newActionRequest("o", cmd)
	bogus := uint(9999)
	in.OTPCredentialID = &bogus
	body, _ := json.Marshal(in)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/actions", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}
}

func TestActionsCRUD_RejectsUnknownFields(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	body := strings.NewReader(`{"name":"x","type":"command","command_path":"/bin/sh","timeout_sec":5,"unknown":1}`)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/actions", body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestActionsCRUD_ListLastInvoked(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	cmd := writeExecScript(t)

	body, _ := json.Marshal(newActionRequest("inv", cmd))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/actions", bytes.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created dto.Action
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	// Seed an invocation row directly.
	id := created.ID
	row := &configstore.ActionInvocation{
		ActionID: &id, ActionNameAt: "inv", SenderCall: "K7XYZ",
		Source: "rf", Status: "ok",
	}
	if err := srv.store.InsertActionInvocation(context.Background(), row); err != nil {
		t.Fatalf("seed invocation: %v", err)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/actions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list []dto.Action
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].LastInvokedBy != "K7XYZ" || list[0].LastInvokedAt == nil {
		t.Fatalf("last_invoked_* not populated: %+v", list)
	}
}
