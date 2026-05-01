package mapscatalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
