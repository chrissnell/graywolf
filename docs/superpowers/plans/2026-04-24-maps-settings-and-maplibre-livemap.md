# Maps Settings & MapLibre Live Map Implementation Plan (Plan 1 of 2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Maps" tab in Settings that lets the operator pick the basemap source and register a per-device token against `auth.nw5w.com`, then rebuild the Live Map on MapLibre GL with full feature parity to the existing Leaflet implementation. Plan 2 layers offline state downloads on top.

**Architecture:**
- Backend: a new SQLite singleton (`MapsConfig`) holds the source choice (`osm` / `graywolf`), the registered callsign + token, and timestamps. New endpoints under `/api/preferences/maps` (GET/PUT for source, POST `/register` to proxy `auth.nw5w.com`). A new CLI flag `--tile-cache-dir` plumbs the offline cache path so Plan 2 can land without re-touching `cmd/`.
- Frontend: a dedicated `/preferences/maps` route (not folded into `Preferences.svelte` because the registration + future state-picker UX is substantial). The Live Map is rewritten as a new component on MapLibre GL JS 4.x, with the existing data-layer (ETag polling, since-cursor delta, station model) extracted into a renderer-agnostic store. Stations / trails / weather render as native MapLibre sources & layers; the "my-position" marker stays a DOM marker. PMTiles protocol is registered up front but unused until Plan 2.
- Mobile-first: every new surface is designed phone-first (≤480px), then promoted to tablet (≤768px) and desktop. The map's overlay panels (layer toggles, time range, status, future info panels) collapse to bottom sheets on mobile. Existing 768px breakpoint and `--bg-*` / `--text-*` theme tokens are reused throughout.

**Tech Stack:**
- Frontend: Svelte 5 runes, `svelte-spa-router`, `@chrissnell/chonky-ui` components (`Box`, `Toggle`, `Drawer`, `Select`), MapLibre GL JS `^4.7.1`, pmtiles `^3.2.0`, `@americana/maplibre-shield-generator` (for highway shields under the americana style)
- Backend: Go 1.22+ `net/http` ServeMux with method-pattern routing, GORM + glebarez/sqlite, `httptest` for handler tests
- Auth flow: `POST https://auth.nw5w.com/register` with `{"callsign":"..."}` returns `{callsign, token}`; tile/tilejson requests carry `Authorization: Bearer <token>`; `/style/*` MUST stay anonymous (Cloudflare edge cache)

---

## Mobile-First Design Principles

These apply to **every** UI task in this plan. If a task's CSS doesn't honor them, the task isn't done.

1. **Phone is the design baseline.** Build the layout at 375×667 (iPhone SE) first. Tablet and desktop are progressive enhancements via `@media (min-width: 769px)`.
2. **Hit targets ≥44×44 px.** Every button, toggle, radio, link in the maps UI. Pad them, don't shrink the font.
3. **One-handed reach.** Primary actions in the bottom 60% of the viewport on mobile (e.g., Register button, source picker). Secondary actions can sit at the top.
4. **Bottom sheets, not popovers.** On mobile, the layer panel and time-range selector slide up from the bottom (use `Drawer` with `anchor="bottom"`). On desktop they're floating cards docked top-right.
5. **No tiny tap zones over the map.** Map overlay controls collapse behind a single FAB on mobile; expanded into a sheet on tap.
6. **Safe areas.** Honor `env(safe-area-inset-*)` for the top app bar and any bottom-anchored sheet.
7. **Theme tokens only.** Use existing CSS custom properties (`--bg-secondary`, `--text-primary`, `--accent`, `--border-color`, `--font-mono`, etc.). Add new tokens to every theme file when you genuinely need a new shade.
8. **No horizontal scroll.** Test by setting Chrome DevTools to 320px wide; nothing should overflow.
9. **Loading states are first-class.** Registration spinners, map-loading shimmers, source-switching feedback. Never let the user wonder if a tap was registered.
10. **Errors lead the user out.** Every failure surfaces the verbatim server `message` (which points at https://github.com/chrissnell/graywolf/issues) plus a "Copy details" button.

---

## File Structure

### Backend (Go)

**Create:**
- `graywolf/pkg/configstore/seed_maps.go` — Get/Upsert for `MapsConfig` singleton, validation
- `graywolf/pkg/configstore/seed_maps_test.go`
- `graywolf/pkg/webapi/maps.go` — `/api/preferences/maps` GET/PUT and `/api/preferences/maps/register` POST handler
- `graywolf/pkg/webapi/maps_test.go`
- `graywolf/pkg/webapi/dto/maps.go` — request/response DTOs
- `graywolf/pkg/mapsauth/client.go` — thin client for `https://auth.nw5w.com/register` with timeout, error mapping, and a configurable `BaseURL` for tests
- `graywolf/pkg/mapsauth/client_test.go`

**Modify:**
- `graywolf/pkg/configstore/models.go` — add `MapsConfig` struct
- `graywolf/pkg/configstore/store.go:123-147` — append `&MapsConfig{}` to `AutoMigrate` list
- `graywolf/pkg/webapi/server.go:175-197` — call `s.registerMaps(mux)` from `RegisterRoutes`
- `graywolf/pkg/webapi/server.go` (constructor) — accept and stash a `mapsauth.Client`
- `graywolf/pkg/app/wiring.go` — construct `mapsauth.Client` and pass into webapi server; surface `--tile-cache-dir` flag and ensure the directory exists on startup
- `graywolf/cmd/graywolf/main.go` (or wherever flags are registered — see Phase 1 Task 6 for discovery) — register the `--tile-cache-dir` flag

### Frontend (Svelte)

**Create:**
- `graywolf/web/src/lib/settings/maps-store.svelte.js` — runes-backed store mirroring server `MapsConfig`, with `register(callsign)` action that POSTs and persists
- `graywolf/web/src/lib/map/maplibre-map.svelte` — base map shell (style, transformRequest, gestures, attribution)
- `graywolf/web/src/lib/map/sources/osm-raster.js` — OSM raster source factory
- `graywolf/web/src/lib/map/sources/graywolf-vector.js` — GW vector source factory (private, `transformRequest` adds bearer)
- `graywolf/web/src/lib/map/layers/stations.js` — GeoJSON source + symbol layer for APRS stations (renderer-agnostic data structure shared with the data-layer store)
- `graywolf/web/src/lib/map/layers/trails.js` — GeoJSON line source + line layer
- `graywolf/web/src/lib/map/layers/weather.js` — weather overlay layer
- `graywolf/web/src/lib/map/layers/hover-path.js` — temporary digi-path line source (cleared on mouseleave)
- `graywolf/web/src/lib/map/layers/my-position.js` — DOM marker (rich HTML with tooltip)
- `graywolf/web/src/lib/map/aprs-icon-spritesheet.js` — runtime canvas-based sprite generation for APRS icons (so symbol layer can reference them by id)
- `graywolf/web/src/lib/map/data-store.svelte.js` — extracted polling/ETag/since-cursor logic from current `LiveMap.svelte`, returns reactive station/trail/weather collections
- `graywolf/web/src/lib/map/info-panel.svelte` — collapsible bottom-sheet / docked-card scaffold for future panels (header, body slot, close button)
- `graywolf/web/src/routes/MapsSettings.svelte` — full Maps Settings page
- `graywolf/web/src/routes/LiveMapV2.svelte` — new MapLibre-backed Live Map (replaces `LiveMap.svelte` at cutover)
- `graywolf/web/src/lib/maps/callsign.js` — pure-function callsign normalizer (uppercase, strip `-SSID`, regex)
- `graywolf/web/src/lib/maps/registration.js` — `register({callsign})` wrapper that calls the backend and maps error codes to user-facing copy
- `graywolf/web/src/lib/maps/state-list.js` — hardcoded 50-states + DC list (used disabled in Plan 1, active in Plan 2)
- `graywolf/web/src/lib/maps/styles.css` — shared Maps-tab + map-overlay styles using theme tokens

**Modify:**
- `graywolf/web/package.json` — add `maplibre-gl`, `pmtiles`, `@americana/maplibre-shield-generator`
- `graywolf/web/src/App.svelte:32-53` — add `/preferences/maps` route → `MapsSettings`; replace `/map` route to point at `LiveMapV2`
- `graywolf/web/src/components/Sidebar.svelte:34-43` — add `{ path: '/preferences/maps', label: 'Maps' }` to the Settings nav group, after Preferences
- `graywolf/web/themes/*.css` — add map overlay tokens (`--map-overlay-bg`, `--map-overlay-fg`, `--map-overlay-shadow`) to each theme

**Delete (at cutover, Phase 5):**
- `graywolf/web/src/routes/LiveMap.svelte` — replaced by `LiveMapV2.svelte`
- `graywolf/web/src/lib/map/station-layer.js`, `trail-layer.js`, `weather-layer.js` — replaced by MapLibre layer modules
- `graywolf/web/src/lib/map/aprs-icons.js` — replaced by `aprs-icon-spritesheet.js`
- `leaflet` from `package.json` (and `leaflet/dist/leaflet.css` import)

---

## Phase 1: Backend Foundation

### Task 1: Add `MapsConfig` model

**Files:**
- Modify: `graywolf/pkg/configstore/models.go`

- [ ] **Step 1: Append the `MapsConfig` struct to models.go**

After the `ThemeConfig` block (around line 357), add:

```go
// MapsConfig is the singleton row that captures the operator's basemap
// source choice plus the device-local registration with auth.nw5w.com.
// Source is one of "osm" (public OSM raster tiles) or "graywolf"
// (private maps.nw5w.com vector tiles, requires Token). An empty Token
// means the user hasn't registered this device yet, in which case the
// frontend forces Source to "osm" until they do.
type MapsConfig struct {
	ID           uint32    `gorm:"primaryKey;autoIncrement" json:"id"`
	Source       string    `gorm:"not null;default:'osm'" json:"source"`
	Callsign     string    `gorm:"not null;default:''" json:"callsign"`
	Token        string    `gorm:"not null;default:''" json:"-"`
	RegisteredAt time.Time `json:"registered_at,omitempty"`
	CreatedAt    time.Time `json:"-"`
	UpdatedAt    time.Time `json:"-"`
}
```

`json:"-"` on `Token` keeps it out of the API response by default; the frontend reveals it only via an explicit "Show token" action that hits a separate `?include_token=1` path (Task 4).

- [ ] **Step 2: Append `&MapsConfig{}` to AutoMigrate**

In `graywolf/pkg/configstore/store.go:123-147`, add `&MapsConfig{},` after `&ThemeConfig{},`:

```go
&UnitsConfig{},
&ThemeConfig{},
&MapsConfig{},
```

- [ ] **Step 3: Run `go build ./...` to confirm the model compiles**

Run: `cd graywolf && go build ./...`
Expected: clean build, no errors.

- [ ] **Step 4: Commit**

```bash
git add graywolf/pkg/configstore/models.go graywolf/pkg/configstore/store.go
git commit -m "configstore: add MapsConfig singleton model"
```

---

### Task 2: Add `Get/UpsertMapsConfig` with tests

**Files:**
- Create: `graywolf/pkg/configstore/seed_maps.go`
- Create: `graywolf/pkg/configstore/seed_maps_test.go`

- [ ] **Step 1: Write the failing tests first**

Create `graywolf/pkg/configstore/seed_maps_test.go`:

```go
package configstore

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestGetMapsConfig_DefaultsWhenEmpty(t *testing.T) {
	s := newTestStore(t)
	c, err := s.GetMapsConfig(context.Background())
	if err != nil {
		t.Fatalf("GetMapsConfig: %v", err)
	}
	if c.Source != "osm" {
		t.Fatalf("default source = %q, want %q", c.Source, "osm")
	}
	if c.Callsign != "" || c.Token != "" {
		t.Fatalf("expected empty callsign/token on fresh install, got %+v", c)
	}
}

func TestUpsertMapsConfig_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	in := MapsConfig{
		Source:       "graywolf",
		Callsign:     "N5XXX",
		Token:        "GKHkfi0a51nVZbiu_eJ7AqZ3YFvZY43Pvq4jOFZWDf0",
		RegisteredAt: now,
	}
	if err := s.UpsertMapsConfig(ctx, in); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := s.GetMapsConfig(ctx)
	if err != nil {
		t.Fatalf("Get after upsert: %v", err)
	}
	if got.Source != "graywolf" || got.Callsign != "N5XXX" || got.Token != in.Token {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestUpsertMapsConfig_RejectsBadSource(t *testing.T) {
	s := newTestStore(t)
	err := s.UpsertMapsConfig(context.Background(), MapsConfig{Source: "google"})
	if err == nil {
		t.Fatal("expected error for unknown source, got nil")
	}
	if !strings.Contains(err.Error(), "source must be") {
		t.Fatalf("error = %v, want contains 'source must be'", err)
	}
}

func TestUpsertMapsConfig_PreservesIDAcrossUpserts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.UpsertMapsConfig(ctx, MapsConfig{Source: "osm"}); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	first, _ := s.GetMapsConfig(ctx)
	if err := s.UpsertMapsConfig(ctx, MapsConfig{Source: "graywolf", Callsign: "N5XXX", Token: "abc"}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	second, _ := s.GetMapsConfig(ctx)
	if second.ID != first.ID {
		t.Fatalf("singleton invariant broken: id %d -> %d", first.ID, second.ID)
	}
}
```

If `newTestStore(t)` doesn't exist, mirror the helper from `seed_units_test.go` / `units_test.go`. Read the existing test file to confirm the helper name before running.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd graywolf && go test ./pkg/configstore/ -run TestGetMapsConfig -v`
Expected: FAIL with `s.GetMapsConfig undefined`.

- [ ] **Step 3: Implement `seed_maps.go`**

Mirror the `seed_units.go` shape exactly:

```go
package configstore

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

const (
	mapsSourceOSM      = "osm"
	mapsSourceGraywolf = "graywolf"
)

// GetMapsConfig returns the singleton maps preference. On a fresh
// install the row doesn't exist yet, so we synthesize a defaults
// struct (Source: "osm") rather than seeding at startup; the UI gets
// a deterministic baseline without an extra migration step.
func (s *Store) GetMapsConfig(ctx context.Context) (MapsConfig, error) {
	var c MapsConfig
	err := s.db.WithContext(ctx).Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return MapsConfig{Source: mapsSourceOSM}, nil
	}
	if err != nil {
		return MapsConfig{}, err
	}
	if c.Source != mapsSourceOSM && c.Source != mapsSourceGraywolf {
		c.Source = mapsSourceOSM
	}
	return c, nil
}

// UpsertMapsConfig persists the singleton maps preference. Source must
// be one of the two recognized values; anything else is rejected so a
// bad PUT can't corrupt the row. ID is adopted from any existing row
// to preserve the singleton invariant.
func (s *Store) UpsertMapsConfig(ctx context.Context, c MapsConfig) error {
	if c.Source != mapsSourceOSM && c.Source != mapsSourceGraywolf {
		return errors.New("source must be 'osm' or 'graywolf'")
	}
	db := s.db.WithContext(ctx)
	if c.ID == 0 {
		var existing MapsConfig
		err := db.Order("id").First(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil {
			c.ID = existing.ID
		}
	}
	cols := map[string]any{
		"source":        c.Source,
		"callsign":      c.Callsign,
		"token":         c.Token,
		"registered_at": c.RegisteredAt,
	}
	if c.ID == 0 {
		return db.Model(&MapsConfig{}).Create(cols).Error
	}
	return db.Model(&MapsConfig{}).Where("id = ?", c.ID).UpdateColumns(cols).Error
}
```

- [ ] **Step 4: Run the tests and verify they pass**

Run: `cd graywolf && go test ./pkg/configstore/ -run TestGetMapsConfig -run TestUpsertMapsConfig -v`
Expected: 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add graywolf/pkg/configstore/seed_maps.go graywolf/pkg/configstore/seed_maps_test.go
git commit -m "configstore: add Get/UpsertMapsConfig with validation"
```

---

### Task 3: Build the `mapsauth` HTTP client

**Files:**
- Create: `graywolf/pkg/mapsauth/client.go`
- Create: `graywolf/pkg/mapsauth/client_test.go`

The client is a thin wrapper over `POST /register`. We isolate it from `webapi` so handler tests can stub the HTTP call without spinning up a real network listener, and so the URL is configurable per-environment.

- [ ] **Step 1: Write the failing tests**

Create `graywolf/pkg/mapsauth/client_test.go`:

```go
package mapsauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegister_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/register" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("content-type = %q", ct)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"callsign":"N5XXX","token":"GKHkfi0a51nVZbiu_eJ7AqZ3YFvZY43Pvq4jOFZWDf0"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	out, err := c.Register(context.Background(), "N5XXX")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if out.Callsign != "N5XXX" || !strings.HasPrefix(out.Token, "GK") {
		t.Fatalf("unexpected response: %+v", out)
	}
}

