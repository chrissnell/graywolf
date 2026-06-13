// Package mapscatalog fetches the public download catalog from the
// graywolf-maps Worker (GET <base>/manifest.json) and caches it
// in-process with a TTL. Stale-on-error: a refresh that fails after
// the catalog is warm continues serving the previous copy with a
// warning. A cold failure (no cached copy) returns the error.
package mapscatalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Country struct {
	ISO2      string      `json:"iso2"`
	Name      string      `json:"name"`
	SizeBytes int64       `json:"sizeBytes"`
	SHA256    string      `json:"sha256"`
	BBox      *[4]float64 `json:"bbox,omitempty"`
}

type Province struct {
	ISO2      string      `json:"iso2"`
	Slug      string      `json:"slug"`
	Name      string      `json:"name"`
	Code      string      `json:"code,omitempty"`
	SizeBytes int64       `json:"sizeBytes"`
	SHA256    string      `json:"sha256"`
	BBox      *[4]float64 `json:"bbox,omitempty"`
}

type State struct {
	Slug      string      `json:"slug"`
	Name      string      `json:"name"`
	Code      string      `json:"code,omitempty"`
	SizeBytes int64       `json:"sizeBytes"`
	SHA256    string      `json:"sha256"`
	BBox      *[4]float64 `json:"bbox,omitempty"`
}

// WorldMap is the single global low-zoom basemap archive. Unlike the
// regional entries it carries a MaxZoom: the archive is capped (e.g.
// z7), so the offline render path overzooms its top tile rather than
// requesting zooms the archive does not contain.
type WorldMap struct {
	Name      string      `json:"name"`
	SizeBytes int64       `json:"sizeBytes"`
	SHA256    string      `json:"sha256"`
	BBox      *[4]float64 `json:"bbox,omitempty"`
	MaxZoom   int         `json:"maxZoom"`
}

type Catalog struct {
	SchemaVersion int        `json:"schemaVersion"`
	GeneratedAt   string     `json:"generatedAt"`
	Countries     []Country  `json:"countries"`
	Provinces     []Province `json:"provinces"`
	States        []State    `json:"states"`
	World         *WorldMap  `json:"world,omitempty"`

	// slugIndex is a lazily-built O(1) membership lookup populated by
	// indexSlugs. Not serialized; rebuilt on every fresh fetch.
	slugIndex map[string]struct{} `json:"-"`
}

// HasSlug reports whether slug names a published archive in this
// catalog. Slugs are namespaced ("state/colorado", "country/de",
// "province/ca/british-columbia"). O(1) when the index is populated
// (every catalog returned by Cache.Get is); falls back to a linear
// scan for hand-constructed Catalog values where indexSlugs has not
// run.
func (c *Catalog) HasSlug(slug string) bool {
	if c.slugIndex != nil {
		_, ok := c.slugIndex[slug]
		return ok
	}
	// Fallback for callers that hand-construct a Catalog (tests,
	// fakes). Production paths build the index in Cache.fetch.
	for _, s := range c.States {
		if "state/"+s.Slug == slug {
			return true
		}
	}
	for _, x := range c.Countries {
		if "country/"+x.ISO2 == slug {
			return true
		}
	}
	for _, p := range c.Provinces {
		if "province/"+p.ISO2+"/"+p.Slug == slug {
			return true
		}
	}
	if c.World != nil && slug == "world" {
		return true
	}
	return false
}

// indexSlugs (re)builds the slugIndex from the entry slices. Called
// after every successful fetch.
func (c *Catalog) indexSlugs() {
	idx := make(map[string]struct{}, len(c.Countries)+len(c.Provinces)+len(c.States))
	for _, s := range c.States {
		idx["state/"+s.Slug] = struct{}{}
	}
	for _, x := range c.Countries {
		idx["country/"+x.ISO2] = struct{}{}
	}
	for _, p := range c.Provinces {
		idx["province/"+p.ISO2+"/"+p.Slug] = struct{}{}
	}
	if c.World != nil {
		idx["world"] = struct{}{}
	}
	c.slugIndex = idx
}

