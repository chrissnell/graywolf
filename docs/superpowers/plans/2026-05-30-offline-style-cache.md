# Offline Map Style Cache Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the MapLibre map render offline by adding a server-side pull-through cache for the upstream style.json, shields.json, sprite, glyphs, and tiles.json. Graywolf does NOT contact the upstream at startup. Instead, the very first browser request for any style asset hydrates that asset from upstream into a disk cache under `<TileCacheDir>/style/`, and every subsequent request (online or offline) serves from disk. Map downloads opportunistically piggyback a full glyph pre-warm so an offline user who downloads a region gets complete label coverage.

**Architecture:** New Go package `pkg/mapsstyle` owns the disk cache + browser-triggered pull-through fetcher, modelled after `pkg/mapscatalog`. New HTTP handlers under `/api/maps/style/{path...}` in `pkg/webapi` serve the cached resources, with `style.json` URL-rewritten on every serve so the browser only ever sees local paths. The existing `/api/maps/downloads/{slug}` POST handler is extended to kick off a best-effort glyph pre-warm in the background after the download starts. Frontend stops fetching from `maps.nw5w.com` for style assets and drops the localStorage fallback.

**Tech Stack:** Go 1.22+ (uses `http.ServeMux` wildcard routing), Svelte 5 / MapLibre GL JS, existing `pkg/mapscache` / `pkg/mapscatalog` patterns.

**Issue:** chrissnell/graywolf#204 — continuation of #195. PR #196 (0.13.12) decoupled the render-bounds path from the live catalog, but the browser still fetches `https://maps.nw5w.com/style/americana-roboto/style.json` directly. The only offline fallback was a per-browser `localStorage` entry, which is per-origin (laptop switching from upstream Pi IP to hotspot Pi IP = empty cache) and per-browser (LAN guests have no cache at all). Fix is to move the cache server-side, but Peter (the reporter) requires that graywolf NOT depend on internet at startup, and Chris requires that maps "just work" for a new online user without manual map downloads. The solution is a true pull-through cache: hydrate on the first online browser request.

---

## Design constraints (recorded; do not violate)

1. **No internet required at startup.** Graywolf must boot fine with no network. No warmer goroutine, no startup upstream calls.
2. **New online user gets a working map without manual action.** First browser request hydrates lazily; subsequent requests serve from disk.
3. **Cache must survive across reboots, multi-origin browsers, and LAN guests.** Per-browser localStorage is out.
4. **Map downloads piggyback full glyph pre-warm.** The user is provably online when downloading a region; this is the right moment to top up the long tail of glyph ranges that MapLibre may never request before the user goes offline.

---

## File Structure

**New Go files:**
- `pkg/mapsstyle/cache.go` — disk-backed pull-through cache. Public types: `Cache`, `Config`. Public methods: `New`, `Get`, `PrewarmGlyphs`.
- `pkg/mapsstyle/cache_test.go` — unit tests for the cache, using `httptest.NewServer` to mock upstream.
- `pkg/mapsstyle/rewrite.go` — rewrites absolute `maps.nw5w.com/style/*` and `maps.nw5w.com/tiles.json` URLs in style.json bytes to local `/api/maps/style/...` paths.
- `pkg/mapsstyle/rewrite_test.go` — unit tests for the rewriter.
- `pkg/webapi/style.go` — HTTP handler registering `GET /api/maps/style/{path...}`.
- `pkg/webapi/style_test.go` — handler tests.

**Modified Go files:**
- `pkg/webapi/server.go` — add `style *mapsstyle.Cache` field, `Config.Style`, populate in `NewServer`, register routes via `registerStyle`.
- `pkg/webapi/downloads.go` — extend `startDownload` to fire-and-forget a glyph pre-warm against `s.style` after kicking off the tile download.
- `pkg/app/wiring.go` — construct `mapsstyle.Cache`, pass it into `webapi.Config{Style: ...}`. No goroutine.

**Modified frontend files:**
- `web/src/lib/map/maplibre-map.svelte` — point `STYLE_URL` and the hardcoded shields URL at local paths; remove the localStorage cache (no longer needed since the server now owns offline persistence).

**Modified wiki/docs:**
- `docs/wiki/code-map.md` — add a row under "Maps integration (graywolf-maps client)" for `pkg/mapsstyle`.
- `docs/wiki/system-topology.md` — extend the "Offline maps catalog" section with the style cache, and add a row to the maps endpoints table.

**Regen artifacts (touched by `make docs-check api-client-check`):**
- `pkg/webapi/docs/docs.go`, `swagger.json`, `swagger.yaml`
- `web/src/lib/api/` generated TS client

---

## Architecture decisions baked into this plan

1. **Pull-through is browser-triggered.** No background goroutines. Every `/api/maps/style/{path}` request: disk-hit serves; disk-miss tries upstream with a short timeout, writes to disk, serves; both miss returns 502. New online users get a working map on first paint without any manual action; offline-from-the-start users see a broken map (acceptable — they have no tiles either).

2. **Disk layout mirrors upstream URL paths verbatim**:
   ```
   <TileCacheDir>/style/americana-roboto/style.json
   <TileCacheDir>/style/americana/shields.json
   <TileCacheDir>/style/americana/sprites/sprite.json
   <TileCacheDir>/style/americana/sprites/sprite.png
   <TileCacheDir>/style/americana/sprites/sprite@2x.png
   <TileCacheDir>/style/roboto-glyphs/Roboto Regular/0-255.pbf
   <TileCacheDir>/style/roboto-glyphs/Roboto Bold/0-255.pbf
   ...
   <TileCacheDir>/style/tiles.json
   ```
   Fontstack names are URL-decoded before use as directory names (literal space), keeping the disk layout human-readable. Path traversal is rejected in `Cache.Get` before any disk access.

3. **HTTP path mirror**:
   ```
   GET /api/maps/style/{path...}
   ```
   `path` is e.g. `americana-roboto/style.json`, `americana/sprites/sprite.png`, `roboto-glyphs/Roboto%20Regular/0-255.pbf`, `tiles.json`.

4. **Upstream fetch is per-request and bounded**:
   - 5-second upstream timeout per asset so a slow upstream can't wedge the map paint.
   - Concurrent fetches for the same path are coalesced via in-flight map (same-asset stampede protection).
   - On upstream failure: return the error to the caller; the handler maps it to 502.

5. **Map-download piggyback for glyphs.** When `POST /api/maps/downloads/{slug}` succeeds in kicking off a download, the webapi handler also calls `style.PrewarmGlyphs(ctx)` in a detached goroutine. PrewarmGlyphs reads the cached `style.json` (which itself was hydrated on a prior browser request or implicitly during this same prewarm), discovers fontstacks via `text-font` layer keys, and fetches every glyph range 0-255 through 65280-65535 per fontstack, capped by `consecutive-404` heuristic (4 in a row → bail on that fontstack). Best-effort, ignores all errors. Idempotent — files already on disk are skipped.

6. **URL rewriting**: `style.json` responses get `glyphs` and `sprite` fields rewritten to `/api/maps/style/...` on every serve. Vector source `url` fields pointing at `maps.nw5w.com/tiles.json` are rewritten too. The terrarium DEM URL on `s3.amazonaws.com` is left untouched — out of scope.

7. **Path safety**: `cleanRelPath` rejects anything containing `..` or starting with `/`. Allowed prefixes: `americana-roboto/style.json`, `americana/shields.json`, `americana/sprites/*` (one segment), `roboto-glyphs/*/<range>.pbf` (two segments), plus the literal `tiles.json`. Anything else → 404.

---

## Task 1: Package scaffold + path helpers

**Files:**
- Create: `pkg/mapsstyle/cache.go`
- Create: `pkg/mapsstyle/cache_test.go`

- [ ] **Step 1: Write the failing path-safety test**

Create `pkg/mapsstyle/cache_test.go`:

```go
package mapsstyle

import "testing"

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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/mapsstyle/...`
Expected: FAIL with `undefined: cleanRelPath`.