func TestRegister_DeviceLimitReached(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"device_limit_reached","message":"Registration failed. Please file an issue at https://github.com/chrissnell/graywolf/issues"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Register(context.Background(), "N5XXX")
	var rerr *Error
	if !errors.As(err, &rerr) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if rerr.Code != "device_limit_reached" || rerr.Status != http.StatusConflict {
		t.Fatalf("unexpected error: %+v", rerr)
	}
	if !strings.Contains(rerr.Message, "github.com/chrissnell/graywolf/issues") {
		t.Fatalf("message missing issue URL: %q", rerr.Message)
	}
}

func TestRegister_RateLimitedEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Register(context.Background(), "N5XXX")
	var rerr *Error
	if !errors.As(err, &rerr) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if rerr.Status != http.StatusTooManyRequests || rerr.Code != "rate_limited" {
		t.Fatalf("unexpected error: %+v", rerr)
	}
}

func TestRegister_BlockedShouldNotRetry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"blocked","message":"Registration failed. Please file an issue at https://github.com/chrissnell/graywolf/issues"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Register(context.Background(), "N5XXX")
	var rerr *Error
	if !errors.As(err, &rerr) || rerr.Code != "blocked" {
		t.Fatalf("expected blocked error, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `cd graywolf && go test ./pkg/mapsauth/ -v`
Expected: FAIL — package doesn't exist.

- [ ] **Step 3: Implement the client**

Create `graywolf/pkg/mapsauth/client.go`:

```go
// Package mapsauth is a thin client for auth.nw5w.com, the registration
// endpoint that issues per-device tokens for the Graywolf private map
// service. The package exists so handlers in pkg/webapi don't have to
// hand-roll JSON+timeout+error-mapping code, and so tests can swap in
// an httptest.Server via the BaseURL constructor argument.
package mapsauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is the production registration endpoint. Override
// via NewClient when stubbing in tests.
const DefaultBaseURL = "https://auth.nw5w.com"

// Client is the registration client. Construct via NewClient.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient returns a client pointed at baseURL. Pass DefaultBaseURL
// in production. The HTTP client has a 15-second total timeout — the
// auth worker is fast under normal load, and a slow server is more
// likely a routing problem than a real deferred response.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// RegisterResponse is the success body emitted by POST /register.
type RegisterResponse struct {
	Callsign string `json:"callsign"`
	Token    string `json:"token"`
}

// Error wraps a non-2xx response from the auth server. Status holds
// the HTTP status; Code is the machine-readable error name from the
// response body (or a synthesized value for empty-body cases like
// 429); Message is the human-readable string the server returned and
// is intended to be surfaced to the user verbatim — it points them
// at https://github.com/chrissnell/graywolf/issues.
type Error struct {
	Status  int    `json:"-"`
	Code    string `json:"error"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("mapsauth: %s (status %d): %s", e.Code, e.Status, e.Message)
}

// Register POSTs the callsign to /register and returns the issued
// token on success. callsign should already be uppercased and have
// any -SSID stripped; the server will reject anything that doesn't
// match ^[A-Z0-9]{3,9}$ with a digit.
func (c *Client) Register(ctx context.Context, callsign string) (RegisterResponse, error) {
	body, err := json.Marshal(map[string]string{"callsign": callsign})
	if err != nil {
		return RegisterResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/register", bytes.NewReader(body))
	if err != nil {
		return RegisterResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return RegisterResponse{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusOK {
		var out RegisterResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			return RegisterResponse{}, fmt.Errorf("mapsauth: decode success body: %w", err)
		}
		return out, nil
	}

	// 429 has an empty body per the integration doc; synthesize.
	if resp.StatusCode == http.StatusTooManyRequests {
		return RegisterResponse{}, &Error{
			Status:  resp.StatusCode,
			Code:    "rate_limited",
			Message: "Too many registration attempts. Please wait a moment and try again.",
		}
	}

	rerr := &Error{Status: resp.StatusCode}
	if err := json.Unmarshal(raw, rerr); err != nil || rerr.Code == "" {
		// 5xx with HTML, or unrecognized body — synthesize.
		rerr.Code = "internal"
		rerr.Message = "Registration failed. Please file an issue at https://github.com/chrissnell/graywolf/issues"
	}
	return RegisterResponse{}, rerr
}
```

- [ ] **Step 4: Run tests, verify they pass**

Run: `cd graywolf && go test ./pkg/mapsauth/ -v`
Expected: 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add graywolf/pkg/mapsauth/
git commit -m "mapsauth: client for auth.nw5w.com /register"
```

---

### Task 4: Add maps DTOs

**Files:**
- Create: `graywolf/pkg/webapi/dto/maps.go`

- [ ] **Step 1: Define request/response DTOs**

```go
package dto

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

// MapsConfigRequest is the body for PUT /api/preferences/maps. Only
// Source is updatable from the client; Callsign and Token are managed
// by the /register sub-endpoint to keep the registration ceremony
// out of generic preference writes.
type MapsConfigRequest struct {
	Source string `json:"source"`
}

func (r MapsConfigRequest) Validate() error {
	if r.Source != "osm" && r.Source != "graywolf" {
		return errors.New("source must be 'osm' or 'graywolf'")
	}
	return nil
}

// MapsConfigResponse is what GET /api/preferences/maps and the PUT
// echo back. Token is omitted unless ?include_token=1 is set on the
// GET — see the handler. Registered is true iff a token is present.
type MapsConfigResponse struct {
	Source       string    `json:"source"`
	Callsign     string    `json:"callsign,omitempty"`
	Registered   bool      `json:"registered"`
	RegisteredAt time.Time `json:"registered_at,omitempty"`
	Token        string    `json:"token,omitempty"`
}

// RegisterRequest is the body for POST /api/preferences/maps/register.
type RegisterRequest struct {
	Callsign string `json:"callsign"`
}

var callsignRE = regexp.MustCompile(`^[A-Z0-9]{3,9}$`)

// NormalizeCallsign uppercases, strips -SSID, and validates the format.
// Returns the cleaned callsign or an error matching the server's
// "must include at least one digit" rule. Used both by the client-side
// pre-flight (in JS, mirrored) and the backend handler.
func NormalizeCallsign(in string) (string, error) {
	s := strings.ToUpper(strings.TrimSpace(in))
	if i := strings.Index(s, "-"); i >= 0 {
		s = s[:i]
	}
	if !callsignRE.MatchString(s) {
		return "", errors.New("callsign must be 3-9 characters, letters and digits only")
	}
	hasDigit := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			hasDigit = true
			break
		}
	}
	if !hasDigit {
		return "", errors.New("callsign must contain at least one digit")
	}
	return s, nil
}

// RegisterResponse mirrors MapsConfigResponse — after a successful
// registration, the endpoint returns the same shape the GET would
// return next, including the freshly issued token (always, just this
// once, so the UI can offer the operator an export-token-to-file flow
// before it goes back to being suppressed).
type RegisterResponse = MapsConfigResponse
```

- [ ] **Step 2: Build to confirm**

Run: `cd graywolf && go build ./pkg/webapi/dto/`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add graywolf/pkg/webapi/dto/maps.go
git commit -m "webapi/dto: add Maps preferences and Register DTOs"
```

---

### Task 5: Add the `/api/preferences/maps` handlers

**Files:**
- Create: `graywolf/pkg/webapi/maps.go`
- Create: `graywolf/pkg/webapi/maps_test.go`
- Modify: `graywolf/pkg/webapi/server.go` — wire `s.mapsAuth` into the constructor and call `s.registerMaps(mux)` from `RegisterRoutes`

- [ ] **Step 1: Write the failing tests**

Create `graywolf/pkg/webapi/maps_test.go` (read `units_test.go` first to find the existing `newTestServer` helper signature; if `mapsauth.Client` needs to be injectable, the helper may need an option). The tests must cover:

```go
package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

func TestGetMapsConfig_DefaultsForFreshInstall(t *testing.T) {
	s := newTestServer(t)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/preferences/maps", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["source"] != "osm" {
		t.Fatalf("default source = %v, want osm", resp["source"])
	}
	if resp["registered"] != false {
		t.Fatalf("expected registered=false on fresh install, got %v", resp["registered"])
	}
	if _, present := resp["token"]; present {
		t.Fatalf("token must not be present without ?include_token=1")
	}
}

func TestPutMapsConfig_RejectsGraywolfWithoutToken(t *testing.T) {
	s := newTestServer(t)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	body := strings.NewReader(`{"source":"graywolf"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/preferences/maps", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
}

func TestRegister_HappyPath_PersistsAndReturnsToken(t *testing.T) {
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"callsign":"N5XXX","token":"GKHkfi0a51nVZbiu_eJ7AqZ3YFvZY43Pvq4jOFZWDf0"}`))
	}))
	defer auth.Close()

	s := newTestServerWithAuth(t, auth.URL)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/preferences/maps/register",
		strings.NewReader(`{"callsign":"n5xxx-9"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["callsign"] != "N5XXX" {
		t.Fatalf("callsign = %v, want N5XXX (uppercased, SSID stripped)", resp["callsign"])
	}
	if resp["token"] == nil || resp["token"] == "" {
		t.Fatalf("token must be present in register response")
	}

	c, err := s.store.GetMapsConfig(context.Background())
	if err != nil {
		t.Fatalf("read after register: %v", err)
	}
	if c.Token == "" || c.Callsign != "N5XXX" {
		t.Fatalf("did not persist: %+v", c)
	}
}

func TestRegister_PropagatesUpstreamError(t *testing.T) {
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"device_limit_reached","message":"Registration failed. Please file an issue at https://github.com/chrissnell/graywolf/issues"}`))
	}))
	defer auth.Close()

	s := newTestServerWithAuth(t, auth.URL)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/preferences/maps/register",
		strings.NewReader(`{"callsign":"N5XXX"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["error"] != "device_limit_reached" {
		t.Fatalf("error code = %v", resp["error"])
	}
	if !strings.Contains(resp["message"].(string), "github.com/chrissnell/graywolf/issues") {
		t.Fatalf("message must include issues URL verbatim, got %v", resp["message"])
	}

	// Did NOT persist token — registration failed.
	c, _ := s.store.GetMapsConfig(context.Background())
	if c.Token != "" {
		t.Fatalf("expected no token persisted on failure, got %q", c.Token)
	}
}

func TestGetMapsConfig_IncludeTokenFlag(t *testing.T) {
	s := newTestServer(t)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	// Seed a token directly.
	_ = s.store.UpsertMapsConfig(context.Background(), configstore.MapsConfig{
		Source: "graywolf", Callsign: "N5XXX", Token: "tok-abc",
	})

	// Default GET: no token.
	req := httptest.NewRequest(http.MethodGet, "/api/preferences/maps", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), "tok-abc") {
		t.Fatalf("token leaked without explicit opt-in: %s", rec.Body.String())
	}

	// With ?include_token=1: token present.
	req = httptest.NewRequest(http.MethodGet, "/api/preferences/maps?include_token=1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "tok-abc") {
		t.Fatalf("token not returned with include_token=1: %s", rec.Body.String())
	}
}
```

The helper `newTestServerWithAuth(t, baseURL string)` doesn't exist yet — extend the existing helper file in `pkg/webapi/` (look for the test setup helper near `units_test.go`) to accept an optional auth URL. If the existing helper signature is `newTestServer(t *testing.T)`, add a sibling that takes a `mapsauth.Client` or its base URL. Mirror the pattern used elsewhere in the package — DO NOT make up a new pattern.

- [ ] **Step 2: Run tests, verify they fail**

Run: `cd graywolf && go test ./pkg/webapi/ -run TestGetMapsConfig -run TestPutMapsConfig -run TestRegister_ -v`
Expected: FAIL — handlers and helpers don't exist.

- [ ] **Step 3: Implement `maps.go`**

```go
package webapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/mapsauth"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerMaps(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/preferences/maps", s.getMapsConfig)
	mux.HandleFunc("PUT /api/preferences/maps", s.updateMapsConfig)
	mux.HandleFunc("POST /api/preferences/maps/register", s.registerMapsToken)
}

func (s *Server) getMapsConfig(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetMapsConfig(r.Context())
	if err != nil {
		s.internalError(w, r, "get maps config", err)
		return
	}
	resp := dto.MapsConfigResponse{
		Source:     c.Source,
		Callsign:   c.Callsign,
		Registered: c.Token != "",
	}
	if !c.RegisteredAt.IsZero() {
		resp.RegisteredAt = c.RegisteredAt
	}
	if r.URL.Query().Get("include_token") == "1" && c.Token != "" {
		resp.Token = c.Token
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) updateMapsConfig(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[dto.MapsConfigRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	current, err := s.store.GetMapsConfig(r.Context())
	if err != nil {
		s.internalError(w, r, "get maps config", err)
		return
	}
	// Refuse "graywolf" source if no token is registered yet.
	if req.Source == "graywolf" && current.Token == "" {
		badRequest(w, "register this device before selecting Graywolf maps")
		return
	}
	current.Source = req.Source
	if err := s.store.UpsertMapsConfig(r.Context(), current); err != nil {
		s.internalError(w, r, "upsert maps config", err)
		return
	}
	s.getMapsConfig(w, r)
}

func (s *Server) registerMapsToken(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[dto.RegisterRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	callsign, err := dto.NormalizeCallsign(req.Callsign)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	out, err := s.mapsAuth.Register(r.Context(), callsign)
	if err != nil {
		var rerr *mapsauth.Error
		if errors.As(err, &rerr) {
			writeJSON(w, rerr.Status, map[string]string{
				"error":   rerr.Code,
				"message": rerr.Message,
			})
			return
		}
		s.internalError(w, r, "register with auth.nw5w.com", err)
		return
	}
	c, _ := s.store.GetMapsConfig(r.Context())
	c.Source = "graywolf"
	c.Callsign = out.Callsign
	c.Token = out.Token
	c.RegisteredAt = time.Now().UTC()
	if err := s.store.UpsertMapsConfig(r.Context(), c); err != nil {
		s.internalError(w, r, "persist registration", err)
		return
	}
	writeJSON(w, http.StatusOK, dto.MapsConfigResponse{
		Source:       c.Source,
		Callsign:     c.Callsign,
		Registered:   true,
		RegisteredAt: c.RegisteredAt,
		Token:        c.Token, // returned exactly once, on the registration response
	})
	_ = configstore.MapsConfig{} // keep the import alive; used implicitly via store.
}
```

- [ ] **Step 4: Wire into the server struct and `RegisterRoutes`**

Edit `graywolf/pkg/webapi/server.go`:
- Add field: `mapsAuth *mapsauth.Client` to the `Server` struct
- Update the constructor to accept it (see existing constructor — it likely takes `Options` or similar; mirror that). If it currently takes a long arg list, add a setter method `SetMapsAuth(*mapsauth.Client)` instead and call it from `pkg/app/wiring.go`.
- In `RegisterRoutes` (line 175-197), add `s.registerMaps(mux)` after `s.registerTheme(mux)`.

Update the test helper to construct the client. If the existing helper is in a `*_test.go` file, add:

```go
func newTestServerWithAuth(t *testing.T, authBaseURL string) *Server {
	s := newTestServer(t)
	s.mapsAuth = mapsauth.NewClient(authBaseURL)
	return s
}
```

And ensure `newTestServer` constructs a non-nil `mapsAuth` (pointing at an unreachable URL) so handlers don't panic.

- [ ] **Step 5: Run tests, verify all pass**

Run: `cd graywolf && go test ./pkg/webapi/ -v -run TestGetMapsConfig -run TestPutMapsConfig -run TestRegister_`
Expected: 5 tests PASS.

- [ ] **Step 6: Run the full webapi test suite to confirm no regressions**

Run: `cd graywolf && go test ./pkg/webapi/...`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add graywolf/pkg/webapi/maps.go graywolf/pkg/webapi/maps_test.go graywolf/pkg/webapi/server.go
git commit -m "webapi: add /api/preferences/maps endpoints with auth.nw5w.com proxy"
```

---

### Task 6: Plumb `mapsauth.Client` and `--tile-cache-dir` through wiring

**Files:**
- Modify: `graywolf/pkg/app/wiring.go` (find the function that constructs the webapi `Server`)
- Modify: the file that registers CLI flags (find it via `grep -rn "flag.String\|flag.Parse" graywolf/cmd/ graywolf/pkg/app/`)

- [ ] **Step 1: Discover the flag-registration site**

Run: `cd graywolf && grep -rn "flag.String\|flag.StringVar\|flag.Parse\|kingpin\|cobra\|cli.New" cmd/ pkg/app/ | head -20`

Identify the file (likely `cmd/graywolf/main.go` or `pkg/app/flags.go`). Read 30 lines around the existing flag block to mirror the pattern (default value style, env-var fallback, comment style).

- [ ] **Step 2: Add the `--tile-cache-dir` flag**

Add the flag with default `<config-dir>/tiles` (resolve relative to the existing `--config-dir` or DB path flag). Example shape, adapt to actual flag library:

```go
tileCacheDir := flag.String(
    "tile-cache-dir",
    filepath.Join(configDir, "tiles"),
    "directory for offline PMTiles cache; created on startup if missing",
)
```

In `wiring.go`, ensure the directory exists at startup:

```go
if err := os.MkdirAll(opts.TileCacheDir, 0o755); err != nil {
    return nil, fmt.Errorf("create tile cache dir %q: %w", opts.TileCacheDir, err)
}
```

Stash the path on the `App` (or equivalent) struct so Plan 2 can read it without re-touching CLI plumbing.

- [ ] **Step 3: Construct `mapsauth.Client` and inject into webapi.Server**

In `wiring.go`, after the webapi server is built:

```go
import "github.com/chrissnell/graywolf/pkg/mapsauth"
// ...
apiServer.SetMapsAuth(mapsauth.NewClient(mapsauth.DefaultBaseURL))
```

(If the constructor takes options, pass it via options instead.)

- [ ] **Step 4: Build and run the binary**

Run: `cd graywolf && make all` (or `go build ./cmd/graywolf`).
Expected: clean build.

- [ ] **Step 5: Smoke test the new endpoint**

Run the binary, then:
```bash
curl -s http://localhost:<port>/api/preferences/maps
```
Expected: `{"source":"osm","registered":false}`.

- [ ] **Step 6: Commit**

```bash
git add graywolf/pkg/app/wiring.go <flag-file>
git commit -m "app: add --tile-cache-dir flag and wire mapsauth client"
```

---

### Task 7: Wire DB file permissions (defensive 0o600)

**Files:**
- Modify: `graywolf/pkg/configstore/store.go` `Open` function (or wherever the SQLite file is created)

The token isn't highly sensitive (Chris OKs storing in DB), but a 0o600 chmod on the SQLite file is a one-line hygiene win that prevents the user's home folder from being accidentally world-readable.

- [ ] **Step 1: Locate the SQLite file open path**

Run: `cd graywolf && grep -n "glebarez\|sqlite.Open\|gorm.Open" pkg/configstore/store.go`

- [ ] **Step 2: After the DB file exists, set its mode to 0o600**

After `gorm.Open(...)` returns successfully, before returning the store, add:

```go
// Best-effort: tighten the DB file permissions. The file may not exist
// on some drivers (e.g. :memory:); ignore errors silently — this is
// hygiene, not a security control.
if !strings.HasPrefix(path, ":") {
    _ = os.Chmod(path, 0o600)
}
```

- [ ] **Step 3: Build & run; verify with `ls -l <db-path>`**

Run: `ls -l <config-dir>/graywolf.db`
Expected: mode `-rw-------`.

- [ ] **Step 4: Commit**

```bash
git add graywolf/pkg/configstore/store.go
git commit -m "configstore: chmod 0o600 on SQLite file after open"
```

---

## Phase 2: Maps Settings UI

### Task 8: Add the Svelte store

**Files:**
- Create: `graywolf/web/src/lib/maps/callsign.js`
- Create: `graywolf/web/src/lib/settings/maps-store.svelte.js`

- [ ] **Step 1: Create the callsign helper**

```js
// graywolf/web/src/lib/maps/callsign.js
//
// Pure-function callsign normalizer that mirrors the server-side regex
// (^[A-Z0-9]{3,9}$, must contain a digit, SSID stripped). Used by the
// Maps Settings form for immediate validation feedback before hitting
// the wire — server is still authoritative.

const CALLSIGN_RE = /^[A-Z0-9]{3,9}$/;

export function normalizeCallsign(input) {
  const upper = String(input ?? '').trim().toUpperCase();
  const idx = upper.indexOf('-');
  const base = idx >= 0 ? upper.slice(0, idx) : upper;
  return base;
}

export function validateCallsign(input) {
  const cs = normalizeCallsign(input);
  if (!CALLSIGN_RE.test(cs)) {
    return { ok: false, message: 'Callsign must be 3-9 letters and digits.' };
  }
  if (!/[0-9]/.test(cs)) {
    return { ok: false, message: 'Callsign must contain at least one digit.' };
  }
  return { ok: true, callsign: cs };
}
```

- [ ] **Step 2: Create the maps store**

```js
// graywolf/web/src/lib/settings/maps-store.svelte.js
//
// Reactive Maps preferences store. Mirrors GET /api/preferences/maps;
// PUTs the source change; calls POST /register through the backend
// proxy so the auth.nw5w.com URL is fixed in one place. After
// registration, the response carries a one-time token which the store
// holds in memory only (so the "Show token" / "Back up token" UI can
// surface it without a second round-trip), but the persisted value
// always comes from the GET — no localStorage mirror for the token.

import { toasts } from '../stores.js';
import { normalizeCallsign } from '../maps/callsign.js';

export const ISSUES_URL = 'https://github.com/chrissnell/graywolf/issues';

function emptyConfig() {
  return {
    source: 'osm',
    callsign: '',
    registered: false,
    registeredAt: null,
    token: null, // populated only immediately after registration
  };
}

export const mapsState = (() => {
  let cfg = $state(emptyConfig());
  let loaded = $state(false);
  let registering = $state(false);

  async function fetchConfig() {
    try {
      const res = await fetch('/api/preferences/maps', { credentials: 'same-origin' });
      if (res.status === 401) return;
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      cfg = {
        source: data.source ?? 'osm',
        callsign: data.callsign ?? '',
        registered: !!data.registered,
        registeredAt: data.registered_at ? new Date(data.registered_at) : null,
        token: cfg.token, // preserve in-memory token across refetches
      };
      loaded = true;
    } catch {
      // Silent — leave defaults; toast happens on user-initiated actions.
    }
  }

  async function setSource(next) {
    const prev = cfg.source;
    cfg = { ...cfg, source: next };
    try {
      const res = await fetch('/api/preferences/maps', {
        method: 'PUT',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ source: next }),
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body?.error || `HTTP ${res.status}`);
      }
      const data = await res.json();
      cfg = { ...cfg, source: data.source };
    } catch (e) {
      cfg = { ...cfg, source: prev };
      toasts.error(`Couldn't change map source: ${e.message}`);
    }
  }

  async function register(rawCallsign) {
    const cs = normalizeCallsign(rawCallsign);
    registering = true;
    try {
      const res = await fetch('/api/preferences/maps/register', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ callsign: cs }),
      });
      const body = await res.json().catch(() => ({}));
      if (!res.ok) {
        return {
          ok: false,
          status: res.status,
          code: body.error ?? 'unknown',
          message: body.message ?? 'Registration failed.',
        };
      }
      cfg = {
        source: body.source,
        callsign: body.callsign,
        registered: true,
        registeredAt: body.registered_at ? new Date(body.registered_at) : new Date(),
        token: body.token, // one-time, in-memory until next fetch
      };
      return { ok: true, token: body.token };
    } catch (e) {
      return {
        ok: false,
        status: 0,
        code: 'network',
        message: `Network error: ${e.message}. Please file an issue at ${ISSUES_URL}`,
      };
    } finally {
      registering = false;
    }
  }

  async function revealToken() {
    const res = await fetch('/api/preferences/maps?include_token=1', { credentials: 'same-origin' });
    if (!res.ok) return null;
    const data = await res.json();
    return data.token ?? null;
  }

  return {
    get source() { return cfg.source; },
    get callsign() { return cfg.callsign; },
    get registered() { return cfg.registered; },
    get registeredAt() { return cfg.registeredAt; },
    get tokenOnce() { return cfg.token; },
    get loaded() { return loaded; },
    get registering() { return registering; },

    fetchConfig,
    setSource,
    register,
    revealToken,
  };
})();
```

- [ ] **Step 3: Run `npm run build` to confirm no TS/syntax errors**

Run: `cd graywolf/web && npm run build`
Expected: clean build.

- [ ] **Step 4: Commit**

```bash
git add graywolf/web/src/lib/maps/callsign.js graywolf/web/src/lib/settings/maps-store.svelte.js
git commit -m "ui(maps): add callsign helper and maps preferences store"
```

---

### Task 9: Add the Maps Settings route + sidebar entry

**Files:**
- Create: `graywolf/web/src/routes/MapsSettings.svelte` (initial scaffold; full UI in Tasks 10-15)
- Modify: `graywolf/web/src/App.svelte`
- Modify: `graywolf/web/src/components/Sidebar.svelte`

- [ ] **Step 1: Add an empty MapsSettings page**

```svelte
<!-- graywolf/web/src/routes/MapsSettings.svelte -->
<script>
  import { onMount } from 'svelte';
  import { mapsState } from '../lib/settings/maps-store.svelte.js';
  import PageHeader from '../components/PageHeader.svelte';

  onMount(() => mapsState.fetchConfig());
