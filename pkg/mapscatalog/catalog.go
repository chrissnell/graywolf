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

type Catalog struct {
	SchemaVersion int        `json:"schemaVersion"`
	GeneratedAt   string     `json:"generatedAt"`
	Countries     []Country  `json:"countries"`
	Provinces     []Province `json:"provinces"`
	States        []State    `json:"states"`
}

// Cache fetches and caches the worker catalog.
type Cache struct {
	baseURL       string
	tokenProvider func(context.Context) string
	ttl           time.Duration
	httpClient    *http.Client

	mu        sync.Mutex
	cached    *Catalog
	fetchedAt time.Time
}

// New constructs a Cache. baseURL is the maps host root (e.g.
// https://maps.nw5w.com). tokenProvider returns the current bearer
// token (may be empty for public testing). ttl is the cache lifetime;
// 0 means always refresh.
func New(baseURL string, tokenProvider func(context.Context) string, ttl time.Duration) *Cache {
	return &Cache{
		baseURL:       baseURL,
		tokenProvider: tokenProvider,
		ttl:           ttl,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Get returns the current catalog. Fresh fetch on miss or expired TTL.
// Stale-on-error after the first successful fetch.
func (c *Cache) Get(ctx context.Context) (Catalog, error) {
	c.mu.Lock()
	if c.cached != nil && c.ttl > 0 && time.Since(c.fetchedAt) < c.ttl {
		out := *c.cached
		c.mu.Unlock()
		return out, nil
	}
	c.mu.Unlock()

	fresh, err := c.fetch(ctx)
	if err != nil {
		c.mu.Lock()
		stale := c.cached
		c.mu.Unlock()
		if stale != nil {
			slog.Warn("mapscatalog refresh failed, serving stale", "err", err)
			return *stale, nil
		}
		return Catalog{}, err
	}
	c.mu.Lock()
	c.cached = &fresh
	c.fetchedAt = time.Now()
	c.mu.Unlock()
	return fresh, nil
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
	if tok := c.tokenProvider(ctx); tok != "" {
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
		return Catalog{}, fmt.Errorf("manifest HTTP %d: %s", resp.StatusCode, string(body))
	}
	var out Catalog
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Catalog{}, fmt.Errorf("decode manifest: %w", err)
	}
	if out.SchemaVersion != 1 {
		return Catalog{}, errors.New("unsupported manifest schemaVersion")
	}
	return out, nil
}