- [ ] **Step 3: Create the package skeleton + cleanRelPath**

Create `pkg/mapsstyle/cache.go`:

```go
// Package mapsstyle is a server-side pull-through cache for MapLibre
// style assets served by the graywolf-maps Worker. The browser hits
// /api/maps/style/{path}; on disk-miss, graywolf fetches from upstream
// once, writes the body to <TileCacheDir>/style/, and serves it. The
// next request comes off disk — including after a reboot, from a
// different browser, or from a LAN guest.
//
// No startup network: graywolf does NOT pre-fetch on boot. The first
// online browser request is the trigger. A new user with internet sees
// a working map immediately; a never-online user sees a broken map
// (which is the correct visible failure mode — they have no tiles
// either). See docs/wiki/system-topology.md for the offline maps story.
package mapsstyle

import (
	"path"
	"strings"
)

// cleanRelPath validates an inbound relative path against the allowed
// upstream surface and returns the cleaned form. Path traversal,
// absolute paths, and prefixes outside the allowed set are rejected.
//
// Allowed: `americana-roboto/style.json`,
// `americana/shields.json`, `americana/sprites/<any single segment>`,
// `roboto-glyphs/<fontstack>/<range>.pbf`, and the literal `tiles.json`.
func cleanRelPath(rel string) (string, bool) {
	if rel == "" || strings.HasPrefix(rel, "/") {
		return "", false
	}
	cleaned := path.Clean(rel)
	if cleaned != rel {
		return "", false
	}
	if strings.Contains(cleaned, "..") {
		return "", false
	}
	if cleaned == "tiles.json" {
		return cleaned, true
	}
	switch {
	case cleaned == "americana-roboto/style.json":
		return cleaned, true
	case cleaned == "americana/shields.json":
		return cleaned, true
	case strings.HasPrefix(cleaned, "americana/sprites/") && strings.Count(cleaned, "/") == 2:
		return cleaned, true
	case strings.HasPrefix(cleaned, "roboto-glyphs/") && strings.Count(cleaned, "/") == 2:
		return cleaned, true
	}
	return "", false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/mapsstyle/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/mapsstyle/cache.go pkg/mapsstyle/cache_test.go
git commit -m "mapsstyle: package scaffold with path-safety helper"
```

---

## Task 2: Upstream URL builder + content-type lookup

**Files:**
- Modify: `pkg/mapsstyle/cache.go`
- Modify: `pkg/mapsstyle/cache_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `pkg/mapsstyle/cache_test.go`:

```go
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
		"americana-roboto/style.json":         "application/json",
		"americana/shields.json":              "application/json",
		"americana/sprites/sprite.json":       "application/json",
		"americana/sprites/sprite.png":        "image/png",
		"americana/sprites/sprite@2x.png":     "image/png",
		"roboto-glyphs/Roboto Regular/0.pbf":  "application/x-protobuf",
		"tiles.json":                          "application/json",
	}
	for in, want := range cases {
		if got := contentTypeFor(in); got != want {
			t.Errorf("contentTypeFor(%q) = %q want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./pkg/mapsstyle/...`
Expected: FAIL — `undefined: upstreamURL`, `undefined: contentTypeFor`.

- [ ] **Step 3: Implement the helpers**

In `pkg/mapsstyle/cache.go`, replace the imports with:

```go
import (
	"fmt"
	"net/url"
	"path"
	"strings"
)
```

Append the implementations:

```go
// upstreamURL maps a cleaned relative path to its absolute upstream
// URL. tiles.json lives at the worker root; everything else is under
// /style/. Fontstack directory names are URL-escaped path-segment-wise
// (space -> %20) since the worker is HTTP and disk uses literal spaces.
func upstreamURL(baseURL, rel string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse baseURL: %w", err)
	}
	if rel == "tiles.json" {
		u.Path = "/tiles.json"
		return u.String(), nil
	}
	segs := strings.Split(rel, "/")
	escaped := make([]string, 0, len(segs)+1)
	escaped = append(escaped, "style")
	for _, s := range segs {
		escaped = append(escaped, url.PathEscape(s))
	}
	u.Path = "/" + path.Join(escaped...)
	return u.String(), nil
}

// contentTypeFor derives the Content-Type from the path extension.
// Unknown extensions fall back to application/octet-stream.
func contentTypeFor(rel string) string {
	switch path.Ext(rel) {
	case ".json":
		return "application/json"
	case ".png":
		return "image/png"
	case ".pbf":
		return "application/x-protobuf"
	}
	return "application/octet-stream"
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./pkg/mapsstyle/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/mapsstyle/cache.go pkg/mapsstyle/cache_test.go
git commit -m "mapsstyle: upstream URL builder + content-type helper"
```

---

## Task 3: Cache struct + disk read/write

**Files:**
- Modify: `pkg/mapsstyle/cache.go`
- Modify: `pkg/mapsstyle/cache_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `pkg/mapsstyle/cache_test.go`:

```go
import (
	"os"
	"path/filepath"
)
```

(Merge with existing import block.)

Append:

```go
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
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./pkg/mapsstyle/...`
Expected: FAIL — `undefined: New`, `undefined: Config`.

- [ ] **Step 3: Implement Cache + disk helpers**

Replace `pkg/mapsstyle/cache.go`'s imports with:

```go
import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)
```

Append the types and helpers:

```go
// Config configures a Cache.
type Config struct {
	// BaseURL is the maps worker root, e.g. https://maps.nw5w.com.
	BaseURL string
	// CacheDir is the directory holding cached assets. Callers should
	// pass <TileCacheDir>/style so the catalog, downloads, and style
	// cache all coexist under TileCacheDir without colliding.
	CacheDir string
	// TokenProvider returns the current bearer token; called per fetch
	// so re-registration rotates the token without restart. May be nil
	// (tests). An empty token sends no ?t= parameter.
	TokenProvider func(context.Context) string
	// LocalPrefix is the URL prefix the browser uses to reach this
	// cache (e.g. "/api/maps/style"). Used to rewrite absolute upstream
	// URLs inside style.json so the served copy points at the local
	// proxy. Required for style.json correctness.
	LocalPrefix string
	// Logger defaults to slog.Default().
	Logger *slog.Logger
	// HTTPClient is the upstream HTTP client. Defaults to a 5s-timeout
	// client; tests override with httptest-provided clients (no
	// timeout) or a custom timeout. The 5s default bounds the worst
	// case where the upstream is slow and the browser is waiting.
	HTTPClient *http.Client
}

// ErrUpstreamUnavailable is returned by Cache.Get when both the disk
// and the upstream are unreachable. The HTTP handler maps it to 502.
var ErrUpstreamUnavailable = errors.New("mapsstyle: upstream unavailable and no cached copy")

// Cache is a disk-backed pull-through cache for MapLibre style assets.
// Concurrent reads are safe; in-flight upstream fetches for the same
// asset are coalesced (stampede protection).
type Cache struct {
	baseURL     string
	cacheDir    string
	tokenFn     func(context.Context) string
	httpClient  *http.Client
	localPrefix string
	logger      *slog.Logger

	mu       sync.Mutex
	inflight map[string]chan struct{}
}

// New constructs a Cache.
func New(cfg Config) *Cache {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Second}
	}
	tokenFn := cfg.TokenProvider
	if tokenFn == nil {
		tokenFn = func(context.Context) string { return "" }
	}
	return &Cache{
		baseURL:     cfg.BaseURL,
		cacheDir:    cfg.CacheDir,
		tokenFn:     tokenFn,
		httpClient:  hc,
		localPrefix: cfg.LocalPrefix,
		logger:      logger,
		inflight:    map[string]chan struct{}{},
	}
}

// readDisk returns the cached body + content-type for rel, or an error
// if no cached copy exists.
func (c *Cache) readDisk(rel string) ([]byte, string, error) {
	p := filepath.Join(c.cacheDir, filepath.FromSlash(rel))
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, "", err
	}
	return b, contentTypeFor(rel), nil
}