// Cache fetches and caches the worker catalog.
type Cache struct {
	baseURL       string
	tokenProvider func(context.Context) string
	ttl           time.Duration
	httpClient    *http.Client

	// diskCacheDir is the directory holding the on-disk catalog.json
	// stale fallback. Empty string disables disk persistence (existing
	// callers that haven't migrated to NewWithDiskCache).
	diskCacheDir string

	mu        sync.Mutex
	cached    *Catalog
	fetchedAt time.Time

	// inflight is set while a Get-driven fetch is in progress so
	// concurrent first-callers wait on a single upstream call instead
	// of each issuing their own. Closed when the fetch completes.
	inflight chan struct{}
}

// New constructs a Cache. baseURL is the maps host root (e.g.
// https://maps.nw5w.com). tokenProvider returns the current bearer
// token (may be empty for public testing). ttl is the cache lifetime;
// 0 means always refresh. Equivalent to NewWithDiskCache with no
// disk cache directory.
func New(baseURL string, tokenProvider func(context.Context) string, ttl time.Duration) *Cache {
	return NewWithDiskCache(baseURL, tokenProvider, ttl, "")
}

// NewWithDiskCache constructs a Cache that also writes every
// successful manifest fetch to <diskCacheDir>/catalog.json and seeds
// the in-memory cache from that file on construction. The seeded
// entry is treated as already-stale (fetchedAt = mtime), so the next
// Get triggers a refresh; if the refresh fails the on-disk copy is
// served via the existing stale-on-error path. This lets the region
// picker show the last known catalog on an offline host instead of
// erroring.
//
// The render path does NOT depend on this disk cache --
// /api/maps/local-bounds is the offline-safe render-bounds source.
func NewWithDiskCache(baseURL string, tokenProvider func(context.Context) string, ttl time.Duration, diskCacheDir string) *Cache {
	c := &Cache{
		baseURL:       baseURL,
		tokenProvider: tokenProvider,
		ttl:           ttl,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
		diskCacheDir:  diskCacheDir,
	}
	c.loadFromDisk()
	return c
}

// loadFromDisk seeds c.cached from <diskCacheDir>/catalog.json when
// present. Failures are silent — disk persistence is best-effort, and
// a missing or corrupt file falls back to fetching from the network.
//
// fetchedAt is left at the zero value (not the file mtime) so the
// disk-seeded entry is always considered expired: the first Get()
// triggers a network refresh, and only if that refresh fails does
// the stale-on-error branch fall back to the on-disk copy. Using the
// file mtime would let a reboot inside the TTL window serve disk
// content as "fresh" and skip the warm-up entirely — defeating the
// point of disk persistence (a lifeline for offline operation, not a
// shortcut around the TTL).
func (c *Cache) loadFromDisk() {
	if c.diskCacheDir == "" {
		return
	}
	path := filepath.Join(c.diskCacheDir, "catalog.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var out Catalog
	if err := json.Unmarshal(b, &out); err != nil {
		return
	}
	if out.SchemaVersion != 1 {
		return
	}
	out.indexSlugs()
	c.cached = &out
	// fetchedAt stays at time.Time{} — see comment above.
}

// saveToDisk writes c.cached atomically to <diskCacheDir>/catalog.json.
// Best-effort — failures (no diskCacheDir, transient I/O error) are
// logged at WARN and do not affect the in-memory cache or the caller.
func (c *Cache) saveToDisk(cat Catalog) {
	if c.diskCacheDir == "" {
		return
	}
	if err := os.MkdirAll(c.diskCacheDir, 0o755); err != nil {
		slog.Warn("mapscatalog: mkdir disk cache failed", "err", err)
		return
	}
	path := filepath.Join(c.diskCacheDir, "catalog.json")
	tmp, err := os.CreateTemp(c.diskCacheDir, "catalog.json.*.tmp")
	if err != nil {
		slog.Warn("mapscatalog: tempfile failed", "err", err)
		return
	}
	tmpName := tmp.Name()
	enc := json.NewEncoder(tmp)
	if err := enc.Encode(cat); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		slog.Warn("mapscatalog: encode disk cache failed", "err", err)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		slog.Warn("mapscatalog: close disk cache failed", "err", err)
		return
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		slog.Warn("mapscatalog: rename disk cache failed", "err", err)
	}
}