</script>

<PageHeader title="Maps" subtitle="Choose your basemap source and register for private maps" />

<p>Maps Settings — under construction.</p>
```

- [ ] **Step 2: Wire the route**

In `graywolf/web/src/App.svelte`, alongside the other route imports, import `MapsSettings`. In the routes object (lines 32-53), add:

```js
'/preferences/maps': MapsSettings,
```

- [ ] **Step 3: Add the sidebar entry**

In `graywolf/web/src/components/Sidebar.svelte:34-43` (the Settings group `items` array), insert after the Preferences entry:

```js
{ path: '/preferences/maps', label: 'Maps' },
```

- [ ] **Step 4: Run dev server, navigate to /preferences/maps**

Run: `cd graywolf/web && npm run dev`
Open browser to `http://localhost:5173/#/preferences/maps`.
Expected: "Maps" appears in the sidebar, clicking it shows the placeholder page.

- [ ] **Step 5: Commit**

```bash
git add graywolf/web/src/routes/MapsSettings.svelte graywolf/web/src/App.svelte graywolf/web/src/components/Sidebar.svelte
git commit -m "ui(maps): add /preferences/maps route and sidebar entry"
```

---

### Task 10: Build the disclosure / consent block

**Files:**
- Modify: `graywolf/web/src/routes/MapsSettings.svelte`
- Create: `graywolf/web/src/lib/maps/styles.css`