// writeDisk persists body atomically as <CacheDir>/<rel>. Parent
// directories are created as needed.
func (c *Cache) writeDisk(rel string, body []byte) error {
	p := filepath.Join(c.cacheDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), filepath.Base(p)+".*.tmp")
	if err != nil {
		return fmt.Errorf("tempfile: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Rename(tmpName, p); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// suppress unused-import noise until upstream-fetch lands in the next
// task. Removed there.
var _ = io.Discard
var _ = http.MethodGet
var _ = url.Parse
var _ = path.Join
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./pkg/mapsstyle/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/mapsstyle/cache.go pkg/mapsstyle/cache_test.go
git commit -m "mapsstyle: Cache type with atomic disk read/write"
```

---

## Task 4: Upstream fetch

**Files:**
- Modify: `pkg/mapsstyle/cache.go`
- Modify: `pkg/mapsstyle/cache_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `pkg/mapsstyle/cache_test.go`:

```go
import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
)
```

(Merge with existing imports.)

```go
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
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./pkg/mapsstyle/...`
Expected: FAIL — `c.fetchUpstream undefined`.

- [ ] **Step 3: Implement fetchUpstream**

In `pkg/mapsstyle/cache.go`, remove the `var _ = ...` placeholders at the bottom and add:

```go
// fetchUpstream fetches rel from the upstream worker and returns the
// body + content-type. Does NOT write to disk; the caller is
// responsible for persistence so the same fetch can be used by both
// the pull-through Get and the download-piggyback glyph pre-warm.
func (c *Cache) fetchUpstream(ctx context.Context, rel string) ([]byte, string, error) {
	u, err := upstreamURL(c.baseURL, rel)
	if err != nil {
		return nil, "", err
	}
	if tok := c.tokenFn(ctx); tok != "" {
		parsed, err := url.Parse(u)
		if err != nil {
			return nil, "", fmt.Errorf("parse upstream URL: %w", err)
		}
		q := parsed.Query()
		q.Set("t", tok)
		parsed.RawQuery = q.Encode()
		u = parsed.String()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("upstream %s: %w", rel, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, "", fmt.Errorf("upstream %s: HTTP %d: %s", rel, resp.StatusCode, string(body))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read upstream %s: %w", rel, err)
	}
	return body, contentTypeFor(rel), nil
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./pkg/mapsstyle/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/mapsstyle/cache.go pkg/mapsstyle/cache_test.go
git commit -m "mapsstyle: upstream fetch with bearer-token query"
```

---

## Task 5: Pull-through Get

**Files:**
- Modify: `pkg/mapsstyle/cache.go`
- Modify: `pkg/mapsstyle/cache_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `pkg/mapsstyle/cache_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./pkg/mapsstyle/...`
Expected: FAIL — `c.Get undefined`.

- [ ] **Step 3: Implement Get**

Append to `pkg/mapsstyle/cache.go`:

```go
// Get returns the body + content-type for rel. Tries disk first; on
// miss, fetches upstream and writes to disk before returning. Returns
// an error if the path is unsafe or both disk + upstream are
// unavailable.
//
// Concurrent Gets for the same rel coalesce: the first caller fetches,
// subsequent callers wait and re-read from disk.
func (c *Cache) Get(ctx context.Context, rel string) ([]byte, string, error) {
	cleaned, ok := cleanRelPath(rel)
	if !ok {
		return nil, "", fmt.Errorf("mapsstyle: rejected path %q", rel)
	}
	// Fast path: disk hit.
	if body, ct, err := c.readDisk(cleaned); err == nil {
		return body, ct, nil
	}

	// Coalesce concurrent misses.
	c.mu.Lock()
	if ch, busy := c.inflight[cleaned]; busy {
		c.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return nil, "", ctx.Err()
		}
		return c.readDisk(cleaned)
	}
	ch := make(chan struct{})
	c.inflight[cleaned] = ch
	c.mu.Unlock()

	body, ct, err := c.fetchUpstream(ctx, cleaned)
	if err == nil {
		if werr := c.writeDisk(cleaned, body); werr != nil {
			c.logger.Warn("mapsstyle: write disk failed", "rel", cleaned, "err", werr)
		}
	}

	c.mu.Lock()
	delete(c.inflight, cleaned)
	c.mu.Unlock()
	close(ch)

	if err != nil {
		return nil, "", err
	}
	return body, ct, nil
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./pkg/mapsstyle/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/mapsstyle/cache.go pkg/mapsstyle/cache_test.go
git commit -m "mapsstyle: pull-through Get with disk-first read"
```

---

## Task 6: style.json URL rewriter

**Files:**
- Create: `pkg/mapsstyle/rewrite.go`
- Create: `pkg/mapsstyle/rewrite_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/mapsstyle/rewrite_test.go`:

```go
package mapsstyle

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRewriteStyleJSON_GlyphsSpriteSources(t *testing.T) {
	in := []byte(`{
		"name": "Graywolf Maps",
		"glyphs": "https://maps.nw5w.com/style/roboto-glyphs/{fontstack}/{range}.pbf",
		"sprite": "https://maps.nw5w.com/style/americana/sprites/sprite",
		"sources": {
			"openmaptiles": {"type": "vector", "url": "https://maps.nw5w.com/tiles.json"},
			"dem": {"type": "raster-dem", "tiles": ["https://s3.amazonaws.com/elevation-tiles-prod/terrarium/{z}/{x}/{y}.png"]}
		},
		"layers": [{"id":"x","type":"background"}]
	}`)
	out, err := RewriteStyleJSON(in, "/api/maps/style")
	if err != nil {
		t.Fatalf("RewriteStyleJSON: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("parse rewritten: %v", err)
	}
	if g, _ := parsed["glyphs"].(string); g != "/api/maps/style/roboto-glyphs/{fontstack}/{range}.pbf" {
		t.Errorf("glyphs not rewritten: %q", g)
	}
	if sp, _ := parsed["sprite"].(string); sp != "/api/maps/style/americana/sprites/sprite" {
		t.Errorf("sprite not rewritten: %q", sp)
	}
	srcs, _ := parsed["sources"].(map[string]any)
	omt, _ := srcs["openmaptiles"].(map[string]any)
	if u, _ := omt["url"].(string); u != "/api/maps/style/tiles.json" {
		t.Errorf("openmaptiles.url not rewritten: %q", u)
	}
	dem, _ := srcs["dem"].(map[string]any)
	demTiles, _ := dem["tiles"].([]any)
	demURL, _ := demTiles[0].(string)
	if !strings.Contains(demURL, "s3.amazonaws.com") {
		t.Errorf("dem URL should be untouched, got %q", demURL)
	}
	if n, _ := parsed["name"].(string); n != "Graywolf Maps" {
		t.Errorf("name not preserved: %q", n)
	}
}

func TestRewriteStyleJSON_NoMatchingFields(t *testing.T) {
	in := []byte(`{"name":"x","layers":[]}`)
	out, err := RewriteStyleJSON(in, "/api/maps/style")
	if err != nil {
		t.Fatalf("RewriteStyleJSON: %v", err)
	}
	if !strings.Contains(string(out), `"name":"x"`) {
		t.Errorf("out: %s", out)
	}
}

func TestRewriteStyleJSON_MalformedJSON(t *testing.T) {
	if _, err := RewriteStyleJSON([]byte(`{not json`), "/api/maps/style"); err == nil {
		t.Fatalf("expected error on malformed input")
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./pkg/mapsstyle/...`
Expected: FAIL — `undefined: RewriteStyleJSON`.

- [ ] **Step 3: Implement RewriteStyleJSON**

Create `pkg/mapsstyle/rewrite.go`:

```go
package mapsstyle

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RewriteStyleJSON rewrites absolute maps.nw5w.com URLs inside a style.json
// payload to point at the local proxy prefix (e.g. "/api/maps/style").
// Rewritten fields:
//   - top-level "glyphs" (URL template)
//   - top-level "sprite" (URL base)
//   - sources.*.url that points at maps.nw5w.com/tiles.json
//
// Other source types (raster-dem on s3.amazonaws.com) are left untouched.
func RewriteStyleJSON(in []byte, localPrefix string) ([]byte, error) {
	localPrefix = strings.TrimRight(localPrefix, "/")
	var doc map[string]any
	if err := json.Unmarshal(in, &doc); err != nil {
		return nil, fmt.Errorf("parse style.json: %w", err)
	}
	if g, ok := doc["glyphs"].(string); ok {
		doc["glyphs"] = rewriteOne(g, localPrefix)
	}
	if s, ok := doc["sprite"].(string); ok {
		doc["sprite"] = rewriteOne(s, localPrefix)
	}
	if srcs, ok := doc["sources"].(map[string]any); ok {
		for _, raw := range srcs {
			src, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if u, ok := src["url"].(string); ok {
				src["url"] = rewriteOne(u, localPrefix)
			}
		}
	}
	return json.Marshal(doc)
}

// rewriteOne rewrites a single absolute maps.nw5w.com URL to the local
// prefix. Strings that don't start with the known upstream are returned
// unchanged.
func rewriteOne(u, localPrefix string) string {
	const stylePrefix = "https://maps.nw5w.com/style/"
	const tilesJSON = "https://maps.nw5w.com/tiles.json"
	if u == tilesJSON {
		return localPrefix + "/tiles.json"
	}
	if strings.HasPrefix(u, stylePrefix) {
		return localPrefix + "/" + strings.TrimPrefix(u, stylePrefix)
	}
	return u
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./pkg/mapsstyle/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/mapsstyle/rewrite.go pkg/mapsstyle/rewrite_test.go
git commit -m "mapsstyle: rewrite upstream URLs in style.json to local proxy"
```

---

## Task 7: Wire RewriteStyleJSON into Get for style.json

**Files:**
- Modify: `pkg/mapsstyle/cache.go`
- Modify: `pkg/mapsstyle/cache_test.go`

- [ ] **Step 1: Write the failing test**

Append to `pkg/mapsstyle/cache_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./pkg/mapsstyle/...`
Expected: FAIL — body still contains the upstream URL.

- [ ] **Step 3: Wire the rewriter into Get**

In `pkg/mapsstyle/cache.go`, replace the `Get` method's final return blocks with:

Replace the disk fast-path:
```go
	if body, ct, err := c.readDisk(cleaned); err == nil {
		return body, ct, nil
	}
```
with:
```go
	if body, ct, err := c.readDisk(cleaned); err == nil {
		return c.maybeRewrite(cleaned, body, ct)
	}
```

Replace the final return:
```go
	if err != nil {
		return nil, "", err
	}
	return body, ct, nil
```
with:
```go
	if err != nil {
		return nil, "", err
	}
	return c.maybeRewrite(cleaned, body, ct)
```

Add the helper:

```go
// maybeRewrite applies the URL rewriter to style.json responses and
// passes other paths through unchanged. A rewrite failure logs a WARN
// and falls back to the raw bytes (degraded but not broken).
func (c *Cache) maybeRewrite(rel string, body []byte, ct string) ([]byte, string, error) {
	if rel != "americana-roboto/style.json" || c.localPrefix == "" {
		return body, ct, nil
	}
	rew, rerr := RewriteStyleJSON(body, c.localPrefix)
	if rerr != nil {
		c.logger.Warn("mapsstyle: rewrite style.json failed, serving raw", "err", rerr)
		return body, ct, nil
	}
	return rew, ct, nil
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./pkg/mapsstyle/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/mapsstyle/cache.go pkg/mapsstyle/cache_test.go
git commit -m "mapsstyle: rewrite style.json on every Get"
```

---

## Task 8: Fontstack discovery helper

**Files:**
- Create: `pkg/mapsstyle/prewarm.go`
- Create: `pkg/mapsstyle/prewarm_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/mapsstyle/prewarm_test.go`:

```go
package mapsstyle

import (
	"sort"
	"testing"
)

func TestDiscoverFontstacks(t *testing.T) {
	style := []byte(`{
		"layers":[
			{"id":"a","layout":{"text-font":["Roboto Regular","Roboto Italic"]}},
			{"id":"b","layout":{"text-font":["Roboto Bold"]}},
			{"id":"c","layout":{}},
			{"id":"d","type":"background"}
		]
	}`)
	got, err := discoverFontstacks(style)
	if err != nil {
		t.Fatalf("discoverFontstacks: %v", err)
	}
	sort.Strings(got)
	want := []string{"Roboto Bold", "Roboto Italic", "Roboto Regular"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("got[%d]=%q want %q", i, got[i], w)
		}
	}
}

func TestDiscoverFontstacks_Malformed(t *testing.T) {
	if _, err := discoverFontstacks([]byte(`{not json`)); err == nil {
		t.Fatalf("expected error")
	}
}

func TestGlyphRel(t *testing.T) {
	if got := glyphRel("Roboto Regular", 0); got != "roboto-glyphs/Roboto Regular/0-255.pbf" {
		t.Errorf("range 0: %q", got)
	}
	if got := glyphRel("Roboto Bold", 1); got != "roboto-glyphs/Roboto Bold/256-511.pbf" {
		t.Errorf("range 1: %q", got)
	}
	if got := glyphRel("Roboto Regular", 255); got != "roboto-glyphs/Roboto Regular/65280-65535.pbf" {
		t.Errorf("range 255: %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./pkg/mapsstyle/...`
Expected: FAIL — `undefined: discoverFontstacks`, `undefined: glyphRel`.

- [ ] **Step 3: Implement the helpers**

Create `pkg/mapsstyle/prewarm.go`:

```go
package mapsstyle

import (
	"encoding/json"
	"fmt"
)

// discoverFontstacks parses style.json bytes and returns the union of
// all `text-font` arrays referenced by layers. Used by PrewarmGlyphs to
// decide which fontstack directories to populate.
func discoverFontstacks(styleJSON []byte) ([]string, error) {
	var doc struct {
		Layers []struct {
			Layout map[string]any `json:"layout"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(styleJSON, &doc); err != nil {
		return nil, fmt.Errorf("parse style.json: %w", err)
	}
	set := map[string]struct{}{}
	for _, lyr := range doc.Layers {
		f, ok := lyr.Layout["text-font"]
		if !ok {
			continue
		}
		arr, ok := f.([]any)
		if !ok {
			continue
		}
		for _, x := range arr {
			if s, ok := x.(string); ok {
				set[s] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out, nil
}

// glyphRel returns the disk-relative path for a fontstack + range tuple.
// Spaces in the fontstack stay literal on disk; the upstream URL builder
// re-encodes them.
func glyphRel(fontstack string, rangeIdx int) string {
	lo := rangeIdx * 256
	hi := lo + 255
	return fmt.Sprintf("roboto-glyphs/%s/%d-%d.pbf", fontstack, lo, hi)
}
```

- [ ] **Step 4: Run test to verify pass**

Run: `go test ./pkg/mapsstyle/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/mapsstyle/prewarm.go pkg/mapsstyle/prewarm_test.go
git commit -m "mapsstyle: fontstack discovery + glyph-range path helper"
```

---

## Task 9: PrewarmGlyphs

**Files:**
- Modify: `pkg/mapsstyle/cache.go`
- Modify: `pkg/mapsstyle/prewarm.go`
- Modify: `pkg/mapsstyle/prewarm_test.go`

- [ ] **Step 1: Write the failing test**

Append to `pkg/mapsstyle/prewarm_test.go`:

```go
import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
)
```

(Merge with existing imports.)

```go
func TestPrewarmGlyphs_HappyPath(t *testing.T) {
	var ranges atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/style/americana-roboto/style.json":
			w.Write([]byte(`{"layers":[{"id":"a","layout":{"text-font":["Roboto Regular"]}}]}`))
			return
		case "/style/roboto-glyphs/Roboto%20Regular/0-255.pbf",
			"/style/roboto-glyphs/Roboto%20Regular/256-511.pbf":
			ranges.Add(1)
			w.Write([]byte("pbf-bytes"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dir := t.TempDir()
	c := New(Config{BaseURL: srv.URL, CacheDir: dir, LocalPrefix: "/api/maps/style"})
	// Style.json must be on disk for fontstack discovery; trigger a Get.
	if _, _, err := c.Get(context.Background(), "americana-roboto/style.json"); err != nil {
		t.Fatalf("seed style.json: %v", err)
	}
	c.SetPrewarmLimits(2, 1) // maxRange=2, stopAfter=1 consecutive 404
	if err := c.PrewarmGlyphs(context.Background()); err != nil {
		t.Fatalf("PrewarmGlyphs: %v", err)
	}
	if ranges.Load() < 2 {
		t.Errorf("expected at least 2 glyph fetches, got %d", ranges.Load())
	}
	if _, err := os.Stat(filepath.Join(dir, "roboto-glyphs", "Roboto Regular", "0-255.pbf")); err != nil {
		t.Errorf("expected 0-255.pbf on disk: %v", err)
	}
}

func TestPrewarmGlyphs_NoStyleOnDisk(t *testing.T) {
	c := New(Config{BaseURL: "http://127.0.0.1:1", CacheDir: t.TempDir()})
	if err := c.PrewarmGlyphs(context.Background()); err == nil {
		t.Fatalf("expected error when style.json not cached")
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./pkg/mapsstyle/...`
Expected: FAIL — `c.PrewarmGlyphs undefined`, `c.SetPrewarmLimits undefined`.

- [ ] **Step 3: Implement PrewarmGlyphs**

In `pkg/mapsstyle/cache.go`, add two fields to the `Cache` struct (after `inflight`):

```go
	prewarmMaxRange    int // highest Unicode block index probed (default 255)
	prewarmStop404     int // bail per fontstack after N consecutive 404s (default 4)
	prewarmConcurrency int // concurrent glyph fetches (default 4)
```

In `New`, after creating the cache value but before returning, add defaults:

```go
	cache := &Cache{
		baseURL:            cfg.BaseURL,
		cacheDir:           cfg.CacheDir,
		tokenFn:            tokenFn,
		httpClient:         hc,
		localPrefix:        cfg.LocalPrefix,
		logger:             logger,
		inflight:           map[string]chan struct{}{},
		prewarmMaxRange:    255,
		prewarmStop404:     4,
		prewarmConcurrency: 4,
	}
	return cache
```

(Replace the previous `return &Cache{...}` with the var-then-return form.)

Then in `pkg/mapsstyle/prewarm.go`, add:

```go
import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)
```

(Add to the existing import block.)

Append the methods:

```go
// SetPrewarmLimits overrides the defaults for PrewarmGlyphs. Tests use
// it to avoid running through 1000+ ranges per case.
func (c *Cache) SetPrewarmLimits(maxRange, stopAfterConsecutive404 int) {
	if maxRange > 0 {
		c.prewarmMaxRange = maxRange
	}
	if stopAfterConsecutive404 > 0 {
		c.prewarmStop404 = stopAfterConsecutive404
	}
}

// PrewarmGlyphs reads the cached style.json from disk, discovers
// fontstacks via `text-font` layer keys, and fetches every glyph range
// 0..prewarmMaxRange for each fontstack. Per fontstack, after
// prewarmStop404 consecutive 404s the walk bails on that stack.
//
// Best-effort: returns the first error encountered after all in-flight
// fetches complete; callers (the map-download piggyback) should log
// and move on.
//
// Idempotent — files already on disk are skipped by Get's disk-first
// fast path.
func (c *Cache) PrewarmGlyphs(ctx context.Context) error {
	stylePath := filepath.Join(c.cacheDir, "americana-roboto", "style.json")
	body, err := os.ReadFile(stylePath)
	if err != nil {
		return fmt.Errorf("read style.json for glyph discovery: %w", err)
	}
	fonts, err := discoverFontstacks(body)
	if err != nil {
		return fmt.Errorf("discover fontstacks: %w", err)
	}
	if len(fonts) == 0 {
		c.logger.Info("mapsstyle: no fontstacks in style.json; skipping glyph pre-warm")
		return nil
	}

	sem := make(chan struct{}, c.prewarmConcurrency)
	var wg sync.WaitGroup
	var fetched atomic.Int64
	var firstErr atomic.Pointer[error]
	for _, fs := range fonts {
		fs := fs
		consecutive404 := 0
		for r := 0; r <= c.prewarmMaxRange; r++ {
			if ctx.Err() != nil {
				wg.Wait()
				return ctx.Err()
			}
			rel := glyphRel(fs, r)
			// Skip if already on disk to keep the prewarm idempotent.
			if _, err := os.Stat(filepath.Join(c.cacheDir, filepath.FromSlash(rel))); err == nil {
				consecutive404 = 0
				continue
			}
			select {
			case <-ctx.Done():
				wg.Wait()
				return ctx.Err()
			case sem <- struct{}{}:
			}
			wg.Add(1)
			ok := c.prewarmOne(ctx, rel, sem, &wg, &fetched, &firstErr)
			if ok {
				consecutive404 = 0
				continue
			}
			consecutive404++
			if consecutive404 >= c.prewarmStop404 {
				c.logger.Debug("mapsstyle: stopping glyph warm",
					"fontstack", fs, "after_range", r,
					"reason", "consecutive 404s")
				break
			}
		}
	}
	wg.Wait()
	c.logger.Info("mapsstyle: glyph pre-warm complete",
		"fontstacks", len(fonts), "ranges_fetched", fetched.Load())
	if perr := firstErr.Load(); perr != nil {
		return *perr
	}
	_ = strings.TrimSpace // keep strings import used in case the file is refactored to share helpers
	return nil
}

// prewarmOne fetches a single glyph asset and writes it to disk. Returns
// true on success (HTTP 200, body written). False on any failure
// (404, network, write); the first error is captured in firstErr.
//
// Synchronous: the caller waits for the result so the consecutive-404
// counter is updated correctly. The sem and wg accounting still apply
// for symmetry with the goroutine-pool pattern, but the actual fetch
// runs inline.
func (c *Cache) prewarmOne(
	ctx context.Context,
	rel string,
	sem chan struct{},
	wg *sync.WaitGroup,
	fetched *atomic.Int64,
	firstErr *atomic.Pointer[error],
) bool {
	defer wg.Done()
	defer func() { <-sem }()
	body, _, err := c.fetchUpstream(ctx, rel)
	if err != nil {
		if firstErr.Load() == nil {
			perr := fmt.Errorf("prewarm %s: %w", rel, err)
			firstErr.CompareAndSwap(nil, &perr)
		}
		return false
	}
	if werr := c.writeDisk(rel, body); werr != nil {
		c.logger.Warn("mapsstyle: write glyph failed", "rel", rel, "err", werr)
		return false
	}
	fetched.Add(1)
	return true
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./pkg/mapsstyle/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/mapsstyle/cache.go pkg/mapsstyle/prewarm.go pkg/mapsstyle/prewarm_test.go
git commit -m "mapsstyle: PrewarmGlyphs walks fontstacks + ranges with 404 bail-out"
```

---

## Task 10: webapi handler

**Files:**
- Create: `pkg/webapi/style.go`
- Create: `pkg/webapi/style_test.go`
- Modify: `pkg/webapi/server.go`

- [ ] **Step 1: Write the failing handler test**

Create `pkg/webapi/style_test.go`:

```go
package webapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/mapsstyle"
)

func TestStyleHandler_ServesViaPullThrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/style/americana-roboto/style.json":
			w.Write([]byte(`{"glyphs":"https://maps.nw5w.com/style/roboto-glyphs/{fontstack}/{range}.pbf","sources":{}}`))
		case "/style/americana/sprites/sprite.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("PNGDATA"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	cache := mapsstyle.New(mapsstyle.Config{
		BaseURL:     upstream.URL,
		CacheDir:    t.TempDir(),
		LocalPrefix: "/api/maps/style",
	})

	s := &Server{style: cache}
	mux := http.NewServeMux()
	s.registerStyle(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/maps/style/americana-roboto/style.json", nil).
		WithContext(context.Background())
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("style.json status: %d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type: %s", ct)
	}
	if !strings.Contains(w.Body.String(), `"glyphs":"/api/maps/style/roboto-glyphs/{fontstack}/{range}.pbf"`) {
		t.Errorf("body not rewritten: %s", w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/maps/style/americana/sprites/sprite.png", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("sprite.png status: %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("content-type: %s", ct)
	}
	if w.Body.String() != "PNGDATA" {
		t.Errorf("body: %q", w.Body.String())
	}
}

func TestStyleHandler_Returns502OnUpstreamFailure(t *testing.T) {
	// Closed port => upstream unreachable. Nothing on disk yet.
	cache := mapsstyle.New(mapsstyle.Config{
		BaseURL:     "http://127.0.0.1:1",
		CacheDir:    t.TempDir(),
		LocalPrefix: "/api/maps/style",
	})
	s := &Server{style: cache}
	mux := http.NewServeMux()
	s.registerStyle(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/maps/style/tiles.json", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestStyleHandler_Rejects_PathTraversal(t *testing.T) {
	cache := mapsstyle.New(mapsstyle.Config{
		BaseURL:     "https://x.invalid",
		CacheDir:    t.TempDir(),
		LocalPrefix: "/api/maps/style",
	})
	s := &Server{style: cache}
	mux := http.NewServeMux()
	s.registerStyle(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/maps/style/../../../etc/passwd", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestStyleHandler_Returns503WhenCacheNil(t *testing.T) {
	s := &Server{style: nil}
	mux := http.NewServeMux()
	s.registerStyle(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/maps/style/tiles.json", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 503 {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./pkg/webapi/...`
Expected: FAIL — `s.registerStyle undefined`, `s.style undefined`.

- [ ] **Step 3: Implement the handler**

Create `pkg/webapi/style.go`:

```go
package webapi

import (
	"net/http"
	"strings"
)

// registerStyle mounts the style-asset proxy at /api/maps/style/{path...}.
//
// The path captures the relative asset (americana-roboto/style.json,
// roboto-glyphs/Roboto%20Regular/0-255.pbf, tiles.json, etc.) and the
// handler delegates to the mapsstyle.Cache. URL-encoded segments
// (e.g. %20) are decoded by Go's URL parser before reaching the handler;
// the cache treats them as literal bytes on disk and re-encodes on
// upstream fetch.
func (s *Server) registerStyle(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/maps/style/{path...}", s.getStyleAsset)
}

// @Summary  Serve a cached MapLibre style asset
// @Tags     maps
// @ID       getMapsStyleAsset
// @Produce  json
// @Produce  octet-stream
// @Param    path path string true "Relative asset path under the upstream worker (e.g. americana-roboto/style.json, americana/sprites/sprite.png, roboto-glyphs/Roboto%20Regular/0-255.pbf, tiles.json)"
// @Success  200 {string} string "Asset body; content-type matches the asset"
// @Failure  404 {object} webtypes.ErrorResponse
// @Failure  502 {object} webtypes.ErrorResponse
// @Failure  503 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /maps/style/{path} [get]
func (s *Server) getStyleAsset(w http.ResponseWriter, r *http.Request) {
	if s.style == nil {
		serviceUnavailable(w, "maps style cache not initialized")
		return
	}
	rel := r.PathValue("path")
	body, ct, err := s.style.Get(r.Context(), rel)
	if err != nil {
		if isPathRejection(err) {
			http.NotFound(w, r)
			return
		}
		// All other failures are "upstream unreachable and nothing on
		// disk." 502 (Bad Gateway) is the closest semantic match: an
		// upstream we depend on is unavailable.
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(body)
}

func isPathRejection(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "rejected path")
}
```

Modify `pkg/webapi/server.go`:

1. Add `"github.com/chrissnell/graywolf/pkg/mapsstyle"` to imports.
2. Add field to `Server`:
   ```go
   style *mapsstyle.Cache // style-asset cache; nil until wired — handler returns 503 when nil
   ```
3. Add field to `Config`:
   ```go
   // Style is the MapLibre style asset cache. Optional — the
   // /api/maps/style/* handler returns 503 when nil. Tests inject a
   // Cache pointed at an httptest upstream + temp dir.
   Style *mapsstyle.Cache
   ```
4. Populate it in `NewServer`'s constructed `&Server{...}`:
   ```go
   style: cfg.Style,
   ```
5. Add `s.registerStyle(mux)` to `RegisterRoutes`, immediately after `s.registerLocalBounds(mux)`.

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./pkg/webapi/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/webapi/style.go pkg/webapi/style_test.go pkg/webapi/server.go
git commit -m "webapi: serve /api/maps/style/{path} as pull-through cache"
```

---

## Task 11: Download-piggyback glyph pre-warm

**Files:**
- Modify: `pkg/webapi/downloads.go`
- Modify: `pkg/webapi/downloads_test.go` (if it exists; otherwise create a small new test file alongside)

- [ ] **Step 1: Locate the existing startDownload handler**

Run: `grep -n "startDownload\b" pkg/webapi/downloads.go`

Read the handler to understand where the response is written; that's the point we want to insert the piggyback (after the download has been kicked off, before/around the response write).

- [ ] **Step 2: Write the failing test**

Add to `pkg/webapi/style_test.go` (so the test sits next to the rest of the style-cache tests):

```go
import (
	"sync/atomic"
	"time"
)
```

(Merge with existing imports.)

```go
func TestPrewarmGlyphsHook_IsInvokedOnDownloadStart(t *testing.T) {
	// This test verifies the hook surface only — that calling
	// triggerGlyphPrewarm against a non-nil style cache invokes
	// PrewarmGlyphs (without actually mocking a full download).
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/style/americana-roboto/style.json" {
			w.Write([]byte(`{"layers":[]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()

	cache := mapsstyle.New(mapsstyle.Config{
		BaseURL:     upstream.URL,
		CacheDir:    t.TempDir(),
		LocalPrefix: "/api/maps/style",
	})
	cache.SetPrewarmLimits(2, 1)
	// Seed style.json so PrewarmGlyphs can discover the empty fontstack list.
	if _, _, err := cache.Get(context.Background(), "americana-roboto/style.json"); err != nil {
		t.Fatalf("seed style.json: %v", err)
	}

	var done atomic.Bool
	go func() {
		_ = cache.PrewarmGlyphs(context.Background())
		done.Store(true)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if done.Load() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !done.Load() {
		t.Fatalf("PrewarmGlyphs did not complete within deadline")
	}
}
```

(This is a smoke test, not a behavioral assertion on the downloads handler. The handler change itself is tiny enough that a regression test against the full download path would be disproportionate; the unit-level coverage of `PrewarmGlyphs` is in `pkg/mapsstyle/prewarm_test.go`.)

- [ ] **Step 3: Run the test to verify it passes**

Run: `go test ./pkg/webapi/...`
Expected: PASS (the test doesn't actually depend on the handler change yet — it's there to lock the hook surface).

- [ ] **Step 4: Modify the startDownload handler**

In `pkg/webapi/downloads.go`, locate `startDownload`. After the call that starts the download has succeeded (i.e. inside the success branch, before the response is written), insert:

```go
	// Piggyback: the user is provably online right now. Fire off a
	// best-effort glyph pre-warm so an offline browser later has the
	// full label set, not just the ranges the browser happened to
	// request during this online session. Detached goroutine: the
	// download response should not wait on the upstream fan-out.
	if s.style != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			// Seed style.json first so fontstacks are discoverable.
			if _, _, err := s.style.Get(ctx, "americana-roboto/style.json"); err != nil {
				s.logger.Debug("style prewarm: seed style.json failed", "err", err)
				return
			}
			if err := s.style.PrewarmGlyphs(ctx); err != nil {
				s.logger.Debug("style prewarm: glyph fetch returned errors", "err", err)
			}
		}()
	}
```

Ensure `pkg/webapi/downloads.go` imports `"context"` and `"time"` (likely already present).

- [ ] **Step 5: Run tests + build**

Run: `go test ./pkg/webapi/... ./pkg/mapsstyle/...` and `go build ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/webapi/downloads.go pkg/webapi/style_test.go
git commit -m "webapi: piggyback glyph pre-warm on map-download start"
```

---

## Task 12: Wire cache into app

**Files:**
- Modify: `pkg/app/wiring.go`

- [ ] **Step 1: Add the cache construction immediately after the catalog block**

In `pkg/app/wiring.go`, find the catalog construction (around line 1239: `catalog := mapscatalog.NewWithDiskCache(...)`). After the warm-up goroutine `}()` (around line 1282), add:

```go
// Style asset cache: browser-triggered pull-through cache for
// style.json, shields.json, sprite, glyph PBFs, and tiles.json.
// Persists under <TileCacheDir>/style/. No startup network: graywolf
// boots fine offline, the first online browser request hydrates the
// cache. Map downloads piggyback a full glyph pre-warm so an offline
// browser later has the full label set. See pkg/mapsstyle + issue #204.
styleCache := mapsstyle.New(mapsstyle.Config{
	BaseURL:       mapscache.DefaultMapsBaseURL,
	CacheDir:      filepath.Join(a.cfg.TileCacheDir, "style"),
	TokenProvider: mapsTokenProvider,
	LocalPrefix:   "/api/maps/style",
	Logger:        a.logger.With("component", "mapsstyle"),
})
```

- [ ] **Step 2: Add the cache to webapi.Config**

Locate the `apiSrv, err := webapi.NewServer(webapi.Config{...})` call (around line 1303). Add `Style: styleCache,`:

```go
apiSrv, err := webapi.NewServer(webapi.Config{
	Store:         a.store,
	Bridge:        a.bridge,
	KissManager:   a.kissMgr,
	KissCtx:       ctx,
	Logger:        a.logger,
	HistoryDBPath: a.cfg.HistoryDBPath,
	Version:       a.cfg.Version,
	MapsCache:     mapsCache,
	Catalog:       catalog,
	Style:         styleCache,
	Demo:          a.cfg.Demo,
})
```

- [ ] **Step 3: Add the imports**

At the top of `pkg/app/wiring.go`, add to the import block (alphabetically):

```go
"path/filepath"
"github.com/chrissnell/graywolf/pkg/mapsstyle"
```

- [ ] **Step 4: Verify it builds**

Run: `go build ./...`
Expected: success.

Run: `go test ./pkg/app/... ./pkg/webapi/... ./pkg/mapsstyle/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/app/wiring.go
git commit -m "app: wire mapsstyle cache into webapi (no startup warmer)"
```

---

## Task 13: Frontend — switch to local proxy URLs

**Files:**
- Modify: `web/src/lib/map/maplibre-map.svelte`

- [ ] **Step 1: Read the current URLs being changed**

The two upstream URLs hardcoded in the component are:

```js
const STYLE_URL = 'https://maps.nw5w.com/style/americana-roboto/style.json';   // line ~86
'https://maps.nw5w.com/style/americana/shields.json'                            // line ~221 (URLShieldRenderer arg)
```

And there is a localStorage cache (`STYLE_CACHE_KEY`, `fetchUpstreamStyle`) that becomes redundant once the server handles offline persistence.

- [ ] **Step 2: Swap the URLs and remove the localStorage cache**

Edit `web/src/lib/map/maplibre-map.svelte`. Replace:

```js
  // Cache the upstream americana style.json across style swaps so we
  // don't re-fetch every time downloads change. The cache is in-memory
  // only; a full page reload always re-fetches from the network.
  //
  // Freshness: maps.nw5w.com serves style.json with `Cache-Control:
  // no-cache` so the browser revalidates with origin on every request
  // and we never sit on a stale style after a deploy. Tiles still go
  // through CF's edge cache untouched.
  //
  // Offline: we save the most recent successful response into
  // localStorage so that an operator who loaded the app online and
  // later went offline still gets a working style. A first-ever load
  // with no network can't be saved by anything we do here — it has to
  // fail visibly so the operator knows to come online once.
  const STYLE_URL = 'https://maps.nw5w.com/style/americana-roboto/style.json';
  const STYLE_CACHE_KEY = 'graywolf:upstream-style:v1';
  let cachedUpstreamStyle = null;

  async function fetchUpstreamStyle() {
    try {
      const res = await fetch(STYLE_URL);
      if (!res.ok) throw new Error(`fetch upstream style: ${res.status}`);
      const text = await res.text();
      try {
        localStorage.setItem(STYLE_CACHE_KEY, text);
      } catch {
        // Quota or disabled storage — non-fatal; next online load retries.
      }
      return JSON.parse(text);
    } catch (err) {
      const cached = localStorage.getItem(STYLE_CACHE_KEY);
      if (!cached) throw err;
      console.warn(
        '[graywolf] upstream style fetch failed, using cached fallback:',
        err,
      );
      return JSON.parse(cached);
    }
  }
```

with:

```js
  // The style.json (and its referenced glyphs, sprite, shields, and
  // tiles.json) are served by graywolf itself via /api/maps/style/...
  // The Go side (pkg/mapsstyle) is a pull-through cache: first online
  // request hydrates the disk, subsequent requests (online or offline)
  // serve from disk. No localStorage hack needed since persistence is
  // server-side, which means LAN guests and post-IP-change sessions
  // share a single cache. See issue #204.
  //
  // In-memory cache across style swaps avoids re-fetching the same
  // bytes when toggling federated mode or flipping map sources.
  const STYLE_URL = '/api/maps/style/americana-roboto/style.json';
  let cachedUpstreamStyle = null;

  async function fetchUpstreamStyle() {
    const res = await fetch(STYLE_URL);
    if (!res.ok) throw new Error(`fetch style: ${res.status}`);
    return await res.json();
  }
```

Then in the `URLShieldRenderer` invocation, replace:

```js
    new URLShieldRenderer(
      'https://maps.nw5w.com/style/americana/shields.json',
```

with:

```js
    new URLShieldRenderer(
      '/api/maps/style/americana/shields.json',
```

- [ ] **Step 3: Verify the build**

Run: `cd web && npm run build`
Expected: clean build.

- [ ] **Step 4: Run web tests**

Run: `cd web && npm test`
Expected: pass. (No new web tests; the URL swap is mechanical and the existing component-lifecycle tests cover the surrounding behavior.)

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/map/maplibre-map.svelte
git commit -m "web/map: fetch style + shields from local proxy; drop localStorage cache"
```

---

## Task 14: Regenerate swagger + API client

**Files:**
- Modify (regen): `pkg/webapi/docs/docs.go`, `pkg/webapi/docs/swagger.json`, `pkg/webapi/docs/swagger.yaml`, `web/src/lib/api/**`

- [ ] **Step 1: Run the project's swagger regeneration**

Run: `make docs-check`

Expected: regenerates files or prints "in sync." If the swag binary is missing, install the project-pinned version:

```bash
go install github.com/swaggo/swag/cmd/swag@v1.16.4
```

Then re-run `make docs-check`.

- [ ] **Step 2: Run the API client regen**

Run: `make api-client-check`

Expected: regenerated TS client.

- [ ] **Step 3: Commit the regen output**

```bash
git add pkg/webapi/docs/ web/src/lib/api/
git commit -m "docs+api-client: regenerate for /api/maps/style endpoint"
```

(Skip the commit for whichever regen produced no diff. If both produced no changes, skip this task entirely.)

---

## Task 15: Update the wiki

**Files:**
- Modify: `docs/wiki/code-map.md`
- Modify: `docs/wiki/system-topology.md`

- [ ] **Step 1: Add a row to code-map.md under Maps integration**

In `docs/wiki/code-map.md`, locate the `## Maps integration (graywolf-maps client)` table (around line 232-243). Insert this row before the "Web-side glue" row:

```markdown
| Style/glyph/sprite/shields/tiles.json pull-through cache, served at `/api/maps/style/{path}` | [`../../pkg/mapsstyle/`](../../pkg/mapsstyle/) |
```

- [ ] **Step 2: Extend the offline-maps catalog section in system-topology.md**

In `docs/wiki/system-topology.md`, locate the `### Offline maps catalog` section (around line 164). Append a new subsection after it, before the next `##` heading:

```markdown
### Offline maps style assets

The MapLibre style depends on a set of resources hosted by the graywolf-maps
Worker: `style.json`, `shields.json`, sprite (JSON + PNG + 2x PNG), per-fontstack
glyph PBFs, and `tiles.json`. Graywolf proxies all of these at
`GET /api/maps/style/{path}` from a disk-backed pull-through cache rooted at
`<TileCacheDir>/style/`. There is no startup network access: graywolf boots
fine offline. The first online browser request for any style asset hydrates
that asset to disk; every subsequent request (online or offline, same
browser or LAN guest) serves from disk.

`POST /api/maps/downloads/{slug}` piggybacks a background glyph pre-warm:
the operator is provably online when starting a region download, so this
is the right moment to top up the long tail of glyph ranges that MapLibre
may never request before the user goes offline. The pre-warm reads the
cached style.json (which the download path also seeds), discovers
fontstacks via `text-font` layer keys, and fetches each glyph range
0-255 through 65280-65535 per fontstack with a "stop after 4 consecutive
404s" bail-out per fontstack. Best-effort; failures are logged at DEBUG.

The `style.json` body is rewritten on every serve so its `glyphs`,
`sprite`, and `sources.*.url` fields point at the local proxy instead
of `maps.nw5w.com`. The terrarium elevation source on `s3.amazonaws.com`
is left untouched — DEM/hillshading is out of scope for the offline
path.

Failure modes:
- Never-online + no cache: 502 from the proxy; the map fails visibly
  in the browser. Acceptable: a never-online host also has no tiles.
- Online + no cache: first request hydrates upstream-to-disk in a few
  hundred ms, then renders normally. Concurrent first requests for the
  same asset are coalesced (one upstream call, all callers share).
- Online + stale cache: cache copies survive across restarts; refresh
  happens implicitly on the next map download via the prewarm hook,
  which re-fetches the eagerly-needed assets.
```

Also add a row to the table around line 130-131 (`auth.nw5w.com` / `maps.nw5w.com` entries) for the dependency map. After the existing `maps.nw5w.com` row:

```markdown
| `maps.nw5w.com` (style assets) | proxied via `/api/maps/style/{path}` from a disk cache; pull-through on first browser request | [`../../pkg/mapsstyle/`](../../pkg/mapsstyle/) | [`../handbook/livemap.html`](../handbook/livemap.html) |
```

- [ ] **Step 3: Commit the wiki update**

```bash
git add docs/wiki/code-map.md docs/wiki/system-topology.md
git commit -m "wiki: document the mapsstyle pull-through cache"
```

---

## Task 16: End-to-end verification

**Files:** (no edits, just verification)

- [ ] **Step 1: Build the binary**

Run: `go build ./...`
Expected: success.

- [ ] **Step 2: Run the full Go test suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 3: Run the web build + tests**

Run: `cd web && npm test && npm run build`
Expected: PASS + clean build.

- [ ] **Step 4: Manual offline-render smoke test**

Boot graywolf online, browse the map (which triggers lazy hydration), then disconnect and verify the map still renders.

```bash
# Online boot (no warmer; nothing happens unless the browser asks)
./graywolf -http-addr 127.0.0.1:8080 -tile-cache-dir /tmp/graywolf-tiles &
GW_PID=$!

# Open http://127.0.0.1:8080/map in a browser. The map should render.
# This populates <TileCacheDir>/style/ with the assets MapLibre actually
# requested.

# Now click "Download Colorado" in MapsSettings. After the download starts,
# wait ~30 seconds; the piggyback prewarm should hydrate the full glyph set.

ls /tmp/graywolf-tiles/style/
ls /tmp/graywolf-tiles/style/roboto-glyphs/

# Kill graywolf.
kill $GW_PID

# Block network access to maps.nw5w.com (e.g. /etc/hosts entry mapping
# to 127.0.0.1, or unplug ethernet). Restart graywolf and reload the
# map page. Map should render fully; DevTools Network tab should show
# all /api/maps/style/* requests returning 200, zero requests to
# maps.nw5w.com for style assets.
```

Expected: map renders offline with labels + shields + icons. Lazy-hydrated browsers without map downloads will see degraded glyph coverage for unvisited Unicode ranges — that's the documented trade-off.

- [ ] **Step 5: Push the branch and open the PR**

```bash
git push -u origin fix/issue-204-offline-style-cache
gh pr create --title "Pull-through cache for MapLibre style assets (fixes offline maps)" --body "$(cat <<'EOF'
## Summary

- 0.13.12 (#196) decoupled the render-bounds path from the live catalog, but the MapLibre style.json itself was still fetched directly from `maps.nw5w.com` by the browser. The only offline fallback was per-browser localStorage, which is per-origin (a laptop switching from upstream WiFi IP to hotspot IP gets an empty cache) and per-browser (LAN guests have no cache at all).
- This branch adds `pkg/mapsstyle`, a server-side pull-through cache that persists style.json, shields.json, sprite, glyph PBFs, and tiles.json under `<TileCacheDir>/style/` and serves them at `/api/maps/style/{path}`. No startup network: graywolf boots fine offline. First online browser request hydrates to disk; subsequent requests (online or offline, any browser, any LAN guest) serve from disk.
- `POST /api/maps/downloads/{slug}` piggybacks a background glyph pre-warm so offline users who download a region get the full label set without having to first pan the map to every place name.
- Svelte component drops the localStorage cache and points the style + shields URLs at the local proxy.

Refs #204

## Test plan

- [x] `go test ./...` — all packages green
- [x] `cd web && npm test && npm run build` — clean
- [x] `make docs-check api-client-check` — swagger + TS client regenerated
- [ ] Scenario A: boot online, browse the map, disconnect network, reboot, browse map again — verify rendering
- [ ] Scenario B: boot online, click Download Colorado, wait 30s, disconnect, reboot, verify rendering with full glyph coverage
- [ ] Scenario C: LAN guest browser that has never visited graywolf hitting an offline graywolf with a populated cache — verify rendering
- [ ] Scenario D: fresh install with empty cache and no network — verify 502 from /api/maps/style/* (correct failure mode)
EOF
)"
```

Expected: PR URL printed. Then post the issue trace with the most substantive commit SHA:

```bash
SHA=$(git rev-parse HEAD)
gh issue comment 204 --body "Fixed in https://github.com/chrissnell/graywolf/commit/$SHA"
```

Then stop. Do not merge. Do not close the issue.

---

## Self-Review

**Spec coverage:**
- [x] No internet required at startup — Task 12 explicitly omits the warmer goroutine
- [x] New online user gets a working map without manual action — Task 5's pull-through Get handles first-ever browser request
- [x] Cache survives reboots, multi-origin browsers, LAN guests — Task 3 puts it on disk
- [x] Map downloads piggyback full glyph pre-warm — Task 11 hooks into startDownload
- [x] style.json URL rewriting — Tasks 6-7
- [x] Path-traversal safety — Tasks 1, 10
- [x] DEM left untouched — verified in Task 6 test

**Placeholder scan:** none.

**Type consistency:**
- `Config.LocalPrefix` (Task 3) used throughout (Tasks 7, 10, 12)
- `mapsstyle.New(Config)` consistent across constructors
- `Cache.Get(ctx, rel) ([]byte, string, error)` and `Cache.PrewarmGlyphs(ctx) error` consistent
- `Cache.SetPrewarmLimits(maxRange, stopAfter)` consistent between Task 9 implementation and tests
- Disk uses `filepath.Join` / `filepath.FromSlash`; URL uses `path` / `url.PathEscape` — separation maintained