// Get returns the current catalog. Fresh fetch on miss or expired TTL.
// Stale-on-error after the first successful fetch. Concurrent callers
// who arrive while a fetch is in flight share the result rather than
// issuing duplicate upstream calls.
func (c *Cache) Get(ctx context.Context) (Catalog, error) {
	for {
		c.mu.Lock()
		// Fast path: fresh cached copy.
		if c.cached != nil && c.ttl > 0 && time.Since(c.fetchedAt) < c.ttl {
			out := *c.cached
			c.mu.Unlock()
			return out, nil
		}
		// Another goroutine is already fetching; wait for it.
		if ch := c.inflight; ch != nil {
			c.mu.Unlock()
			select {
			case <-ch:
			case <-ctx.Done():
				return Catalog{}, ctx.Err()
			}
			// Loop: re-check the cache. If the leader succeeded the
			// fast-path returns; if it failed and we have stale data,
			// the stale-on-error branch below returns it; otherwise we
			// take a turn as the leader on the next iteration.
			continue
		}
		// We're the leader: install our channel and fetch.
		ch := make(chan struct{})
		c.inflight = ch
		c.mu.Unlock()

		fresh, err := c.fetch(ctx)

		c.mu.Lock()
		if err == nil {
			c.cached = &fresh
			c.fetchedAt = time.Now()
		}
		c.inflight = nil
		stale := c.cached
		c.mu.Unlock()
		close(ch)

		if err != nil {
			if stale != nil && err != ctx.Err() {
				slog.Warn("mapscatalog refresh failed, serving stale", "err", err)
				return *stale, nil
			}
			return Catalog{}, err
		}
		return fresh, nil
	}
}

// Refresh forces a fetch and updates the cache. Returns the new
// catalog and any error from the fetch (cache is unchanged on error).
func (c *Cache) Refresh(ctx context.Context) (Catalog, error) {
	fresh, err := c.fetch(ctx)
	if err != nil {
		return Catalog{}, err
	}
	c.mu.Lock()
	c.cached = &fresh
	c.fetchedAt = time.Now()
	c.mu.Unlock()
	return fresh, nil
}

func (c *Cache) fetch(ctx context.Context) (Catalog, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return Catalog{}, fmt.Errorf("parse baseURL: %w", err)
	}
	u.Path = "/manifest.json"
	hadToken := false
	if tok := c.tokenProvider(ctx); tok != "" {
		hadToken = true
		q := u.Query()
		q.Set("t", tok)
		u.RawQuery = q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Catalog{}, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Catalog{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Catalog{}, fetchError(resp.StatusCode, hadToken, string(body))
	}
	var out Catalog
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Catalog{}, fmt.Errorf("decode manifest: %w", err)
	}
	if out.SchemaVersion != 1 {
		return Catalog{}, errors.New("unsupported manifest schemaVersion")
	}
	out.indexSlugs()
	c.saveToDisk(out)
	return out, nil
}

// fetchError turns a non-200 manifest response into an operator-facing
// error. Auth failures (401/403) are the common real-world case --
// they mean the maps service rejected our credentials -- so we say so
// in plain language and point at the fix (register the device under the
// Settings tab) instead of surfacing the raw upstream body like
// "unauthorized: missing", which tells an operator nothing actionable.
func fetchError(status int, hadToken bool, body string) error {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		if !hadToken {
			return fmt.Errorf("To activate Graywolf Maps, go to the Settings tab and register your device (HTTP %d)", status)
		}
		return fmt.Errorf("Graywolf Maps access was rejected (the token may be expired or revoked) -- go to the Settings tab and re-register your device (HTTP %d)", status)
	default:
		body = strings.TrimSpace(body)
		if body == "" {
			return fmt.Errorf("maps catalog request failed: HTTP %d", status)
		}
		return fmt.Errorf("maps catalog request failed: HTTP %d: %s", status, body)
	}
}