The disclosure block is the first thing the user sees when "Graywolf private maps" is selected but not yet registered. It must explain:
- These maps are provided free at Chris Snell's (NW5W) personal expense
- What gets transmitted: callsign + IP address (capturing IP is server-side, but we disclose it)
- No other personal info collected
- Why the registration exists (anti-abuse)
- Operators must opt in

- [ ] **Step 1: Design the disclosure component inline in MapsSettings.svelte**

Replace the placeholder body with the consent block + a checkbox-gated "Continue to registration" CTA:

```svelte
<script>
  import { onMount } from 'svelte';
  import { Box, Toggle, Button } from '@chrissnell/chonky-ui';
  import { mapsState } from '../lib/settings/maps-store.svelte.js';
  import PageHeader from '../components/PageHeader.svelte';

  let consented = $state(false);

  onMount(() => mapsState.fetchConfig());
</script>

<PageHeader title="Maps" subtitle="Choose your basemap source" />

{#if !mapsState.registered}
  <Box title="About Graywolf private maps">
    <p class="prose">
      Graywolf can use a private, prettier basemap hosted by the project author,
      <strong>Chris Snell (NW5W)</strong>. Chris pays for the hosting and bandwidth
      personally, and provides this map to the amateur radio community at no cost.
    </p>
    <p class="prose">
      To prevent abuse from non-amateur clients, the map server requires a one-time
      registration per device.
    </p>
    <h3 class="prose-heading">What is sent during registration</h3>
    <ul class="prose-list">
      <li>Your callsign (uppercase, without -SSID).</li>
      <li>Your IP address, captured by the server.</li>
    </ul>
    <p class="prose">
      Nothing else. No email, no name, no metadata. Each install registers
      independently — your laptop and your tablet each get their own token.
    </p>
    <Toggle
      checked={consented}
      onCheckedChange={(v) => (consented = v)}
      label="I understand and agree."
    />
  </Box>
{/if}

<style>
  @import '../lib/maps/styles.css';
</style>
```

- [ ] **Step 2: Create the shared styles file**

```css
/* graywolf/web/src/lib/maps/styles.css */

.prose {
  font-size: 14px;
  line-height: 1.55;
  color: var(--text-primary);
  margin: 0 0 12px;
}
.prose-heading {
  font-size: 13px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 1px;
  color: var(--text-secondary);
  margin: 16px 0 8px;
}
.prose-list {
  margin: 0 0 12px 18px;
  padding: 0;
  font-size: 14px;
  line-height: 1.55;
  color: var(--text-primary);
}
.prose-list li {
  margin-bottom: 4px;
}

/* Mobile-first: stack the form vertically; tablet+ allows side-by-side. */
.maps-row {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
@media (min-width: 600px) {
  .maps-row {
    flex-direction: row;
    align-items: end;
  }
}

.maps-cta {
  /* Primary CTA spans the row on mobile, sits inline on tablet+. */
  width: 100%;
  min-height: 44px;
}
@media (min-width: 600px) {
  .maps-cta {
    width: auto;
  }
}
```

- [ ] **Step 3: View on dev server at 375px and 1280px**

Run dev server, narrow Chrome DevTools to 375px, confirm:
- Type is readable (≥14px body)
- No horizontal scroll
- Toggle hit target ≥ 44px
- Resize to 1280px: layout adapts, no awkward wide blocks

- [ ] **Step 4: Commit**

```bash
git add graywolf/web/src/routes/MapsSettings.svelte graywolf/web/src/lib/maps/styles.css
git commit -m "ui(maps): add disclosure/consent block for private maps"
```

---

### Task 11: Build the registration form

**Files:**
- Modify: `graywolf/web/src/routes/MapsSettings.svelte`

- [ ] **Step 1: Add the form below the disclosure**

```svelte
<script>
  // ...existing imports...
  import { Box, Toggle, Button, TextField } from '@chrissnell/chonky-ui';
  import { validateCallsign } from '../lib/maps/callsign.js';
  import { ISSUES_URL } from '../lib/settings/maps-store.svelte.js';

  let consented = $state(false);
  let callsignInput = $state('');
  let lastError = $state(null);  // { code, message, status }

  let validation = $derived(validateCallsign(callsignInput));
  let canSubmit = $derived(consented && validation.ok && !mapsState.registering);

  async function onRegister() {
    lastError = null;
    const result = await mapsState.register(callsignInput);
    if (!result.ok) {
      lastError = result;
    } else {
      callsignInput = '';
    }
  }
</script>

<!-- ...existing disclosure Box... -->

{#if !mapsState.registered}
  <Box title="Register this device">
    <div class="maps-row">
      <TextField
        label="Your callsign"
        placeholder="N5XXX"
        value={callsignInput}
        oninput={(e) => (callsignInput = e.currentTarget.value)}
        autocapitalize="characters"
        autocomplete="off"
        spellcheck="false"
        inputmode="text"
        disabled={!consented}
      />
      <Button
        class="maps-cta"
        variant="primary"
        disabled={!canSubmit}
        onclick={onRegister}
        loading={mapsState.registering}
      >
        Register
      </Button>
    </div>
    {#if callsignInput && !validation.ok}
      <p class="form-hint form-hint-error">{validation.message}</p>
    {:else if !consented}
      <p class="form-hint">Tick "I understand and agree" above to continue.</p>
    {:else}
      <p class="form-hint">We will send <code>{validation.callsign ?? '...'}</code> to <code>auth.nw5w.com</code>.</p>
    {/if}

    {#if lastError}
      <div class="error-card" role="alert">
        <h3>Registration failed</h3>
        <p>{lastError.message}</p>
        {#if lastError.code === 'device_limit_reached'}
          <p>This callsign has reached its 40-device limit. Please open an issue at the link above so the operator can rotate tokens for you.</p>
        {:else if lastError.code === 'rate_limited'}
          <p>Wait about 10 seconds and try again.</p>
        {:else if lastError.code === 'blocked'}
          <p>This callsign has been blocked. Please open an issue at the link above to ask the operator about it.</p>
        {/if}
        <a class="error-link" href={ISSUES_URL} target="_blank" rel="noreferrer noopener">
          Open a GitHub issue
        </a>
      </div>
    {/if}
  </Box>
{/if}
```

Append to `styles.css`:

```css
.form-hint {
  margin: 12px 0 0;
  font-size: 13px;
  color: var(--text-muted);
}
.form-hint code {
  font-family: var(--font-mono);
  font-size: 12px;
}
.form-hint-error {
  color: var(--color-danger);
}
.error-card {
  margin-top: 16px;
  padding: 12px 16px;
  border: 1px solid var(--color-danger);
  border-left-width: 4px;
  border-radius: 6px;
  background: color-mix(in srgb, var(--color-danger) 8%, var(--bg-secondary));
}
.error-card h3 {
  margin: 0 0 6px;
  font-size: 13px;
  font-weight: 700;
  color: var(--color-danger);
}
.error-card p {
  margin: 0 0 6px;
  font-size: 13px;
  line-height: 1.45;
}
.error-link {
  display: inline-block;
  margin-top: 4px;
  min-height: 44px;
  line-height: 44px;
  font-weight: 600;
  color: var(--accent);
}
```

- [ ] **Step 2: Verify error states by short-circuiting the network**

In Chrome DevTools, throttle to "Offline", click Register, confirm the network-error card appears with the issue link.

- [ ] **Step 3: Verify success path against the real auth endpoint** (or a local httptest stub)

If you have a sandbox callsign you don't mind burning a slot for, register against the real `auth.nw5w.com`. Otherwise launch the binary with a stub auth server (set `MAPSAUTH_BASE_URL` env var if you added env-var fallback in Task 6, or run with the test stub).

Confirm: page transitions to "registered" state (Task 12 builds that view; for now, just confirm `mapsState.registered === true` in DevTools console).

- [ ] **Step 4: Commit**

```bash
git add graywolf/web/src/routes/MapsSettings.svelte graywolf/web/src/lib/maps/styles.css
git commit -m "ui(maps): add registration form with verbatim server error display"
```

---

### Task 12: Build the registered-state UI (callsign, re-register, show/back-up token)

**Files:**
- Modify: `graywolf/web/src/routes/MapsSettings.svelte`

- [ ] **Step 1: Add the registered-state branch**

