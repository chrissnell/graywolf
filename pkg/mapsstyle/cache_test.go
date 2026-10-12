package mapsstyle

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

func TestCache_Get_RewritesStyleJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"glyphs":"https://maps.nw5w.com/style/roboto-glyphs/{fontstack}/{range}.pbf","sprite":"https://maps.nw5w.com/style/americana/sprites/sprite","sources":{}}`))
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, CacheDir: t.TempDir(), LocalPrefix: "/api/maps/style"})
	body, _, err := c.Get(context.Background(), "americana-roboto/style.json")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(string(body), `"glyphs":"/api/maps/style/roboto-glyphs/{fontstack}/{range}.pbf"`) {
		t.Fatalf("glyphs not rewritten in served body: %s", body)
	}
	if !strings.Contains(string(body), `"sprite":"/api/maps/style/americana/sprites/sprite"`) {
		t.Fatalf("sprite not rewritten: %s", body)
	}
}

// captureLogger returns a logger writing text records into buf.
func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestCache_Get_UpstreamFailureIsLoggedAndThrottled(t *testing.T) {
	var buf bytes.Buffer
	c := New(Config{BaseURL: "http://127.0.0.1:1", CacheDir: t.TempDir(), Logger: captureLogger(&buf)})
	clock := time.Unix(1_700_000_000, 0)
	c.now = func() time.Time { return clock }

	const msg = "upstream fetch failed and no usable cached copy"
	get := func(rel string) {
		t.Helper()
		if _, _, err := c.Get(context.Background(), rel); err == nil {
			t.Fatalf("Get(%q): expected error", rel)
		}
	}

	get("americana-roboto/style.json")
	if n := strings.Count(buf.String(), msg); n != 1 {
		t.Fatalf("after first failure: %d log lines, want 1\n%s", n, buf.String())
	}
	if !strings.Contains(buf.String(), "rel=americana-roboto/style.json") {
		t.Errorf("log line missing rel: %s", buf.String())
	}

	// Within the window: counted, not logged.
	get("tiles.json")
	get("americana/shields.json")
	if n := strings.Count(buf.String(), msg); n != 1 {
		t.Fatalf("within window: %d log lines, want 1\n%s", n, buf.String())
	}

	// After the window: logged again with the suppressed count.
	clock = clock.Add(upstreamFailureLogInterval)
	get("tiles.json")
	if n := strings.Count(buf.String(), msg); n != 2 {
		t.Fatalf("after window: %d log lines, want 2\n%s", n, buf.String())
	}
	if !strings.Contains(buf.String(), "suppressed=2") {
		t.Errorf("second log line missing suppressed=2: %s", buf.String())
	}
}

func TestCache_Get_CanceledContextNotLogged(t *testing.T) {
	var buf bytes.Buffer
	c := New(Config{BaseURL: "http://127.0.0.1:1", CacheDir: t.TempDir(), Logger: captureLogger(&buf)})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := c.Get(ctx, "tiles.json"); err == nil {
		t.Fatalf("expected error")
	}
	if buf.Len() != 0 {
		t.Errorf("canceled request logged: %s", buf.String())
	}
}

func TestCache_FetchUpstream_ErrorRedactsToken(t *testing.T) {
	const tok = "s3cret-bearer-token"
	var buf bytes.Buffer
	c := New(Config{
		BaseURL:       "http://127.0.0.1:1",
		CacheDir:      t.TempDir(),
		Logger:        captureLogger(&buf),
		TokenProvider: func(context.Context) string { return tok },
	})
	_, _, err := c.Get(context.Background(), "tiles.json")
	if err == nil {
		t.Fatalf("expected error")
	}
	if strings.Contains(err.Error(), tok) {
		t.Errorf("error leaks token: %v", err)
	}
	if strings.Contains(buf.String(), tok) {
		t.Errorf("log leaks token: %s", buf.String())
	}
}

func TestCache_Get_CorruptCachedJSONIsRefetched(t *testing.T) {
	var upstreamCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.Write([]byte(`{"fresh":true}`))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	dir := t.TempDir()
	c := New(Config{BaseURL: srv.URL, CacheDir: dir, Logger: captureLogger(&buf)})
	if err := c.writeDisk("tiles.json", []byte(`{"truncated":`)); err != nil {
		t.Fatal(err)
	}
	body, _, err := c.Get(context.Background(), "tiles.json")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(body) != `{"fresh":true}` {
		t.Fatalf("expected refetched body, got %s", body)
	}
	if upstreamCalls.Load() != 1 {
		t.Fatalf("expected 1 upstream call, got %d", upstreamCalls.Load())
	}
	if !strings.Contains(buf.String(), "cached copy is corrupt") {
		t.Errorf("corrupt cache not logged: %s", buf.String())
	}
	// The good copy replaced the corrupt one on disk.
	onDisk, err := os.ReadFile(filepath.Join(dir, "tiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != `{"fresh":true}` {
		t.Fatalf("disk not repaired, got %s", onDisk)
	}
}

func TestCache_Get_CorruptCachedJSONUpstreamDownReturnsError(t *testing.T) {
	var buf bytes.Buffer
	c := New(Config{BaseURL: "http://127.0.0.1:1", CacheDir: t.TempDir(), Logger: captureLogger(&buf)})
	if err := c.writeDisk("americana-roboto/style.json", []byte(`<html>`)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Get(context.Background(), "americana-roboto/style.json"); err == nil {
		t.Fatalf("expected error instead of serving corrupt cached copy")
	}
	if !strings.Contains(buf.String(), "no usable cached copy") {
		t.Errorf("upstream failure not logged: %s", buf.String())
	}
}

func TestCache_Get_InvalidUpstreamJSONNotCached(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// e.g. a captive portal answering 200 with its login page
		w.Write([]byte(`<html><body>Sign in to Wi-Fi</body></html>`))
	}))
	defer srv.Close()
	dir := t.TempDir()
	c := New(Config{BaseURL: srv.URL, CacheDir: dir})
	if _, _, err := c.Get(context.Background(), "tiles.json"); err == nil {
		t.Fatalf("expected error for non-JSON upstream body")
	}
	if _, err := os.Stat(filepath.Join(dir, "tiles.json")); !os.IsNotExist(err) {
		t.Fatalf("invalid upstream body was written to disk (stat err=%v)", err)
	}
}

func TestCache_Get_BinaryAssetsNotValidated(t *testing.T) {
	c := New(Config{BaseURL: "http://127.0.0.1:1", CacheDir: t.TempDir()})
	png := []byte{0x89, 'P', 'N', 'G'}
	if err := c.writeDisk("americana/sprites/sprite.png", png); err != nil {
		t.Fatal(err)
	}
	body, _, err := c.Get(context.Background(), "americana/sprites/sprite.png")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(body, png) {
		t.Fatalf("binary asset altered: %v", body)
	}
}

func TestCache_CheckWritable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tiles", "style")
	c := New(Config{CacheDir: dir})
	if err := c.CheckWritable(); err != nil {
		t.Fatalf("CheckWritable on fresh dir: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("cache dir not created: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("probe file left behind: %v", entries)
	}
}

func TestCache_CheckWritable_ReadOnlyDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	c := New(Config{CacheDir: dir})
	err := c.CheckWritable()
	if err == nil {
		t.Fatalf("expected error for read-only dir")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("error doesn't name the directory: %v", err)
	}
}
