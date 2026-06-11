package mapscatalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func canned() Catalog {
	return Catalog{
		SchemaVersion: 1,
		GeneratedAt:   "2026-04-30T00:00:00Z",
		Countries: []Country{
			{ISO2: "de", Name: "Germany", SizeBytes: 1234, SHA256: "a"},
		},
		Provinces: []Province{
			{ISO2: "ca", Slug: "british-columbia", Name: "British Columbia", Code: "BC", SizeBytes: 5678, SHA256: "b"},
		},
		States: []State{
			{Slug: "colorado", Name: "Colorado", Code: "CO", SizeBytes: 9012, SHA256: "c"},
		},
	}
}

func TestGet_FetchesAndCaches(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(canned())
	}))
	defer srv.Close()

	c := New(srv.URL, func(context.Context) string { return "tok" }, time.Hour)
	ctx := context.Background()

	got1, err := c.Get(ctx)
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	if len(got1.Countries) != 1 || got1.Countries[0].ISO2 != "de" {
		t.Fatalf("unexpected catalog: %+v", got1)
	}

	got2, err := c.Get(ctx)
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if got2.GeneratedAt != got1.GeneratedAt {
		t.Fatalf("expected cached copy")
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 fetch, got %d", calls.Load())
	}
}

func TestGet_TTLRefresh(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(canned())
	}))
	defer srv.Close()

	c := New(srv.URL, func(context.Context) string { return "tok" }, 0)
	ctx := context.Background()
	if _, err := c.Get(ctx); err != nil {
		t.Fatalf("Get1: %v", err)
	}
	if _, err := c.Get(ctx); err != nil {
		t.Fatalf("Get2: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 fetches with TTL=0, got %d", calls.Load())
	}
}

func TestGet_ServesStaleOnError(t *testing.T) {
	var allowError atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if allowError.Load() {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(canned())
	}))
	defer srv.Close()

	c := New(srv.URL, func(context.Context) string { return "tok" }, 0)
	ctx := context.Background()
	if _, err := c.Get(ctx); err != nil {
		t.Fatalf("warm: %v", err)
	}
	allowError.Store(true)
	got, err := c.Get(ctx)
	if err != nil {
		t.Fatalf("expected stale fallback, got error: %v", err)
	}
	if got.SchemaVersion != 1 {
		t.Fatalf("expected stale catalog, got %+v", got)
	}
}

func TestGet_ColdFailureReturnsError(t *testing.T) {
	c := New("http://127.0.0.1:0", func(context.Context) string { return "tok" }, time.Hour)
	_, err := c.Get(context.Background())
	if err == nil {
		t.Fatal("expected error on cold failure")
	}
}

func TestColdFailure_AuthErrorIsActionable(t *testing.T) {
	cases := []struct {
		name      string
		token     string
		status    int
		body      string
		wantParts []string
	}{
		{
			name:      "invalid token",
			token:     "stale-token",
			status:    http.StatusUnauthorized,
			body:      "unauthorized: missing",
			wantParts: []string{"Graywolf Maps access was rejected", "re-register your device", "Settings tab", "HTTP 401"},
		},
		{
			name:      "no token",
			token:     "",
			status:    http.StatusUnauthorized,
			body:      "unauthorized: missing",
			wantParts: []string{"To activate Graywolf Maps", "register your device", "Settings tab", "HTTP 401"},
		},
		{
			name:      "forbidden",
			token:     "revoked",
			status:    http.StatusForbidden,
			body:      "forbidden",
			wantParts: []string{"Graywolf Maps access was rejected", "HTTP 403"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, tc.body, tc.status)
			}))
			defer srv.Close()

			c := New(srv.URL, func(context.Context) string { return tc.token }, time.Hour)
			_, err := c.Get(context.Background())
			if err == nil {
				t.Fatal("expected error on auth failure")
			}
			msg := err.Error()
			for _, want := range tc.wantParts {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q missing %q", msg, want)
				}
			}
			// The raw upstream body must not leak into the auth message.
			if strings.Contains(msg, "unauthorized: missing") {
				t.Errorf("auth error leaked raw upstream body: %q", msg)
			}
		})
	}
}

func TestColdFailure_NonAuthErrorIncludesStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, func(context.Context) string { return "tok" }, time.Hour)
	_, err := c.Get(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("expected HTTP 500 in error, got %q", err.Error())
	}
}

func TestGet_AppendsToken(t *testing.T) {
	var seenQuery atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenQuery.Store(r.URL.RawQuery)
		_ = json.NewEncoder(w).Encode(canned())
	}))
	defer srv.Close()

	c := New(srv.URL, func(context.Context) string { return "abc123" }, time.Hour)
	if _, err := c.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	q, _ := seenQuery.Load().(string)
	if q != "t=abc123" {
		t.Fatalf("expected t=abc123, got %q", q)
	}
}

func TestNew_LoadsDiskCacheWhenPresent(t *testing.T) {
	dir := t.TempDir()
	contents := `{"schemaVersion":1,"generatedAt":"2026-05-01","countries":[],"provinces":[],"states":[{"slug":"colorado","name":"Colorado","sizeBytes":100,"sha256":"x","bbox":[-109,37,-102,41]}]}`
	if err := os.WriteFile(filepath.Join(dir, "catalog.json"), []byte(contents), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// Upstream that returns 500 -- forces stale-on-error path.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer upstream.Close()

	c := NewWithDiskCache(upstream.URL, func(context.Context) string { return "" }, time.Hour, dir)
	got, err := c.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.States) != 1 || got.States[0].Slug != "colorado" {
		t.Fatalf("expected colorado from disk cache, got %+v", got.States)
	}
}

func TestFetch_WritesDiskCacheOnSuccess(t *testing.T) {
	dir := t.TempDir()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"schemaVersion":1,"generatedAt":"x","countries":[],"provinces":[],"states":[{"slug":"utah","name":"Utah","sizeBytes":1,"sha256":"x"}]}`))
	}))
	defer upstream.Close()

	c := NewWithDiskCache(upstream.URL, func(context.Context) string { return "" }, time.Hour, dir)
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(dir, "catalog.json"))
	if err != nil {
		t.Fatalf("read disk cache: %v", err)
	}
	if !strings.Contains(string(b), `"utah"`) {
		t.Fatalf("disk cache missing utah: %s", string(b))
	}
}

func TestNew_NoDiskCacheStillFunctions(t *testing.T) {
	// Existing call site behavior: New() with no disk cache still works.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"schemaVersion":1,"generatedAt":"x","countries":[],"provinces":[],"states":[]}`))
	}))
	defer upstream.Close()
	c := New(upstream.URL, func(context.Context) string { return "" }, time.Hour)
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
}