```svelte
<script>
  // ...existing imports...
  let revealedToken = $state(null);
  let revealing = $state(false);

  async function onShowToken() {
    revealing = true;
    revealedToken = await mapsState.revealToken();
    revealing = false;
  }

  async function onCopyToken() {
    const t = revealedToken ?? mapsState.tokenOnce;
    if (!t) return;
    try {
      await navigator.clipboard.writeText(t);
      // Use existing toasts API
    } catch {}
  }

  function onDownloadToken() {
    const t = revealedToken ?? mapsState.tokenOnce;
    if (!t) return;
    const blob = new Blob([`callsign: ${mapsState.callsign}\ntoken: ${t}\n`], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `graywolf-maps-${mapsState.callsign.toLowerCase()}.txt`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  }

  async function onReregister() {
    revealedToken = null;
    const result = await mapsState.register(mapsState.callsign);
    if (!result.ok) {
      lastError = result;
    }
  }
</script>

{#if mapsState.registered}
  <Box title="Registered">
    <p class="prose">
      This device is registered as <code>{mapsState.callsign}</code>.
      {#if mapsState.registeredAt}
        Registered {mapsState.registeredAt.toLocaleString()}.
      {/if}
    </p>

    {#if mapsState.tokenOnce}
      <div class="token-once" role="region" aria-label="Token displayed once">
        <p class="prose">
          <strong>Save this token.</strong> The server only emits it once;
          if you lose it, click "Re-register this device" below to get a new one.
        </p>
        <code class="token-display">{mapsState.tokenOnce}</code>
        <div class="maps-row">
          <Button class="maps-cta" onclick={onCopyToken}>Copy</Button>
          <Button class="maps-cta" onclick={onDownloadToken}>Download as file</Button>
        </div>
      </div>
    {:else}
      <div class="maps-row">
        <Button class="maps-cta" onclick={onShowToken} loading={revealing}>
          {revealedToken ? 'Hide token' : 'Show token'}
        </Button>
        <Button class="maps-cta" variant="secondary" onclick={onReregister} loading={mapsState.registering}>
          Re-register this device
        </Button>
      </div>
      {#if revealedToken}
        <code class="token-display">{revealedToken}</code>
      {/if}
    {/if}
  </Box>
{/if}
```

Append to `styles.css`:

```css
.token-once {
  margin: 12px 0;
  padding: 12px;
  border: 1px solid var(--accent);
  border-radius: 6px;
  background: color-mix(in srgb, var(--accent) 6%, var(--bg-secondary));
}
.token-display {
  display: block;
  margin: 12px 0;
  padding: 12px;
  background: var(--bg-tertiary);
  color: var(--text-primary);
  font-family: var(--font-mono);
  font-size: 12px;
  word-break: break-all;
  border-radius: 4px;
  user-select: all;
}
```

- [ ] **Step 2: Mobile QA**

At 375px width, confirm:
- Buttons stack vertically
- Token wraps cleanly (no overflow)
- "Re-register" button is reachable without horizontal scroll

- [ ] **Step 3: Commit**

```bash
git add graywolf/web/src/routes/MapsSettings.svelte graywolf/web/src/lib/maps/styles.css
git commit -m "ui(maps): registered-state UI with token reveal/backup/re-register"
```

---

### Task 13: Build the source picker

**Files:**
- Modify: `graywolf/web/src/routes/MapsSettings.svelte`
- Create: `graywolf/web/src/lib/maps/state-list.js` (referenced for the disabled "Offline" radio hint)

- [ ] **Step 1: Add the state list helper**

```js
// graywolf/web/src/lib/maps/state-list.js
//
// 50 US states + DC, slugs match the maps.nw5w.com R2 layout
// (lowercase, hyphenated). Used disabled-only in Plan 1 (the offline
// radio is greyed out until Plan 2 lands the download UI), but the
// list lives here so Plan 2 picks it up without a follow-up move.

export const US_STATES = [
  { slug: 'alabama', name: 'Alabama' },
  { slug: 'alaska', name: 'Alaska' },
  { slug: 'arizona', name: 'Arizona' },
  { slug: 'arkansas', name: 'Arkansas' },
  { slug: 'california', name: 'California' },
  { slug: 'colorado', name: 'Colorado' },
  { slug: 'connecticut', name: 'Connecticut' },
  { slug: 'delaware', name: 'Delaware' },
  { slug: 'district-of-columbia', name: 'District of Columbia' },
  { slug: 'florida', name: 'Florida' },
  { slug: 'georgia', name: 'Georgia' },
  { slug: 'hawaii', name: 'Hawaii' },
  { slug: 'idaho', name: 'Idaho' },
  { slug: 'illinois', name: 'Illinois' },
  { slug: 'indiana', name: 'Indiana' },
  { slug: 'iowa', name: 'Iowa' },
  { slug: 'kansas', name: 'Kansas' },
  { slug: 'kentucky', name: 'Kentucky' },
  { slug: 'louisiana', name: 'Louisiana' },
  { slug: 'maine', name: 'Maine' },
  { slug: 'maryland', name: 'Maryland' },
  { slug: 'massachusetts', name: 'Massachusetts' },
  { slug: 'michigan', name: 'Michigan' },
  { slug: 'minnesota', name: 'Minnesota' },
  { slug: 'mississippi', name: 'Mississippi' },
  { slug: 'missouri', name: 'Missouri' },
  { slug: 'montana', name: 'Montana' },
  { slug: 'nebraska', name: 'Nebraska' },
  { slug: 'nevada', name: 'Nevada' },
  { slug: 'new-hampshire', name: 'New Hampshire' },
  { slug: 'new-jersey', name: 'New Jersey' },
  { slug: 'new-mexico', name: 'New Mexico' },
  { slug: 'new-york', name: 'New York' },
  { slug: 'north-carolina', name: 'North Carolina' },
  { slug: 'north-dakota', name: 'North Dakota' },
  { slug: 'ohio', name: 'Ohio' },
  { slug: 'oklahoma', name: 'Oklahoma' },
  { slug: 'oregon', name: 'Oregon' },
  { slug: 'pennsylvania', name: 'Pennsylvania' },
  { slug: 'rhode-island', name: 'Rhode Island' },
  { slug: 'south-carolina', name: 'South Carolina' },
  { slug: 'south-dakota', name: 'South Dakota' },
  { slug: 'tennessee', name: 'Tennessee' },
  { slug: 'texas', name: 'Texas' },
  { slug: 'utah', name: 'Utah' },
  { slug: 'vermont', name: 'Vermont' },
  { slug: 'virginia', name: 'Virginia' },
  { slug: 'washington', name: 'Washington' },
  { slug: 'west-virginia', name: 'West Virginia' },
  { slug: 'wisconsin', name: 'Wisconsin' },
  { slug: 'wyoming', name: 'Wyoming' },
];
```

- [ ] **Step 2: Add the source picker block**

```svelte
<script>
  // ...
  // Three-radio source picker. "Graywolf maps (offline)" is disabled in
  // Plan 1 because no PMTiles download exists yet; Plan 2 enables it.
  const sources = [
    { value: 'osm', label: 'OpenStreetMap public tiles', sublabel: 'Free, available everywhere, less polished cartography.' },
    { value: 'graywolf', label: 'Graywolf private maps (online)', sublabel: 'Polished cartography. Requires registration and an internet connection.' },
    { value: 'graywolf-offline', label: 'Graywolf private maps (offline)', sublabel: 'Coming soon — pre-downloaded state tiles.', disabled: true },
  ];

  function onSourceChange(v) {
    if (v === 'graywolf-offline') return;
    mapsState.setSource(v);
  }
</script>

<Box title="Map source">
  <fieldset class="radio-group">
    <legend class="visually-hidden">Choose a basemap source</legend>
    {#each sources as src}
      <label class="radio-row" class:disabled={src.disabled || (src.value === 'graywolf' && !mapsState.registered)}>
        <input
          type="radio"
          name="map-source"
          value={src.value}
          checked={mapsState.source === src.value}
          disabled={src.disabled || (src.value === 'graywolf' && !mapsState.registered)}
          onchange={(e) => onSourceChange(e.currentTarget.value)}
        />
        <span class="radio-text">
          <span class="radio-label">{src.label}</span>
          <span class="radio-sublabel">{src.sublabel}</span>
        </span>
      </label>
    {/each}
  </fieldset>
</Box>
```

Append to `styles.css`:

```css
.radio-group {
  border: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.radio-row {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 12px;
  min-height: 44px;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  cursor: pointer;
  transition: background 0.15s, border-color 0.15s;
}
.radio-row:hover:not(.disabled) {
  background: var(--bg-hover);
  border-color: var(--accent);
}
.radio-row.disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.radio-row input[type="radio"] {
  width: 20px;
  height: 20px;
  margin-top: 2px;
  accent-color: var(--accent);
  flex-shrink: 0;
}
.radio-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.radio-label {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
}
.radio-sublabel {
  font-size: 13px;
  color: var(--text-muted);
}
.visually-hidden {
  position: absolute;
  width: 1px;
  height: 1px;
  margin: -1px;
  padding: 0;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
```

- [ ] **Step 3: QA at 375px**

Verify each radio row is ≥44px tall, the touch target is the entire row (not just the dot), and the selected source survives a page reload (re-fetched from server).

- [ ] **Step 4: Commit**

```bash
git add graywolf/web/src/routes/MapsSettings.svelte graywolf/web/src/lib/maps/state-list.js graywolf/web/src/lib/maps/styles.css
git commit -m "ui(maps): add three-radio source picker"
```

---

### Task 14: Mobile pass on the Maps Settings page

**Files:**
- Modify: `graywolf/web/src/lib/maps/styles.css`
- Modify: `graywolf/web/src/routes/MapsSettings.svelte` if reordering needed

- [ ] **Step 1: Test at 320px / 375px / 768px / 1280px**

Run dev server. Resize through each breakpoint. Verify:
- No horizontal overflow at any width
- All buttons ≥44px tall
- Disclosure paragraphs read comfortably (line-height ≥1.5, font ≥14px)
- Token display wraps; copy button reachable
- Registered state's "Re-register" button doesn't get clipped

- [ ] **Step 2: Test on a real phone via local network**

Run `npm run dev -- --host`, open the URL on a phone (same Wi-Fi). Verify:
- Hamburger nav opens, "Maps" entry is tappable
- Form input doesn't auto-zoom on focus (font-size on inputs ≥16px to prevent iOS zoom)
- Touch targets feel right (no accidental misses)

If iOS auto-zoom occurs, append:

```css
.maps-row :global(input[type="text"]),
.maps-row :global(input[type="search"]) {
  font-size: 16px;
}
```

- [ ] **Step 3: Commit any tweaks**

```bash
git add graywolf/web/src/lib/maps/styles.css graywolf/web/src/routes/MapsSettings.svelte
git commit -m "ui(maps): mobile QA pass — hit targets, font sizes, overflow"
```

---

### Task 15: Verify the full backend roundtrip from the UI

**Files:** none

- [ ] **Step 1: Run the binary, open browser, walk through registration**

1. Start fresh DB: delete `<config-dir>/graywolf.db`, start `make run` (or `./bin/graywolf`)
2. Open `http://localhost:<port>/#/preferences/maps`
3. Confirm OSM is selected by default and "Graywolf private maps (online)" is greyed out
4. Tick consent, type your callsign, click Register
5. Verify the registered-state UI appears with the one-time token
6. Click "Download as file" — verify the .txt file contents
7. Switch source to "Graywolf private maps (online)" — confirm radio updates
8. Reload the page — confirm registration persists, source persists, token is hidden until "Show token" clicked

- [ ] **Step 2: Inspect the DB to confirm persistence**

Run: `sqlite3 <config-dir>/graywolf.db "SELECT source, callsign, length(token), registered_at FROM maps_configs;"`
Expected: `graywolf|N5XXX|43|<timestamp>`

- [ ] **Step 3: No commit (verification only)**

If anything's wrong, fix it as a follow-up commit referencing the failing scenario.

---

## Phase 3: MapLibre Infrastructure & Data Layer

### Task 16: Add MapLibre dependencies and theme tokens

**Files:**
- Modify: `graywolf/web/package.json`
- Modify: `graywolf/web/themes/graywolf.css`, `graywolf-night.css`, `grayscale.css`, `grayscale-night.css`

- [ ] **Step 1: Add the npm deps**

Run from `graywolf/web/`:
```bash
npm install --save maplibre-gl@^4.7.1 pmtiles@^3.2.0 @americana/maplibre-shield-generator@^0.7.0
```

- [ ] **Step 2: Add map overlay tokens to each theme file**

In each of the four theme CSS files, inside the `html[data-theme="..."]` block, append:

```css
/* Map UI tokens */
--map-overlay-bg: var(--bg-secondary);
--map-overlay-fg: var(--text-primary);
--map-overlay-muted: var(--text-muted);
--map-overlay-border: var(--border-color);
--map-overlay-shadow: 0 4px 16px rgba(0, 0, 0, 0.25);
--map-attribution-bg: color-mix(in srgb, var(--bg-secondary) 92%, transparent);
```

(For the night themes, the shadow opacity should be higher; tune per theme.)

- [ ] **Step 3: Verify build**

