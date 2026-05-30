package mapsstyle

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCleanRelPath(t *testing.T) {
	cases := []struct {
		in      string
		wantOK  bool
		wantOut string
	}{
		{"americana-roboto/style.json", true, "americana-roboto/style.json"},
		{"americana/shields.json", true, "americana/shields.json"},
		{"americana/sprites/sprite.json", true, "americana/sprites/sprite.json"},
		{"americana/sprites/sprite.png", true, "americana/sprites/sprite.png"},
		{"americana/sprites/sprite@2x.png", true, "americana/sprites/sprite@2x.png"},
		{"roboto-glyphs/Roboto Regular/0-255.pbf", true, "roboto-glyphs/Roboto Regular/0-255.pbf"},
		{"tiles.json", true, "tiles.json"},
		{"../etc/passwd", false, ""},
		{"americana-roboto/../../../etc/passwd", false, ""},
		{"/absolute", false, ""},
		{"", false, ""},
		{"unknown-prefix/x.json", false, ""},
		{"americana-roboto/sub/style.json", false, ""}, // nested under americana-roboto not allowed
	}
	for _, tc := range cases {
		got, ok := cleanRelPath(tc.in)
		if ok != tc.wantOK {
			t.Errorf("cleanRelPath(%q) ok=%v want %v", tc.in, ok, tc.wantOK)
			continue
		}
		if ok && got != tc.wantOut {
			t.Errorf("cleanRelPath(%q) = %q want %q", tc.in, got, tc.wantOut)
		}
	}
}

func TestUpstreamURL(t *testing.T) {
	cases := []struct {
		rel  string
		want string
	}{
		{"americana-roboto/style.json", "https://maps.nw5w.com/style/americana-roboto/style.json"},
		{"americana/shields.json", "https://maps.nw5w.com/style/americana/shields.json"},
		{"americana/sprites/sprite.png", "https://maps.nw5w.com/style/americana/sprites/sprite.png"},
		{"roboto-glyphs/Roboto Regular/0-255.pbf", "https://maps.nw5w.com/style/roboto-glyphs/Roboto%20Regular/0-255.pbf"},
		{"tiles.json", "https://maps.nw5w.com/tiles.json"},
	}
	for _, tc := range cases {
		got, err := upstreamURL("https://maps.nw5w.com", tc.rel)
		if err != nil {
			t.Errorf("upstreamURL(%q) err: %v", tc.rel, err)
			continue
		}
		if got != tc.want {
			t.Errorf("upstreamURL(%q) = %q want %q", tc.rel, got, tc.want)
		}
	}
}

func TestContentTypeFor(t *testing.T) {
	cases := map[string]string{
		"americana-roboto/style.json":        "application/json",
		"americana/shields.json":             "application/json",
		"americana/sprites/sprite.json":      "application/json",
		"americana/sprites/sprite.png":       "image/png",
		"americana/sprites/sprite@2x.png":    "image/png",
		"roboto-glyphs/Roboto Regular/0.pbf": "application/x-protobuf",
		"tiles.json":                         "application/json",
	}
	for in, want := range cases {
		if got := contentTypeFor(in); got != want {
			t.Errorf("contentTypeFor(%q) = %q want %q", in, got, want)
		}
	}
}

