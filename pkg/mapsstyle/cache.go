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
	// Set both Path (unescaped) and RawPath (escaped) so url.String()
	// emits the pre-escaped form instead of double-escaping %20 -> %2520.
	u.Path = "/" + path.Join(append([]string{"style"}, segs...)...)
	u.RawPath = "/" + strings.Join(escaped, "/")
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
		return c.maybeRewrite(cleaned, body, ct)
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
		body, ct, err := c.readDisk(cleaned)
		if err != nil {
			return nil, "", err
		}
		return c.maybeRewrite(cleaned, body, ct)
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
	return c.maybeRewrite(cleaned, body, ct)
}

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