Run: `cd graywolf/web && npm run build`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add graywolf/web/package.json graywolf/web/package-lock.json graywolf/web/themes/
git commit -m "ui(map): add MapLibre/pmtiles deps and map overlay theme tokens"
```

---

### Task 17: Build the MapLibre map shell

**Files:**
- Create: `graywolf/web/src/lib/map/maplibre-map.svelte`

This is the renderer shell — it owns the `maplibregl.Map` instance, applies the right style based on `mapsState.source`, sets up `transformRequest` for the bearer token, registers the `pmtiles://` protocol, and exposes the map instance to children via context. Layers are added by sibling components in later tasks.

- [ ] **Step 1: Create the shell**

```svelte
<!-- graywolf/web/src/lib/map/maplibre-map.svelte -->
<script>
  import { onMount, onDestroy, setContext } from 'svelte';
  import maplibregl from 'maplibre-gl';
  import 'maplibre-gl/dist/maplibre-gl.css';
  import { Protocol } from 'pmtiles';
  import { mapsState } from '../settings/maps-store.svelte.js';
  import { osmRasterStyle } from './sources/osm-raster.js';
  import { graywolfVectorStyle } from './sources/graywolf-vector.js';

  let { initialCenter = [-98, 39], initialZoom = 4, oncreate = null } = $props();

  let container;
  let map = null;

  // Register the pmtiles:// protocol once. Plan 2 uses it for offline
  // PMTiles archives; safe to register now even though no offline file
  // is ever requested in Plan 1.
  if (!maplibregl.getProtocol?.('pmtiles')) {
    maplibregl.addProtocol('pmtiles', new Protocol().tile);
  }

  // Resolve the active style based on the user's source choice. We
  // re-create the map when the source changes; MapLibre supports
  // setStyle() for hot-swapping but we'd lose marker/source state and
  // the cleanup is trickier. Re-creating keeps the data flow simple.
  function buildStyle() {
    if (mapsState.source === 'graywolf' && mapsState.registered) {
      return graywolfVectorStyle();
    }
    return osmRasterStyle();
  }

  // transformRequest: attach Bearer token to maps.nw5w.com requests
  // EXCEPT /style/* (must stay anonymous to keep CF edge cache shared).
  function transformRequest(url) {
    if (url.startsWith('https://maps.nw5w.com/') &&
        !url.startsWith('https://maps.nw5w.com/style/') &&
        mapsState.tokenOnce) {
      return { url, headers: { Authorization: `Bearer ${mapsState.tokenOnce}` } };
    }
    // The persisted-token path: fetch from the cookie-authenticated
    // /api endpoint that proxies the token; but we want to keep the
    // bearer in the browser to avoid round-tripping through Go for
    // every tile. So persist it into a closure-scoped variable that
    // gets updated on fetchConfig() — see TokenSync below.
    if (url.startsWith('https://maps.nw5w.com/') &&
        !url.startsWith('https://maps.nw5w.com/style/') &&
        bearerToken) {
      return { url, headers: { Authorization: `Bearer ${bearerToken}` } };
    }
    return { url };
  }

  // Local mirror of the bearer token (since mapsState.tokenOnce is
  // null after a page reload). Refreshed via revealToken().
  let bearerToken = $state(null);

  async function syncToken() {
    if (mapsState.registered && !bearerToken) {
      bearerToken = await mapsState.revealToken();
    } else if (!mapsState.registered) {
      bearerToken = null;
    }
  }

  onMount(async () => {
    await syncToken();
    map = new maplibregl.Map({
      container,
      style: buildStyle(),
      center: initialCenter,
      zoom: initialZoom,
      attributionControl: { compact: true },
      transformRequest,
      cooperativeGestures: false, // we want pan/pinch to work without modifier on mobile
    });
    map.addControl(new maplibregl.NavigationControl({ showCompass: false }), 'top-right');
    map.addControl(new maplibregl.ScaleControl({ maxWidth: 100, unit: 'imperial' }), 'bottom-left');
    setContext('maplibre-map', { getMap: () => map });
    map.once('load', () => oncreate?.(map));
  });

  // Watch the source choice and rebuild on change.
  $effect(() => {
    const _ = mapsState.source; // track
    const __ = mapsState.registered;
    if (!map) return;
    map.setStyle(buildStyle());
  });

  // Watch token availability — if it appears after first render
  // (e.g. user just registered), refresh the style so authed
  // tile requests succeed.
  $effect(() => {
    syncToken();
  });

  onDestroy(() => {
    map?.remove();
    map = null;
  });
</script>

<div bind:this={container} class="map-container" role="application" aria-label="Map">
</div>

<style>
  .map-container {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    background: var(--bg-secondary);
  }
  /* MapLibre sets its own positioning on canvas; just ensure the
     container is the offset parent. */
  :global(.maplibregl-ctrl-attrib) {
    background: var(--map-attribution-bg) !important;
    color: var(--map-overlay-fg) !important;
    font-size: 11px;
  }
</style>
```

- [ ] **Step 2: Create the source factories**

`graywolf/web/src/lib/map/sources/osm-raster.js`:

```js
// OSM public raster tiles wrapped in a MapLibre style. Used as the
// default basemap and as a fallback when the user hasn't registered
// for private maps. Tiles come straight from the OSM tile servers,
// same URL Leaflet was hitting; no API key required.
export function osmRasterStyle() {
  return {
    version: 8,
    sources: {
      osm: {
        type: 'raster',
        tiles: [
          'https://a.tile.openstreetmap.org/{z}/{x}/{y}.png',
          'https://b.tile.openstreetmap.org/{z}/{x}/{y}.png',
          'https://c.tile.openstreetmap.org/{z}/{x}/{y}.png',
        ],
        tileSize: 256,
        maxzoom: 19,
        attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
      },
    },
    layers: [
      { id: 'osm', type: 'raster', source: 'osm' },
    ],
  };
}
```

`graywolf/web/src/lib/map/sources/graywolf-vector.js`:

```js
// Private GW vector tiles via maps.nw5w.com. Returns the URL of the
// shared style.json (americana-roboto, the project default). Auth is
// handled in the Map component's transformRequest.
export function graywolfVectorStyle() {
  return 'https://maps.nw5w.com/style/americana-roboto/style.json';
}
```

- [ ] **Step 3: Smoke test by mounting the shell on a temp page**

Add a temp route (e.g. `/_map-test`) with `<MaplibreMap />` filling the viewport. Open in browser, verify:
- OSM tiles load (default state)
- After registration + setting source to "graywolf", style swap loads the americana-roboto style
- Network tab shows the `Authorization: Bearer ...` header on `/{z}/{x}/{y}.mvt` and `/tiles.json`, NOT on `/style/*`

- [ ] **Step 4: Remove the temp route, commit**

```bash
git add graywolf/web/src/lib/map/maplibre-map.svelte graywolf/web/src/lib/map/sources/
git commit -m "ui(map): MapLibre map shell with source switching and bearer transformRequest"
```

---

### Task 18: Extract the data-store from current `LiveMap.svelte`

**Files:**
- Create: `graywolf/web/src/lib/map/data-store.svelte.js`