func TestCache_DiskReadWrite(t *testing.T) {
	dir := t.TempDir()
	c := New(Config{BaseURL: "https://maps.nw5w.com", CacheDir: dir})

	if _, _, err := c.readDisk("americana-roboto/style.json"); err == nil {
		t.Fatalf("expected miss on empty disk")
	}

	body := []byte(`{"name":"test"}`)
	if err := c.writeDisk("americana-roboto/style.json", body); err != nil {
		t.Fatalf("writeDisk: %v", err)
	}
	got, ct, err := c.readDisk("americana-roboto/style.json")
	if err != nil {
		t.Fatalf("readDisk: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("body mismatch: got %q want %q", got, body)
	}
	if ct != "application/json" {
		t.Fatalf("content-type: got %q want application/json", ct)
	}

	if _, err := os.Stat(filepath.Join(dir, "americana-roboto", "style.json")); err != nil {
		t.Fatalf("file not at expected path: %v", err)
	}
}

func TestCache_WriteIsAtomic(t *testing.T) {
	dir := t.TempDir()
	c := New(Config{BaseURL: "https://maps.nw5w.com", CacheDir: dir})
	if err := c.writeDisk("tiles.json", []byte(`v1`)); err != nil {
		t.Fatal(err)
	}
	if err := c.writeDisk("tiles.json", []byte(`v2`)); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}

func TestCache_FetchUpstream(t *testing.T) {
	var got atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Add(1)
		if r.URL.Path != "/style/americana-roboto/style.json" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("t") != "abc" {
			t.Errorf("expected ?t=abc, got %q", r.URL.RawQuery)
		}
		w.Write([]byte(`{"upstream":true}`))
	}))
	defer srv.Close()

	c := New(Config{
		BaseURL:       srv.URL,
		CacheDir:      t.TempDir(),
		TokenProvider: func(context.Context) string { return "abc" },
	})
	body, ct, err := c.fetchUpstream(context.Background(), "americana-roboto/style.json")
	if err != nil {
		t.Fatalf("fetchUpstream: %v", err)
	}
	if string(body) != `{"upstream":true}` {
		t.Fatalf("body: %s", body)
	}
	if ct != "application/json" {
		t.Fatalf("content-type: %s", ct)
	}
	if got.Load() != 1 {
		t.Fatalf("expected 1 upstream call, got %d", got.Load())
	}
}

func TestCache_FetchUpstream_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, CacheDir: t.TempDir()})
	if _, _, err := c.fetchUpstream(context.Background(), "tiles.json"); err == nil {
		t.Fatalf("expected error on 404")
	}
}

func TestCache_Get_DiskHit(t *testing.T) {
	var upstreamCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.Write([]byte(`{"upstream":true}`))
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, CacheDir: t.TempDir()})
	if err := c.writeDisk("tiles.json", []byte(`{"cached":true}`)); err != nil {
		t.Fatal(err)
	}
	body, ct, err := c.Get(context.Background(), "tiles.json")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(body) != `{"cached":true}` {
		t.Fatalf("expected disk hit, got %s", body)
	}
	if ct != "application/json" {
		t.Fatalf("content-type: %s", ct)
	}
	if upstreamCalls.Load() != 0 {
		t.Fatalf("expected 0 upstream calls, got %d", upstreamCalls.Load())
	}
}

func TestCache_Get_DiskMissTriggersPullThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"fresh":true}`))
	}))
	defer srv.Close()
	dir := t.TempDir()
	c := New(Config{BaseURL: srv.URL, CacheDir: dir})
	body, ct, err := c.Get(context.Background(), "tiles.json")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(body) != `{"fresh":true}` {
		t.Fatalf("body: %s", body)
	}
	if ct != "application/json" {
		t.Fatalf("ct: %s", ct)
	}
	if _, err := os.Stat(filepath.Join(dir, "tiles.json")); err != nil {
		t.Fatalf("expected disk write, got %v", err)
	}
}

func TestCache_Get_BothMissReturnsUpstreamErr(t *testing.T) {
	// Closed-port URL => upstream fetch immediately fails.
	c := New(Config{BaseURL: "http://127.0.0.1:1", CacheDir: t.TempDir()})
	if _, _, err := c.Get(context.Background(), "tiles.json"); err == nil {
		t.Fatalf("expected error when both disk and upstream fail")
	}
}

func TestCache_Get_RejectsBadPath(t *testing.T) {
	c := New(Config{BaseURL: "https://x", CacheDir: t.TempDir()})
	if _, _, err := c.Get(context.Background(), "../etc/passwd"); err == nil {
		t.Fatalf("expected rejection of path traversal")
	}
}