The current Live Map mixes rendering and data-fetching in one 856-line component. Extract the polling/ETag/since-cursor/visibility logic into a renderer-agnostic store that emits reactive `stations`, `trails`, `weather`, `myPosition` collections. The new `LiveMapV2.svelte` consumes this store; the old Leaflet view also could (but won't, since we delete it at cutover).

- [ ] **Step 1: Identify the boundaries to extract**

Read `graywolf/web/src/routes/LiveMap.svelte` (search for `fetch('/api/stations`, `etag`, `since-cursor`, `visibilityState`, `setInterval`, `setTimeout` to find the polling block). Note the existing data shape (the keys returned by `/api/stations`).

- [ ] **Step 2: Implement the store**

```js
// graywolf/web/src/lib/map/data-store.svelte.js
//
// Renderer-agnostic data layer for the Live Map. Owns the polling
// loop, ETag-based caching, since-cursor delta updates, visibility-
// aware backoff. Emits reactive collections of stations / trails /
// weather / my-position that any renderer (Leaflet, MapLibre) can
// subscribe to.
//
// The current LiveMap.svelte inlines this logic; we extract it both
// to keep LiveMapV2 small and to make the polling testable in
// isolation.

export const liveMapData = (() => {
  let stations = $state(new Map());        // callsign -> station object
  let trails   = $state(new Map());        // callsign -> array of [lng, lat, t]
  let weather  = $state(new Map());        // callsign -> weather payload
  let myPosition = $state(null);
  let lastFetchAt = $state(null);
  let pollingState = $state('idle');       // idle | polling | error
  let timerangeMs = $state(60 * 60 * 1000); // 1h default; user-controlled

  let etag = null;
  let sinceCursor = null;
  let bounds = null;            // [[w,s],[e,n]]
  let pollHandle = null;
  let backoffMs = 5000;          // start at 5s, exp backoff to 60s on error

  function setBounds(b) {
    if (!b) return;
    const changed = !bounds ||
      b[0][0] !== bounds[0][0] || b[0][1] !== bounds[0][1] ||
      b[1][0] !== bounds[1][0] || b[1][1] !== bounds[1][1];
    bounds = b;
    if (changed) {
      // Bounds changed → invalidate cursor; full reload next cycle
      sinceCursor = null;
      etag = null;
    }
  }

  function setTimerange(ms) {
    timerangeMs = ms;
    sinceCursor = null;
    etag = null;
  }

  async function fetchOnce() {
    if (!bounds) return;
    pollingState = 'polling';
    const params = new URLSearchParams();
    params.set('bbox', `${bounds[0][0]},${bounds[0][1]},${bounds[1][0]},${bounds[1][1]}`);
    params.set('timerange', String(Math.round(timerangeMs / 1000)));
    params.set('include', 'weather');
    if (sinceCursor) params.set('since', sinceCursor);

    try {
      const res = await fetch(`/api/stations?${params}`, {
        credentials: 'same-origin',
        headers: etag ? { 'If-None-Match': etag } : {},
      });
      if (res.status === 304) {
        pollingState = 'idle';
        backoffMs = 5000;
        return;
      }
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      etag = res.headers.get('etag') || etag;
      const body = await res.json();
      sinceCursor = body.cursor ?? null;
      applyDelta(body, !params.has('since'));
      lastFetchAt = new Date();
      pollingState = 'idle';
      backoffMs = 5000;
    } catch (e) {
      pollingState = 'error';
      backoffMs = Math.min(backoffMs * 2, 60_000);
    }
  }

  function applyDelta(body, fullReplace) {
    if (fullReplace) {
      stations = new Map();
      trails = new Map();
      weather = new Map();
    }
    for (const s of body.stations ?? []) {
      stations.set(s.callsign, s);
      // Append to trail if new position
      const t = trails.get(s.callsign) ?? [];
      t.push([s.lon, s.lat, s.last_heard]);
      if (t.length > 200) t.shift();
      trails.set(s.callsign, t);
      if (s.weather) weather.set(s.callsign, s.weather);
    }
    for (const callsign of body.removed ?? []) {
      stations.delete(callsign);
      trails.delete(callsign);
      weather.delete(callsign);
    }
    if (body.my_position) myPosition = body.my_position;
    // Trigger reactivity for Maps (Svelte 5)
    stations = new Map(stations);
    trails = new Map(trails);
    weather = new Map(weather);
  }

  function start() {
    stop();
    const tick = async () => {
      if (document.visibilityState === 'visible') await fetchOnce();
      pollHandle = setTimeout(tick, backoffMs);
    };
    tick();
  }

  function stop() {
    if (pollHandle) {
      clearTimeout(pollHandle);
      pollHandle = null;
    }
  }

  return {
    get stations() { return stations; },
    get trails() { return trails; },
    get weather() { return weather; },
    get myPosition() { return myPosition; },
    get lastFetchAt() { return lastFetchAt; },
    get pollingState() { return pollingState; },
    get timerangeMs() { return timerangeMs; },

    setBounds,
    setTimerange,
    start,
    stop,
  };
})();
```

NOTE: **read the actual current Live Map** before writing this and reconcile the request/response shape. The above is a sketch — the exact field names (`bbox`, `since`, `cursor`, `removed`, `my_position`, etc.) must match the existing `/api/stations` contract verbatim. Do not invent fields.

- [ ] **Step 3: Build & confirm**

Run: `cd graywolf/web && npm run build`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add graywolf/web/src/lib/map/data-store.svelte.js
git commit -m "ui(map): extract polling/ETag data layer to renderer-agnostic store"
```

---

### Task 19: Build the APRS icon spritesheet

**Files:**
- Create: `graywolf/web/src/lib/map/aprs-icon-spritesheet.js`

Leaflet rendered each station as a DOM marker with an HTML icon class. MapLibre symbol layers want a sprite — an image atlas keyed by name. We generate one at runtime by drawing each APRS symbol onto a canvas, then `map.addImage(name, ImageData)`.

- [ ] **Step 1: Read the existing icon mapping**

Open `graywolf/web/src/lib/map/aprs-icons.js` and list each unique APRS symbol the existing UI handles (the mapping from `(table, symbol)` to icon name).

- [ ] **Step 2: Implement the generator**

```js
// graywolf/web/src/lib/map/aprs-icon-spritesheet.js
//
// At map-load time, generate a sprite image per APRS symbol code so
// the symbol layer can reference it via icon-image. We do this in JS
// rather than shipping a static sprite atlas because the current
// Leaflet implementation generates icons via CSS classes — we want
// pixel-identical output without re-authoring the artwork.

import { iconClassFor } from './aprs-icons.js'; // (or whatever the legacy helper exports)

const ICON_SIZE = 28;

// Renders a single icon to an offscreen canvas → ImageBitmap.
// The trick: temporarily attach a styled div to the document, wait
// for layout, then html2canvas-style capture isn't available without
// a heavy dep. Simpler approach: for each unique icon, emit an
// SVG that mirrors the legacy CSS (which the legacy code likely
// loaded as a sprite already — confirm during Step 1) and rasterize
// via Image+canvas.

export async function loadAprsSprites(map) {
  // Discover the set of (table, symbol) pairs by reading the legacy
  // icon CSS or a lookup table. For each, render to a 28×28 canvas,
  // then call:
  //   map.addImage(`aprs-${table}-${symbol}`, imageData);
  //
  // The symbol layer then references the image via:
  //   layout: { 'icon-image': ['concat', 'aprs-', ['get', 'table'], '-', ['get', 'symbol']] }
}
```

NOTE: this task's *implementation* depends on what `aprs-icons.js` actually does today. **Read it first**, then write the simplest path. If the legacy icons are already PNGs in the repo, just `map.loadImage()` each one in a loop — no canvas rendering needed.

- [ ] **Step 3: Write a quick visual test**

Mount a temp `<MaplibreMap oncreate={loadAprsSprites} />` with a hardcoded GeoJSON of one feature per symbol. Confirm icons render at the right positions and sizes.

- [ ] **Step 4: Commit**

```bash
git add graywolf/web/src/lib/map/aprs-icon-spritesheet.js
git commit -m "ui(map): runtime APRS icon sprite loader for MapLibre"
```

---

## Phase 4: Live Map Feature Parity

The remaining tasks port one feature each from `LiveMap.svelte` to `LiveMapV2.svelte` using MapLibre primitives. **Don't try to land all of these in one go** — each task ends with a build-and-eyeball-it gate.

### Task 20: Stations layer — markers + callsign labels

**Files:**
- Create: `graywolf/web/src/lib/map/layers/stations.js`
- Create: `graywolf/web/src/routes/LiveMapV2.svelte` (initial scaffold)

- [ ] **Step 1: Scaffold LiveMapV2**

```svelte
<!-- graywolf/web/src/routes/LiveMapV2.svelte -->
<script>
  import { onMount } from 'svelte';
  import MaplibreMap from '../lib/map/maplibre-map.svelte';
  import { liveMapData } from '../lib/map/data-store.svelte.js';
  import { mountStationsLayer } from '../lib/map/layers/stations.js';
  import { loadAprsSprites } from '../lib/map/aprs-icon-spritesheet.js';

  let map = null;
  let stationsCleanup = null;

  async function onMapReady(m) {
    map = m;
    await loadAprsSprites(m);
    stationsCleanup = mountStationsLayer(m, () => liveMapData.stations);
    // Bounds tracking for the data store
    const updateBounds = () => {
      const b = m.getBounds();
      liveMapData.setBounds([[b.getWest(), b.getSouth()], [b.getEast(), b.getNorth()]]);
    };
    m.on('moveend', updateBounds);
    updateBounds();
    liveMapData.start();
  }

  onMount(() => () => {
    liveMapData.stop();
    stationsCleanup?.();
  });
</script>

<div class="livemap-shell">
  <MaplibreMap oncreate={onMapReady} />
</div>

<style>
  .livemap-shell {
    position: absolute;
    inset: 0;
    overflow: hidden;
  }
</style>
```

- [ ] **Step 2: Implement the stations layer**

```js
// graywolf/web/src/lib/map/layers/stations.js
//
// Stations render as a MapLibre symbol layer backed by a GeoJSON
// source. The symbol layer references icons added by
// loadAprsSprites(); the text-field renders the callsign label
// underneath. High-contrast mode is a CSS-toggleable text-color
// property.

const SOURCE_ID = 'gw-stations';
const ICON_LAYER_ID = 'gw-stations-icons';
const LABEL_LAYER_ID = 'gw-stations-labels';

export function mountStationsLayer(map, getStations) {
  if (!map.getSource(SOURCE_ID)) {
    map.addSource(SOURCE_ID, {
      type: 'geojson',
      data: { type: 'FeatureCollection', features: [] },
    });
  }
  if (!map.getLayer(ICON_LAYER_ID)) {
    map.addLayer({
      id: ICON_LAYER_ID,
      type: 'symbol',
      source: SOURCE_ID,
      layout: {
        'icon-image': ['concat', 'aprs-', ['get', 'table'], '-', ['get', 'symbol']],
        'icon-size': 1,
        'icon-allow-overlap': true,
        'icon-ignore-placement': true,
      },
    });
  }
  if (!map.getLayer(LABEL_LAYER_ID)) {
    map.addLayer({
      id: LABEL_LAYER_ID,
      type: 'symbol',
      source: SOURCE_ID,
      layout: {
        'text-field': ['get', 'callsign'],
        'text-offset': [0, 1.4],
        'text-anchor': 'top',
        'text-size': 11,
        'text-font': ['Roboto Regular', 'Noto Sans Regular'],
        'text-allow-overlap': false,
      },
      paint: {
        'text-color': 'var(--text-primary)', // overridden via CSS variables in MapLibre 4+
        'text-halo-color': 'rgba(0,0,0,0.7)',
        'text-halo-width': 1.2,
      },
    });
  }

  function refresh() {
    const features = [];
    for (const [callsign, s] of getStations()) {
      features.push({
        type: 'Feature',
        geometry: { type: 'Point', coordinates: [s.lon, s.lat] },
        properties: {
          callsign,
          table: s.symbol_table ?? '/',
          symbol: s.symbol ?? '`',
        },
      });
    }
    map.getSource(SOURCE_ID).setData({ type: 'FeatureCollection', features });
  }

  // Rerun on data changes via Svelte's $effect from the caller.
  // We expose a tick() that the host component triggers; alternative
  // is wiring an effect here, but stations.js stays framework-free
  // by accepting a getter and exposing refresh().
  refresh();
  const interval = setInterval(refresh, 1000); // simple fallback
  return () => {
    clearInterval(interval);
    if (map.getLayer(LABEL_LAYER_ID)) map.removeLayer(LABEL_LAYER_ID);
    if (map.getLayer(ICON_LAYER_ID)) map.removeLayer(ICON_LAYER_ID);
    if (map.getSource(SOURCE_ID)) map.removeSource(SOURCE_ID);
  };
}
```

(Replace the `setInterval` poll with an `$effect` from the host component on cleanup — the simplest version uses interval; refactor to reactive update once the rest of the layers land.)

- [ ] **Step 3: Eyeball it**

Run dev server, navigate to `/map` (still pointed at old LiveMap; switch the route in `App.svelte` temporarily for this test), confirm stations appear with icons and labels.

- [ ] **Step 4: Commit**

```bash
git add graywolf/web/src/lib/map/layers/stations.js graywolf/web/src/routes/LiveMapV2.svelte
git commit -m "ui(map): port stations layer to MapLibre symbols"
```

---

### Task 21: Trails layer

**Files:**
- Create: `graywolf/web/src/lib/map/layers/trails.js`
- Modify: `graywolf/web/src/routes/LiveMapV2.svelte` to mount it

- [ ] **Step 1: Implement**

```js
// graywolf/web/src/lib/map/layers/trails.js
import { liveMapData } from '../data-store.svelte.js';

const SOURCE_ID = 'gw-trails';
const LAYER_ID = 'gw-trails-line';

export function mountTrailsLayer(map, getTrails) {
  if (!map.getSource(SOURCE_ID)) {
    map.addSource(SOURCE_ID, {
      type: 'geojson',
      data: { type: 'FeatureCollection', features: [] },
    });
  }
  if (!map.getLayer(LAYER_ID)) {
    map.addLayer({
      id: LAYER_ID,
      type: 'line',
      source: SOURCE_ID,
      paint: {
        'line-color': '#3fb950',
        'line-width': 2,
        'line-opacity': 0.65,
      },
    }, 'gw-stations-icons'); // place beneath station icons
  }
  function refresh() {
    const features = [];
    for (const [callsign, points] of getTrails()) {
      if (points.length < 2) continue;
      features.push({
        type: 'Feature',
        geometry: { type: 'LineString', coordinates: points.map((p) => [p[0], p[1]]) },
        properties: { callsign },
      });
    }
    map.getSource(SOURCE_ID).setData({ type: 'FeatureCollection', features });
  }
  refresh();
  const interval = setInterval(refresh, 1000);
  return () => {
    clearInterval(interval);
    if (map.getLayer(LAYER_ID)) map.removeLayer(LAYER_ID);
    if (map.getSource(SOURCE_ID)) map.removeSource(SOURCE_ID);
  };
}
```

- [ ] **Step 2: Mount it from LiveMapV2**

Add to the `onMapReady` body:
```js
import { mountTrailsLayer } from '../lib/map/layers/trails.js';
let trailsCleanup = mountTrailsLayer(m, () => liveMapData.trails);
```
And in cleanup: `trailsCleanup?.();`

- [ ] **Step 3: Eyeball — drive a station around (or wait for movement) and confirm trail line draws**

- [ ] **Step 4: Commit**

```bash
git add graywolf/web/src/lib/map/layers/trails.js graywolf/web/src/routes/LiveMapV2.svelte
git commit -m "ui(map): port station trails to MapLibre line layer"
```

---

### Task 22: Weather overlay layer

**Files:**
- Create: `graywolf/web/src/lib/map/layers/weather.js`

- [ ] **Step 1: Read the legacy weather rendering**

Open `graywolf/web/src/lib/map/weather-layer.js` (81 lines). Note exactly what gets drawn — likely a coloured circle / wind-barb / temperature label per station.

- [ ] **Step 2: Re-implement using a symbol layer (for labels) and a circle layer (for wx markers)**

Mirror the legacy semantics. Source = GeoJSON of stations that have `weather` populated.

- [ ] **Step 3: Mount from LiveMapV2 + visual QA**

- [ ] **Step 4: Commit**

```bash
git add graywolf/web/src/lib/map/layers/weather.js graywolf/web/src/routes/LiveMapV2.svelte
git commit -m "ui(map): port weather overlay to MapLibre layers"
```

---

### Task 23: Hover-path layer (digi paths)

**Files:**
- Create: `graywolf/web/src/lib/map/layers/hover-path.js`

The legacy implementation draws a thick green line through the digipeater path on station hover. Port to a one-feature GeoJSON line that's repopulated on `mouseenter` and cleared on `mouseleave`.

- [ ] **Step 1: Implement**

```js
const SOURCE_ID = 'gw-hover-path';
const LAYER_ID = 'gw-hover-path-line';

export function mountHoverPathLayer(map, getStation) {
  map.addSource(SOURCE_ID, {
    type: 'geojson',
    data: { type: 'FeatureCollection', features: [] },
  });
  map.addLayer({
    id: LAYER_ID,
    type: 'line',
    source: SOURCE_ID,
    paint: {
      'line-color': '#3fb950',
      'line-width': 3,
      'line-opacity': 0.85,
    },
  });
  map.on('mouseenter', 'gw-stations-icons', (e) => {
    const f = e.features?.[0];
    if (!f) return;
    map.getCanvas().style.cursor = 'pointer';
    const station = getStation(f.properties.callsign);
    if (!station?.path_coords) return;
    map.getSource(SOURCE_ID).setData({
      type: 'FeatureCollection',
      features: [{
        type: 'Feature',
        geometry: { type: 'LineString', coordinates: station.path_coords },
      }],
    });
  });
  map.on('mouseleave', 'gw-stations-icons', () => {
    map.getCanvas().style.cursor = '';
    map.getSource(SOURCE_ID).setData({ type: 'FeatureCollection', features: [] });
  });
  return () => {
    if (map.getLayer(LAYER_ID)) map.removeLayer(LAYER_ID);
    if (map.getSource(SOURCE_ID)) map.removeSource(SOURCE_ID);
  };
}
```

- [ ] **Step 2: Mount + eyeball**

- [ ] **Step 3: Commit**

```bash
git add graywolf/web/src/lib/map/layers/hover-path.js graywolf/web/src/routes/LiveMapV2.svelte
git commit -m "ui(map): port hover digi-path layer to MapLibre"
```

---

### Task 24: Click-to-popup for stations

**Files:**
- Modify: `graywolf/web/src/routes/LiveMapV2.svelte`

The existing Leaflet popup shows callsign / last heard / path / position. Use `maplibregl.Popup` for parity. Make the popup itself touch-friendly: minimum width 240px, close button ≥44px, content padded.

- [ ] **Step 1: Wire the click handler**

In `onMapReady`:
```js
import maplibregl from 'maplibre-gl';
m.on('click', 'gw-stations-icons', (e) => {
  const f = e.features?.[0];
  if (!f) return;
  const s = liveMapData.stations.get(f.properties.callsign);
  if (!s) return;
  new maplibregl.Popup({ offset: 18, maxWidth: '320px', className: 'gw-station-popup' })
    .setLngLat([s.lon, s.lat])
    .setHTML(renderPopupHTML(s))
    .addTo(m);
});
```

- [ ] **Step 2: Implement `renderPopupHTML(s)`**

Mirror the legacy `popup-helpers.js` output (HTML escape, format last-heard, display path). Style via `:global(.gw-station-popup .maplibregl-popup-content)` rules using the `--map-overlay-*` tokens.

- [ ] **Step 3: Commit**

```bash
git add graywolf/web/src/routes/LiveMapV2.svelte graywolf/web/src/lib/map/popup.js
git commit -m "ui(map): port station click-popups to MapLibre"
```

---

### Task 25: My-position marker (DOM marker)

**Files:**
- Create: `graywolf/web/src/lib/map/layers/my-position.js`

A symbol layer can't easily host the rich tooltip + own-beacon-path hover behavior the existing UI shows. Use `maplibregl.Marker` with a custom HTMLElement.

- [ ] **Step 1: Implement using `maplibregl.Marker`**

```js
import maplibregl from 'maplibre-gl';

export function mountMyPosition(map, getMyPosition) {
  let marker = null;
  function refresh() {
    const p = getMyPosition();
    if (!p) {
      marker?.remove(); marker = null;
      return;
    }
    if (!marker) {
      const el = document.createElement('div');
      el.className = 'gw-my-position';
      el.title = `My station: ${p.callsign}`;
      marker = new maplibregl.Marker({ element: el }).setLngLat([p.lon, p.lat]).addTo(map);
    } else {
      marker.setLngLat([p.lon, p.lat]);
    }
  }
  refresh();
  const interval = setInterval(refresh, 1000);
  return () => { clearInterval(interval); marker?.remove(); };
}
```

Style `.gw-my-position` to match the existing own-position visual (read `LiveMap.svelte`'s `.own-position-marker` block).

- [ ] **Step 2: Commit**

```bash
git add graywolf/web/src/lib/map/layers/my-position.js graywolf/web/src/routes/LiveMapV2.svelte
git commit -m "ui(map): port my-position marker to MapLibre DOM marker"
```

---

### Task 26: Layer panel — desktop floating card / mobile bottom sheet

**Files:**
- Create: `graywolf/web/src/lib/map/info-panel.svelte` (the reusable scaffold)
- Modify: `graywolf/web/src/routes/LiveMapV2.svelte`

The existing `.map-layer-control` and `.layer-panel` blocks become a reusable component. On desktop it's a floating card top-right. On mobile (≤768px) it's a bottom sheet (slides up from the bottom edge).

- [ ] **Step 1: Build the InfoPanel scaffold**

```svelte
<!-- graywolf/web/src/lib/map/info-panel.svelte -->
<script>
  import { Drawer } from '@chrissnell/chonky-ui';

  let { title, open = $bindable(false), anchor = 'right', children } = $props();

  let isMobile = $state(false);
  $effect(() => {
    const mq = window.matchMedia('(max-width: 768px)');
    isMobile = mq.matches;
    const handler = (e) => (isMobile = e.matches);
    mq.addEventListener('change', handler);
    return () => mq.removeEventListener('change', handler);
  });
</script>

{#if isMobile}
  <Drawer bind:open anchor="bottom">
    <Drawer.Header>{title}</Drawer.Header>
    <Drawer.Body>{@render children()}</Drawer.Body>
  </Drawer>
{:else if open}
  <aside class="info-panel" data-anchor={anchor} aria-label={title}>
    <header class="info-panel-header">
      <h2>{title}</h2>
      <button type="button" class="info-panel-close" onclick={() => (open = false)} aria-label="Close panel">×</button>
    </header>
    <div class="info-panel-body">{@render children()}</div>
  </aside>
{/if}

<style>
  .info-panel {
    position: absolute;
    top: 12px;
    right: 12px;
    width: 280px;
    background: var(--map-overlay-bg);
    color: var(--map-overlay-fg);
    border: 1px solid var(--map-overlay-border);
    border-radius: 8px;
    box-shadow: var(--map-overlay-shadow);
    z-index: 50;
  }
  .info-panel[data-anchor="left"]  { right: auto; left: 12px; }
  .info-panel-header {
    display: flex; align-items: center; justify-content: space-between;
    padding: 10px 12px; border-bottom: 1px solid var(--map-overlay-border);
  }
  .info-panel-header h2 { margin: 0; font-size: 13px; font-weight: 700; text-transform: uppercase; letter-spacing: 1px; }
  .info-panel-close {
    background: transparent; border: none; color: var(--map-overlay-fg);
    width: 36px; height: 36px; cursor: pointer; font-size: 22px; line-height: 1;
  }
  .info-panel-body { padding: 12px; max-height: 60vh; overflow-y: auto; }
</style>
```

- [ ] **Step 2: Use it for the layer toggles in LiveMapV2**

Add a FAB top-right that opens the layer panel; inside, render the existing layer toggles (Stations, High-contrast labels, APRS-IS, Trails, Weather, My Position) as `<Toggle>` rows. Each toggle calls `m.setLayoutProperty(layerId, 'visibility', v ? 'visible' : 'none')`.

- [ ] **Step 3: Mobile QA — drawer slides up cleanly, hit targets ≥44px**

- [ ] **Step 4: Commit**

```bash
git add graywolf/web/src/lib/map/info-panel.svelte graywolf/web/src/routes/LiveMapV2.svelte
git commit -m "ui(map): collapsible info-panel scaffold (desktop card / mobile sheet)"
```

---

### Task 27: Time-range selector (extended to 7 days)

**Files:**
- Modify: `graywolf/web/src/routes/LiveMapV2.svelte`

The existing dropdown offers 1h, 2h, 4h, 8h, 12h, 1d. Per Chris: extend up to 7 days.

- [ ] **Step 1: Define the option set**

```js
const TIMERANGES = [
  { ms: 1 * 3600_000,     label: '1 hour' },
  { ms: 2 * 3600_000,     label: '2 hours' },
  { ms: 4 * 3600_000,     label: '4 hours' },
  { ms: 8 * 3600_000,     label: '8 hours' },
  { ms: 12 * 3600_000,    label: '12 hours' },
  { ms: 24 * 3600_000,    label: '1 day' },
  { ms: 2 * 86400_000,    label: '2 days' },
  { ms: 4 * 86400_000,    label: '4 days' },
  { ms: 7 * 86400_000,    label: '7 days' },
];
```

- [ ] **Step 2: Render via Chonky `Select`**

Place the selector as a docked control top-right (or inside the layer panel — match the legacy layout). On mobile, a native `<select>` is fine and respects iOS conventions.

- [ ] **Step 3: Wire to data store**

```js
function onTimerangeChange(ms) { liveMapData.setTimerange(ms); }
```

- [ ] **Step 4: Verify the longer ranges actually return data**

Switch to "7 days" — confirm the network request includes `timerange=604800` and the map repopulates.

- [ ] **Step 5: Commit**

```bash
git add graywolf/web/src/routes/LiveMapV2.svelte
git commit -m "ui(map): extend time-range selector to 7 days"
```

---

### Task 28: Coord display + status bar

**Files:**
- Modify: `graywolf/web/src/routes/LiveMapV2.svelte`

The legacy coord display sits bottom-right and shows lat/lon + Maidenhead under the cursor. The status bar shows polling state, station count, timerange, and last-fetch-ago.

- [ ] **Step 1: Port coord display**

```svelte
<div class="map-coord-display" aria-live="off">
  {coords.lat}, {coords.lon}
  <span class="map-coord-grid">{coords.grid}</span>
</div>
```

Wire `coords` from `m.on('mousemove', ...)`. Reuse the legacy `lat-lon-to-grid.js` helper if one exists; if not, port it.

- [ ] **Step 2: Port status bar**

Bottom-left or bottom-center bar showing:
- `liveMapData.pollingState` icon (idle dot / spinner / red dot)
- `liveMapData.stations.size` station count
- "1 hour" current range
- "5 sec ago" relative time from `liveMapData.lastFetchAt`

On mobile, collapse to icons only; expand on tap.

- [ ] **Step 3: Commit**

```bash
git add graywolf/web/src/routes/LiveMapV2.svelte
git commit -m "ui(map): port coord display and status bar to LiveMapV2"
```

---

## Phase 5: Cutover & Cleanup

### Task 29: Switch the `/map` route to LiveMapV2

**Files:**
- Modify: `graywolf/web/src/App.svelte`

- [ ] **Step 1: Repoint the route**

In the routes object, change `'/map': LiveMap` to `'/map': LiveMapV2`. Remove the `LiveMap` import.

- [ ] **Step 2: Manual full-page QA**

Run dev server, exercise every feature: pan, zoom, click marker, hover marker, toggle layers, change timerange, switch source between OSM and graywolf, register/deregister.

- [ ] **Step 3: Commit**

```bash
git add graywolf/web/src/App.svelte
git commit -m "ui(map): cutover /map to MapLibre LiveMapV2"
```

---

### Task 30: Remove Leaflet code

**Files:**
- Delete: `graywolf/web/src/routes/LiveMap.svelte`
- Delete: `graywolf/web/src/lib/map/station-layer.js`, `trail-layer.js`, `weather-layer.js`, `aprs-icons.js`
- Modify: `graywolf/web/package.json` — remove `leaflet`

- [ ] **Step 1: Delete legacy files**

```bash
rm graywolf/web/src/routes/LiveMap.svelte
rm graywolf/web/src/lib/map/station-layer.js
rm graywolf/web/src/lib/map/trail-layer.js
rm graywolf/web/src/lib/map/weather-layer.js
rm graywolf/web/src/lib/map/aprs-icons.js
```

- [ ] **Step 2: Remove the leaflet npm dep**

```bash
cd graywolf/web && npm uninstall leaflet
```

Remove any `import 'leaflet/dist/leaflet.css'` lines that turn up in `grep -r leaflet graywolf/web/src/`.

- [ ] **Step 3: Build & verify**

```bash
cd graywolf/web && npm run build
```
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add -u graywolf/web/
git commit -m "ui(map): remove Leaflet code and dependency"
```

---

### Task 31: Mobile polish + theme verification + final QA

**Files:** any

- [ ] **Step 1: Walk every theme**

Switch through graywolf, graywolf-night, grayscale, grayscale-night. Verify:
- Map overlay panels read correctly in each theme
- Attribution control text is readable
- Popup styling honors theme tokens
- Status bar / coord display visible against each map background

- [ ] **Step 2: Mobile end-to-end pass**

On a real phone (or DevTools mobile emulation):
- Tab to /map; pan/pinch zoom works
- Tap a marker — popup opens, close button reachable
- Open layer panel (FAB → bottom sheet); toggles work
- Open time-range selector; works as native iOS picker
- Switch to /preferences/maps; complete a full registration → re-register → source switch flow
- Rotate device portrait/landscape — no layout breaks

- [ ] **Step 3: Run the full Go test suite**

```bash
cd graywolf && go test ./...
```
Expected: all PASS.

- [ ] **Step 4: Run the web build**

```bash
cd graywolf/web && npm run build
```
Expected: clean, bundle size sanity-checked (the MapLibre add will be ~600KB minified — acceptable; Leaflet was ~150KB).

- [ ] **Step 5: Commit any remaining tweaks**

```bash
git commit -am "ui(map): final mobile/theme polish for MapLibre cutover"
```

---

## Out of scope (deferred to Plan 2)

- Per-state PMTiles downloads (UI + backend streaming + progress + on-disk cache management)
- The "Graywolf private maps (offline)" radio option is wired but always disabled in Plan 1
- Auto-detection of offline coverage (use offline tiles where viewport intersects a downloaded state, fall back to online)
- Force token rotation (operator-assisted, admin-console-only — no Graywolf UI)
- Self-service usage stats (`GET /api/me` on the tile worker — not implemented server-side)

## Self-review checklist (for the implementer)

- [ ] Every task has actual code, not "implement X here"
- [ ] Every task ends with a verification step + a commit
- [ ] Mobile-first design principles honored on every UI surface
- [ ] All registration error codes (`bad_request`, `invalid_callsign`, `blocked`, `device_limit_reached`, `rate_limited`, `internal`) surface the verbatim server `message` plus a link to https://github.com/chrissnell/graywolf/issues
- [ ] `transformRequest` attaches Bearer token to `maps.nw5w.com` requests EXCEPT `/style/*`
- [ ] PMTiles protocol is registered up front (Plan 2 doesn't have to add it)
- [ ] `--tile-cache-dir` flag exists and the directory is created at startup (Plan 2 just writes into it)
- [ ] Token is stored in SQLite singleton row (per Chris's call); DB file gets `chmod 0o600`
- [ ] Time-range dropdown extends to 7 days
- [ ] No partial implementations; every started feature lands working
- [ ] Old Leaflet code is fully removed at cutover, not left as dead code
