# Maps Offline Downloads & Auto-Detection Implementation Plan (Plan 2 of 2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Layer offline state downloads on top of Plan 1's MapLibre Live Map. Operators can pre-download per-state PMTiles archives from `maps.nw5w.com`, and the map automatically uses local tiles where coverage exists, falling back to online tiles where it doesn't. Activates the third radio option (Graywolf private maps — offline) that Plan 1 left disabled.

**Architecture:**
- **Backend** streams PMTiles archives from `maps.nw5w.com/download/state/<slug>.pmtiles` to `<tile-cache-dir>/<slug>.pmtiles` on disk, tracking download status in a new `MapsDownload` SQLite table. A new in-process download manager runs each download in a goroutine, exposes progress via a polling endpoint. A static read-only handler at `GET /tiles/<slug>.pmtiles` serves the files to the browser with HTTP Range support (PMTiles needs that for efficient tile fetches).
- **Frontend** adds a state-picker bottom sheet (mobile-first) and per-state status rows (size, downloaded date, progress bar, delete) inside the existing Maps Settings tab. Activates the offline radio option.
- **Map integration** registers a custom MapLibre protocol `gw-tile://` whose handler picks per-tile between local PMTiles archives and the network. When the user is in "Graywolf maps" mode AND any state is downloaded, the map uses this federated protocol; otherwise it stays on the existing online behavior. State bounding boxes are hardcoded (they're stable and well-known) so the protocol handler can dispatch in O(states) per tile request without additional network round-trips.
- **Plan 1 follow-ups** ship in this plan: code-split `maplibre-gl` and `pmtiles` via Vite's `manualChunks` so non-map routes don't ship the ~600 KB gzipped mapping deps; hide MapLibre's `NavigationControl` zoom buttons on `≤768px` viewports (pinch-zoom is sufficient on touch).

**Tech Stack:**
- Backend: Go 1.22+, GORM/SQLite, `net/http` Range support for static file serving, goroutine-managed downloads with `context.WithCancel` for stop semantics
- Frontend: Svelte 5 runes, `pmtiles@3.2` `PMTiles` class for in-browser archive querying, `maplibre-gl@4.7` `addProtocol` for the federated tile router

---

## Mobile-First Design Principles (carried over from Plan 1)

These continue to apply to every UI task in this plan:

1. Phone-first layout (375×667 baseline); tablet/desktop are progressive enhancements via `@media (min-width: 769px)`
2. Hit targets ≥44×44 px on every button, toggle, list row
3. State picker presents as a bottom sheet on mobile (chonky-ui `Drawer` anchor="bottom"), as an inline section or popover on desktop
4. Progress bars are bold and clear (not 2-pixel hairlines)
5. Disk-size readouts honor the operator's units preference where applicable (sizes are always bytes, but use IEC binary prefixes — KiB, MiB, GiB — that match what `du -h` shows)
6. Use the existing theme tokens (`--map-overlay-*`, `--text-primary`, `--accent`, `--color-success`, etc.); add new ones only if genuinely needed
7. Long-running downloads must show progress (bytes + percent + estimated time remaining); never leave the operator wondering if it's working
8. Errors lead the user out: surface the verbatim error code, the GH issues URL, and a retry button

---

## File Structure

### Backend (Go)

**Create:**
- `graywolf/pkg/configstore/seed_downloads.go` — Get/List/Upsert/Delete for `MapsDownload`, status transitions
- `graywolf/pkg/configstore/seed_downloads_test.go`
- `graywolf/pkg/mapscache/manager.go` — `*Manager` orchestrates downloads, tracks active goroutines, exposes Status/Start/Stop/Delete methods
- `graywolf/pkg/mapscache/manager_test.go` — uses an `httptest.NewServer` stub of `maps.nw5w.com/download/state/...` with deliberate slow-write behavior to verify progress tracking
- `graywolf/pkg/mapscache/atomic_writer.go` — small helper for "write to .tmp file, then rename to final" so a partial download never corrupts the final path
- `graywolf/pkg/webapi/downloads.go` — five handlers: list, get-status, start (POST), delete (DELETE), serve-pmtiles (GET with Range support)
- `graywolf/pkg/webapi/downloads_test.go`
- `graywolf/pkg/webapi/dto/downloads.go` — DTOs for the list/status/start endpoints

**Modify:**
- `graywolf/pkg/configstore/models.go` — add `MapsDownload` struct
- `graywolf/pkg/configstore/store.go:123-148` — append `&MapsDownload{}` to AutoMigrate
- `graywolf/pkg/webapi/server.go` — add `mapsCache *mapscache.Manager` field; wire into `Config` and `NewServer`; call `s.registerDownloads(mux)` from `RegisterRoutes`
- `graywolf/pkg/app/wiring.go` — construct `mapscache.Manager` with the tile-cache-dir and the configstore, pass into `webapi.Config{MapsCache: ...}`

### Frontend (Svelte)

**Create:**
- `graywolf/web/src/lib/maps/downloads-store.svelte.js` — reactive store for the downloads list + active progress polling
- `graywolf/web/src/lib/maps/state-bounds.js` — hardcoded bounding boxes for the 51 entries in `state-list.js`
- `graywolf/web/src/lib/maps/format-bytes.js` — IEC byte formatter (`123 MiB`, `1.4 GiB`)
- `graywolf/web/src/lib/maps/state-picker.svelte` — modal/sheet containing the state list with download buttons
- `graywolf/web/src/lib/map/sources/gw-federated-protocol.js` — registers a `gw-tile://` MapLibre protocol that routes per-tile to PMTiles or network

**Modify:**
- `graywolf/web/src/routes/MapsSettings.svelte` — wire the state picker + downloaded-states list under the source picker; activate the third radio option when `downloadsState.completed.size > 0`
- `graywolf/web/src/lib/map/maplibre-map.svelte` — when `mapsState.source === 'graywolf'` AND `downloadsState.completed.size > 0`, switch from the upstream `style.json` to a locally-derived style that uses the `gw-tile://` protocol
- `graywolf/web/vite.config.js` — add `build.rollupOptions.output.manualChunks` to split `maplibre-gl` + `pmtiles` into a `vendor-map` chunk that only loads on `/map` and `/preferences/maps`
- `graywolf/web/src/lib/map/maplibre-map.svelte` (already on the file list) — hide `NavigationControl` on `≤768px` viewports

---

## Phase 1: Backend Download Infrastructure

### Task 1: Add `MapsDownload` model

**Files:**
- Modify: `graywolf/pkg/configstore/models.go`
- Modify: `graywolf/pkg/configstore/store.go` (AutoMigrate list)

- [ ] **Step 1: Append the struct to `models.go`** after `MapsConfig`:

```go
// MapsDownload tracks one state's offline PMTiles archive. The file
// itself lives at <tile-cache-dir>/<slug>.pmtiles; this row is just
// the metadata. Status transitions: pending -> downloading ->
// complete | error. A retry restarts at pending.
type MapsDownload struct {
	ID              uint32    `gorm:"primaryKey;autoIncrement" json:"id"`
	Slug            string    `gorm:"not null;uniqueIndex" json:"slug"`
	Status          string    `gorm:"not null;default:'pending'" json:"status"` // pending|downloading|complete|error
	BytesTotal      int64     `gorm:"not null;default:0" json:"bytes_total"`
	BytesDownloaded int64     `gorm:"not null;default:0" json:"bytes_downloaded"`
	DownloadedAt    time.Time `json:"downloaded_at"`
	ErrorMessage    string    `gorm:"not null;default:''" json:"error_message,omitempty"`
	CreatedAt       time.Time `json:"-"`
	UpdatedAt       time.Time `json:"-"`
}
```

- [ ] **Step 2: Add `&MapsDownload{}` to AutoMigrate in `store.go`** after `&MapsConfig{}`.

- [ ] **Step 3: Build to confirm**

Run: `cd graywolf && go build ./...`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add graywolf/pkg/configstore/models.go graywolf/pkg/configstore/store.go
git commit -m "configstore: add MapsDownload model"
```

---

### Task 2: Add `MapsDownload` CRUD with tests

**Files:**
- Create: `graywolf/pkg/configstore/seed_downloads.go`
- Create: `graywolf/pkg/configstore/seed_downloads_test.go`

- [ ] **Step 1: Write failing tests**

```go
package configstore

import (
	"context"
	"testing"
	"time"
)

func TestListMapsDownloads_EmptyByDefault(t *testing.T) {
	s := newTestStore(t)
	got, err := s.ListMapsDownloads(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 rows on fresh install, got %d", len(got))
	}
}

func TestUpsertMapsDownload_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	in := MapsDownload{
		Slug:            "georgia",
		Status:          "complete",
		BytesTotal:      52_000_000,
		BytesDownloaded: 52_000_000,
		DownloadedAt:    now,
	}
	if err := s.UpsertMapsDownload(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMapsDownload(ctx, "georgia")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "complete" || got.BytesTotal != 52_000_000 || got.Slug != "georgia" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestUpsertMapsDownload_RejectsBadStatus(t *testing.T) {
	s := newTestStore(t)
	err := s.UpsertMapsDownload(context.Background(), MapsDownload{
		Slug:   "georgia",
		Status: "weird",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestDeleteMapsDownload_RemovesRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_ = s.UpsertMapsDownload(ctx, MapsDownload{Slug: "texas", Status: "complete"})
	if err := s.DeleteMapsDownload(ctx, "texas"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetMapsDownload(ctx, "texas")
	if got.ID != 0 {
		t.Fatalf("expected row gone, got %+v", got)
	}
}

func TestUpsertMapsDownload_SecondCallUpdatesNotInserts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_ = s.UpsertMapsDownload(ctx, MapsDownload{Slug: "ohio", Status: "downloading", BytesDownloaded: 1024})
	first, _ := s.GetMapsDownload(ctx, "ohio")
	_ = s.UpsertMapsDownload(ctx, MapsDownload{Slug: "ohio", Status: "complete", BytesDownloaded: 99000})
	second, _ := s.GetMapsDownload(ctx, "ohio")
	if first.ID != second.ID {
		t.Fatalf("uniqueIndex on slug should have updated row, ID changed: %d -> %d", first.ID, second.ID)
	}
	if second.Status != "complete" {
		t.Fatalf("status not updated: %q", second.Status)
	}
}
```

- [ ] **Step 2: Run tests, confirm they fail**

`go test ./pkg/configstore/ -run TestListMapsDownloads -run TestUpsertMapsDownload -run TestDeleteMapsDownload -v`
Expected: FAIL — methods undefined.

- [ ] **Step 3: Implement `seed_downloads.go`**

```go
package configstore

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

var validDownloadStatuses = map[string]bool{
	"pending":     true,
	"downloading": true,
	"complete":    true,
	"error":       true,
}

func (s *Store) ListMapsDownloads(ctx context.Context) ([]MapsDownload, error) {
	var rows []MapsDownload
	if err := s.db.WithContext(ctx).Order("slug").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Store) GetMapsDownload(ctx context.Context, slug string) (MapsDownload, error) {
	var d MapsDownload
	err := s.db.WithContext(ctx).Where("slug = ?", slug).First(&d).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return MapsDownload{}, nil
	}
	return d, err
}

func (s *Store) UpsertMapsDownload(ctx context.Context, d MapsDownload) error {
	if !validDownloadStatuses[d.Status] {
		return fmt.Errorf("invalid status %q", d.Status)
	}
	if d.Slug == "" {
		return errors.New("slug required")
	}
	db := s.db.WithContext(ctx)
	var existing MapsDownload
	err := db.Where("slug = ?", d.Slug).First(&existing).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err == nil {
		d.ID = existing.ID
	}
	cols := map[string]any{
		"slug":             d.Slug,
		"status":           d.Status,
		"bytes_total":      d.BytesTotal,
		"bytes_downloaded": d.BytesDownloaded,
		"downloaded_at":    d.DownloadedAt,
		"error_message":    d.ErrorMessage,
	}
	if d.ID == 0 {
		return db.Model(&MapsDownload{}).Create(cols).Error
	}
	return db.Model(&MapsDownload{}).Where("id = ?", d.ID).UpdateColumns(cols).Error
}

func (s *Store) DeleteMapsDownload(ctx context.Context, slug string) error {
	return s.db.WithContext(ctx).Where("slug = ?", slug).Delete(&MapsDownload{}).Error
}
```

- [ ] **Step 4: Run tests, confirm they pass**

Expected: 5 PASS.

- [ ] **Step 5: Commit**

```bash
git add graywolf/pkg/configstore/seed_downloads.go graywolf/pkg/configstore/seed_downloads_test.go
git commit -m "configstore: add MapsDownload CRUD with validation"
```

---

### Task 3: Build the download manager (`pkg/mapscache`)

**Files:**
- Create: `graywolf/pkg/mapscache/atomic_writer.go`
- Create: `graywolf/pkg/mapscache/manager.go`
- Create: `graywolf/pkg/mapscache/manager_test.go`

The manager owns a goroutine pool (one in-flight download at a time per slug, max N concurrent total — start with N=2 to be polite to maps.nw5w.com), tracks each download's progress via atomic counters in memory, and persists final status to the configstore on completion.

- [ ] **Step 1: Atomic writer helper**

```go
// Package mapscache manages on-disk PMTiles archives for offline use.
package mapscache

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// writeAtomic streams r into a tempfile sibling of finalPath, then
// renames it. Truncates if a stale .tmp exists. Caller is responsible
// for removing the .tmp on early-return errors via the returned cleanup.
func writeAtomic(finalPath string, r io.Reader, onProgress func(int64)) (int64, error) {
	tmp := finalPath + ".tmp"
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return 0, fmt.Errorf("mkdir parent: %w", err)
	}
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, fmt.Errorf("open tmp: %w", err)
	}
	defer f.Close()

	written, err := copyWithProgress(f, r, onProgress)
	if err != nil {
		_ = os.Remove(tmp)
		return written, err
	}
	if err := f.Sync(); err != nil {
		_ = os.Remove(tmp)
		return written, fmt.Errorf("fsync: %w", err)
	}
	if err := os.Rename(tmp, finalPath); err != nil {
		_ = os.Remove(tmp)
		return written, fmt.Errorf("rename: %w", err)
	}
	return written, nil
}

func copyWithProgress(dst io.Writer, src io.Reader, onProgress func(int64)) (int64, error) {
	buf := make([]byte, 64*1024)
	var total int64
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return total, werr
			}
			total += int64(n)
			if onProgress != nil {
				onProgress(total)
			}
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}
```

- [ ] **Step 2: Manager**

```go
package mapscache

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// Manager orchestrates per-state PMTiles downloads. Active downloads
// run in goroutines; the in-memory `inflight` map exposes live
// byte-counters so the HTTP handler can report progress without
// hammering the store. Completion (or failure) writes to the store
// and removes the inflight entry. At most maxConcurrent downloads
// run at once.
type Manager struct {
	cacheDir       string                       // e.g. /var/lib/graywolf/tiles
	store          *configstore.Store           // for persistence
	tokenProvider  func(context.Context) string // returns the current Bearer token, or "" if not registered
	mapsBaseURL    string                       // e.g. https://maps.nw5w.com (override in tests)
	httpClient     *http.Client
	maxConcurrent  int

	mu       sync.Mutex
	inflight map[string]*activeDownload // slug -> running state
	sem      chan struct{}              // bounded semaphore size = maxConcurrent
}

type activeDownload struct {
	slug       string
	bytesTotal int64
	bytesDone  int64
	cancel     context.CancelFunc
	startedAt  time.Time
}

// Status snapshot for the API.
type Status struct {
	Slug            string    `json:"slug"`
	State           string    `json:"state"`            // "pending" | "downloading" | "complete" | "error" | "absent"
	BytesTotal      int64     `json:"bytes_total"`
	BytesDownloaded int64     `json:"bytes_downloaded"`
	DownloadedAt    time.Time `json:"downloaded_at,omitempty"`
	ErrorMessage    string    `json:"error_message,omitempty"`
}

func New(cacheDir string, store *configstore.Store, tokenProvider func(context.Context) string, mapsBaseURL string, maxConcurrent int) *Manager {
	if maxConcurrent < 1 {
		maxConcurrent = 2
	}
	return &Manager{
		cacheDir:      cacheDir,
		store:         store,
		tokenProvider: tokenProvider,
		mapsBaseURL:   mapsBaseURL,
		httpClient:    &http.Client{Timeout: 0}, // no timeout — large downloads
		maxConcurrent: maxConcurrent,
		inflight:      make(map[string]*activeDownload),
		sem:           make(chan struct{}, maxConcurrent),
	}
}

// PathFor returns the on-disk path of the given slug's PMTiles archive.
// The file may not exist; check Status.State == "complete" first.
func (m *Manager) PathFor(slug string) string {
	return filepath.Join(m.cacheDir, slug+".pmtiles")
}

// Status returns a snapshot for slug. Reads in-memory counters when
// downloading, falls back to the persisted row otherwise.
func (m *Manager) Status(ctx context.Context, slug string) (Status, error) {
	m.mu.Lock()
	a, isActive := m.inflight[slug]
	m.mu.Unlock()
	if isActive {
		return Status{
			Slug:            slug,
			State:           "downloading",
			BytesTotal:      a.bytesTotal,
			BytesDownloaded: a.bytesDone,
		}, nil
	}
	row, err := m.store.GetMapsDownload(ctx, slug)
	if err != nil {
		return Status{}, err
	}
	if row.ID == 0 {
		return Status{Slug: slug, State: "absent"}, nil
	}
	return Status{
		Slug:            row.Slug,
		State:           row.Status,
		BytesTotal:      row.BytesTotal,
		BytesDownloaded: row.BytesDownloaded,
		DownloadedAt:    row.DownloadedAt,
		ErrorMessage:    row.ErrorMessage,
	}, nil
}

// List returns the status of every state ever downloaded or in flight.
func (m *Manager) List(ctx context.Context) ([]Status, error) {
	rows, err := m.store.ListMapsDownloads(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Status, 0, len(rows)+len(m.inflight))
	seen := make(map[string]bool)
	for _, r := range rows {
		// Inflight overrides persisted: if a re-download is in progress, surface the live counter.
		m.mu.Lock()
		a, active := m.inflight[r.Slug]
		m.mu.Unlock()
		if active {
			out = append(out, Status{Slug: r.Slug, State: "downloading", BytesTotal: a.bytesTotal, BytesDownloaded: a.bytesDone})
		} else {
			out = append(out, Status{
				Slug: r.Slug, State: r.Status, BytesTotal: r.BytesTotal, BytesDownloaded: r.BytesDownloaded,
				DownloadedAt: r.DownloadedAt, ErrorMessage: r.ErrorMessage,
			})
		}
		seen[r.Slug] = true
	}
	// Surface inflight entries that don't have a persisted row yet (first download).
	m.mu.Lock()
	for slug, a := range m.inflight {
		if !seen[slug] {
			out = append(out, Status{Slug: slug, State: "downloading", BytesTotal: a.bytesTotal, BytesDownloaded: a.bytesDone})
		}
	}
	m.mu.Unlock()
	return out, nil
}

// Start kicks off a download for slug. Returns ErrAlreadyInflight if
// another download for the same slug is already running. Idempotent
// otherwise — re-downloads succeed by replacing the file atomically.
var ErrAlreadyInflight = errors.New("download already in flight")

func (m *Manager) Start(ctx context.Context, slug string) error {
	m.mu.Lock()
	if _, busy := m.inflight[slug]; busy {
		m.mu.Unlock()
		return ErrAlreadyInflight
	}
	dlCtx, cancel := context.WithCancel(context.Background())
	a := &activeDownload{slug: slug, cancel: cancel, startedAt: time.Now()}
	m.inflight[slug] = a
	m.mu.Unlock()

	// Persist a "downloading" row up front so the GET endpoint sees state immediately.
	_ = m.store.UpsertMapsDownload(ctx, configstore.MapsDownload{
		Slug:   slug,
		Status: "downloading",
	})

	go m.run(dlCtx, a)
	return nil
}

func (m *Manager) run(ctx context.Context, a *activeDownload) {
	m.sem <- struct{}{}
	defer func() { <-m.sem }()
	defer func() {
		m.mu.Lock()
		delete(m.inflight, a.slug)
		m.mu.Unlock()
	}()

	tok := m.tokenProvider(ctx)
	url := fmt.Sprintf("%s/download/state/%s.pmtiles", m.mapsBaseURL, a.slug)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		m.fail(ctx, a.slug, err)
		return
	}
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		m.fail(ctx, a.slug, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		m.fail(ctx, a.slug, fmt.Errorf("upstream %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode)))
		return
	}
	a.bytesTotal = resp.ContentLength
	if a.bytesTotal > 0 {
		_ = m.store.UpsertMapsDownload(ctx, configstore.MapsDownload{
			Slug: a.slug, Status: "downloading", BytesTotal: a.bytesTotal,
		})
	}

	finalPath := m.PathFor(a.slug)
	written, err := writeAtomic(finalPath, resp.Body, func(n int64) { a.bytesDone = n })
	if err != nil {
		m.fail(ctx, a.slug, err)
		return
	}

	_ = m.store.UpsertMapsDownload(ctx, configstore.MapsDownload{
		Slug: a.slug, Status: "complete",
		BytesTotal: written, BytesDownloaded: written,
		DownloadedAt: time.Now().UTC(),
	})
}

func (m *Manager) fail(ctx context.Context, slug string, err error) {
	_ = m.store.UpsertMapsDownload(ctx, configstore.MapsDownload{
		Slug: slug, Status: "error", ErrorMessage: err.Error(),
	})
}

// Delete cancels any in-flight download for slug, removes the row,
// and best-effort removes the on-disk file.
func (m *Manager) Delete(ctx context.Context, slug string) error {
	m.mu.Lock()
	if a, busy := m.inflight[slug]; busy {
		a.cancel()
		delete(m.inflight, slug)
	}
	m.mu.Unlock()
	if err := m.store.DeleteMapsDownload(ctx, slug); err != nil {
		return err
	}
	_ = os.Remove(m.PathFor(slug))
	return nil
}
```

- [ ] **Step 3: Tests**

Stub `maps.nw5w.com` via `httptest.NewServer` that serves a known PMTiles-shaped byte stream slowly enough to observe `BytesDownloaded` mid-flight. Verify:
- Start → progress visible in Status() while running → final state "complete" with bytes match
- Start on the same slug while running returns `ErrAlreadyInflight`
- Delete during active download cancels and removes the row
- Bad upstream status (e.g. 401) results in state "error" with the upstream status in the message
- Restart a slug after error: succeeds and replaces the row

Use a small temp dir for the cache and clean up via `t.TempDir()`.

- [ ] **Step 4: Build + test**

```bash
cd graywolf && go test ./pkg/mapscache/...
```

- [ ] **Step 5: Commit**

```bash
git add graywolf/pkg/mapscache/
git commit -m "mapscache: download manager with atomic file writes"
```

---

### Task 4: HTTP handlers — list, status, start, delete, serve

**Files:**
- Create: `graywolf/pkg/webapi/dto/downloads.go`
- Create: `graywolf/pkg/webapi/downloads.go`
- Create: `graywolf/pkg/webapi/downloads_test.go`
- Modify: `graywolf/pkg/webapi/server.go` — add `mapsCache *mapscache.Manager` field on `Server` and `MapsCache *mapscache.Manager` on `Config`. `NewServer` accepts it (nil OK in tests; routes that need it return 503 if absent). Call `s.registerDownloads(mux)` from `RegisterRoutes`.

### Endpoints

```
GET    /api/maps/downloads               -> []DownloadStatusResponse
GET    /api/maps/downloads/{slug}        -> DownloadStatusResponse
POST   /api/maps/downloads/{slug}        -> 202 {state: "downloading", ...}
DELETE /api/maps/downloads/{slug}        -> 204
GET    /tiles/{slug}.pmtiles             -> raw file with Range support
```

### Validation

- `{slug}` must match `^[a-z][a-z\-]{1,40}$` (mirror the slug pattern from the manifest); 400 on mismatch
- `{slug}` must be in the hardcoded US states list (use `state-list.js`'s slug set; mirror it in Go as a `validSlugs` set in the handler file)
- POST returns 409 if a download is already in flight for this slug
- DELETE on absent slug returns 204 (idempotent)

### Token plumbing

The `tokenProvider` callback that `mapscache.Manager` calls to get the bearer token is wired in `pkg/app/wiring.go`:

```go
mapsCache := mapscache.New(
    cfg.TileCacheDir,
    store,
    func(ctx context.Context) string {
        c, _ := store.GetMapsConfig(ctx)
        return c.Token
    },
    "https://maps.nw5w.com", // override per env var if you add one later
    2, // maxConcurrent
)
```

The download endpoints DON'T require operator authentication (cookie auth handles that at the api/* boundary already); they internally use the stored token to talk to the upstream.

### `/tiles/{slug}.pmtiles` — Range support

PMTiles fetches small byte ranges from the archive (the index, then per-tile blobs). Use Go's `http.ServeFile` which natively supports the `Range:` request header. Set `Cache-Control: public, max-age=3600` so MapLibre/browser caches the range responses for the session.

```go
func (s *Server) serveTilesPMTiles(w http.ResponseWriter, r *http.Request) {
    slug := r.PathValue("slug")
    if !validSlug(slug) {
        http.NotFound(w, r)
        return
    }
    path := s.mapsCache.PathFor(slug)
    info, err := os.Stat(path)
    if err != nil || info.IsDir() {
        http.NotFound(w, r)
        return
    }
    w.Header().Set("Content-Type", "application/vnd.pmtiles")
    w.Header().Set("Cache-Control", "private, max-age=3600")
    http.ServeFile(w, r, path)
}
```

Note: `/tiles/...` is OUTSIDE `/api/...` so it's NOT auto-wrapped by RequireAuth. Wire it explicitly in `pkg/app/wiring.go` AFTER the RequireAuth middleware so it benefits from the auth gate. Or wrap it manually:

```go
mux.Handle("GET /tiles/{slug}.pmtiles", webauth.RequireAuth(http.HandlerFunc(apiSrv.ServeTilesPMTiles)))
```

The handler must be a method on `*Server` exported as `ServeTilesPMTiles` so `wiring.go` can wrap it.

### Tests

- List with no rows → empty array
- POST starts a download (use a stub upstream); GET shortly after shows `state: "downloading"` with progress
- GET on absent slug returns `state: "absent"` with 200 (NOT 404 — the row's absence IS the answer)
- POST same slug twice → second call 409
- DELETE removes file + row; subsequent GET returns "absent"
- `/tiles/<slug>.pmtiles` 404s when no file; serves bytes when present; honors `Range:` header

- [ ] **Step 1-7: implement, test, commit**

Commit:
```
git commit -m "webapi: add /api/maps/downloads endpoints with PMTiles file serving"
```

---

### Task 5: Wire `mapscache.Manager` into the app

**Files:**
- Modify: `graywolf/pkg/app/wiring.go`

- [ ] **Step 1: Construct the manager**

After the configstore is opened and the tile cache dir is verified:

```go
mapsCache := mapscache.New(
    a.cfg.TileCacheDir,
    store,
    func(ctx context.Context) string {
        c, _ := store.GetMapsConfig(ctx)
        return c.Token
    },
    mapsauth.DefaultBaseURL, // wait — this is auth.nw5w.com, not maps.nw5w.com. Use a literal "https://maps.nw5w.com" here, or add a constant in pkg/mapscache.
    2,
)
```

- [ ] **Step 2: Pass into `webapi.NewServer`**

Update the `webapi.Config{...}` literal in `wiring.go:794-810` to include `MapsCache: mapsCache`.

- [ ] **Step 3: Wire the `/tiles/...` route on the outer mux behind RequireAuth**

Look at how `mux.Handle("/api/", webauth.RequireAuth(apiMux))` is structured. Add a sibling handler for `/tiles/`:

```go
tilesHandler := http.HandlerFunc(apiSrv.ServeTilesPMTiles)
mux.Handle("/tiles/", webauth.RequireAuth(tilesHandler))
```

The exact pattern syntax depends on Go 1.22 ServeMux behavior — verify with the existing routes. If the `/tiles/{slug}.pmtiles` pattern conflicts with existing patterns, scope it down or move into the apiMux.

- [ ] **Step 4: Build, run, smoke test**

```bash
cd graywolf && go build ./cmd/graywolf
./graywolf-bin --config-dir=/tmp/gwtest --tile-cache-dir=/tmp/gwtest/tiles
# In another terminal, after registering & seeding:
curl -s http://localhost:<port>/api/maps/downloads
```

- [ ] **Step 5: Commit**

```bash
git add graywolf/pkg/app/wiring.go
git commit -m "app: wire mapscache.Manager and /tiles route into the server"
```

---

## Phase 2: Frontend Download UI

### Task 6: Downloads store + format helper + state bounds

**Files:**
- Create: `graywolf/web/src/lib/maps/format-bytes.js`
- Create: `graywolf/web/src/lib/maps/state-bounds.js`
- Create: `graywolf/web/src/lib/maps/downloads-store.svelte.js`

- [ ] **Step 1: format-bytes.js**

```js
// IEC binary prefix formatter — matches what `du -h` shows.
const UNITS = ['bytes', 'KiB', 'MiB', 'GiB', 'TiB'];
export function formatBytes(n) {
  if (!Number.isFinite(n) || n <= 0) return '0 bytes';
  const idx = Math.min(UNITS.length - 1, Math.floor(Math.log(n) / Math.log(1024)));
  const v = n / Math.pow(1024, idx);
  return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${UNITS[idx]}`;
}
```

- [ ] **Step 2: state-bounds.js**

A static map of `slug -> [[swLat, swLon], [neLat, neLon]]` for all 51 entries. Use known US state bounding boxes; an authoritative source is the USGS state data (or the OpenStreetMap state extracts). Hardcode them. Example shape:

```js
export const STATE_BOUNDS = {
  alabama:        [[30.137, -88.473], [35.008, -84.889]],
  alaska:         [[51.214, -179.148], [71.538, -129.974]],
  // ... all 51
};
```

(Generate these using the OSM Nominatim API or `geojson-extent` against a known states geometry — write them in alphabetical order to match `state-list.js`.)

- [ ] **Step 3: downloads-store.svelte.js**

```js
import { toasts } from '../stores.js';

export const downloadsState = (() => {
  let items = $state(new Map()); // slug -> { state, bytes_total, bytes_downloaded, downloaded_at, error_message }
  let pollHandle = null;

  async function refresh() {
    try {
      const res = await fetch('/api/maps/downloads', { credentials: 'same-origin' });
      if (!res.ok) return;
      const arr = await res.json();
      const next = new Map();
      for (const r of arr) next.set(r.slug, r);
      items = next;
    } catch {}
  }

  async function start(slug) {
    const res = await fetch(`/api/maps/downloads/${slug}`, {
      method: 'POST',
      credentials: 'same-origin',
    });
    if (res.status === 409) {
      toasts.error(`${slug}: download already in progress`);
      return;
    }
    if (!res.ok) {
      const body = await res.json().catch(() => ({}));
      toasts.error(`${slug}: ${body.message ?? 'failed to start'}`);
      return;
    }
    await refresh();
    ensurePolling();
  }

  async function remove(slug) {
    const res = await fetch(`/api/maps/downloads/${slug}`, {
      method: 'DELETE',
      credentials: 'same-origin',
    });
    if (!res.ok) {
      toasts.error(`${slug}: delete failed`);
      return;
    }
    await refresh();
  }

  function hasActiveDownload() {
    for (const [, v] of items) if (v.state === 'downloading') return true;
    return false;
  }

  function ensurePolling() {
    if (pollHandle) return;
    const tick = async () => {
      await refresh();
      if (!hasActiveDownload()) {
        clearInterval(pollHandle);
        pollHandle = null;
      }
    };
    pollHandle = setInterval(tick, 1500);
  }

  return {
    get items() { return items; },
    refresh, start, remove,
    get completed() {
      const out = new Set();
      for (const [slug, v] of items) if (v.state === 'complete') out.add(slug);
      return out;
    },
    ensurePolling,
  };
})();
```

- [ ] **Step 4: Commit**

```bash
git add graywolf/web/src/lib/maps/format-bytes.js graywolf/web/src/lib/maps/state-bounds.js graywolf/web/src/lib/maps/downloads-store.svelte.js
git commit -m "ui(maps): add downloads store, byte formatter, and state bounds"
```

---

### Task 7: State picker + download list UI

**Files:**
- Create: `graywolf/web/src/lib/maps/state-picker.svelte`
- Modify: `graywolf/web/src/routes/MapsSettings.svelte` — add a "Offline maps" section under the source picker, only visible when `mapsState.registered`

The state picker is a modal/sheet that opens when the user clicks "Add a state" inside the offline-maps section. Inside the picker:
- A search box that filters the 51 states
- For each state, a row showing: name, current status (absent/complete/downloading/error), action button (Download / Re-download / Delete) with progress bar inline when downloading

The downloads section in MapsSettings shows ONLY downloaded states (status complete) — sorted alphabetically — with size, downloaded date, delete button, and re-download button.

`StatePicker.svelte` (sketch — fill in details):

```svelte
<script>
  import { US_STATES } from './state-list.js';
  import { downloadsState } from './downloads-store.svelte.js';
  import { formatBytes } from './format-bytes.js';
  import { Drawer, Button, Input } from '@chrissnell/chonky-ui';

  let { open = $bindable(false) } = $props();
  let query = $state('');

  let filtered = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return US_STATES;
    return US_STATES.filter((s) => s.name.toLowerCase().includes(q) || s.slug.includes(q));
  });
</script>

<Drawer bind:open anchor="bottom">
  <Drawer.Header>Add offline state</Drawer.Header>
  <Drawer.Body>
    <Input bind:value={query} placeholder="Search states..." />
    <ul class="state-list">
      {#each filtered as state}
        {@const item = downloadsState.items.get(state.slug)}
        {@const status = item?.state ?? 'absent'}
        <li class="state-row">
          <span class="state-name">{state.name}</span>
          <span class="state-status">
            {#if status === 'downloading'}
              {formatBytes(item.bytes_downloaded)} / {formatBytes(item.bytes_total)}
              <progress value={item.bytes_downloaded} max={item.bytes_total || 1}></progress>
            {:else if status === 'complete'}
              {formatBytes(item.bytes_total)}
            {:else if status === 'error'}
              <span class="status-error">Error: {item.error_message}</span>
            {/if}
          </span>
          {#if status === 'absent' || status === 'error'}
            <Button onclick={() => downloadsState.start(state.slug)}>Download</Button>
          {:else if status === 'complete'}
            <Button variant="default" onclick={() => downloadsState.start(state.slug)}>Re-download</Button>
            <Button variant="danger" onclick={() => downloadsState.remove(state.slug)}>Delete</Button>
          {/if}
        </li>
      {/each}
    </ul>
  </Drawer.Body>
</Drawer>

<!-- styles -->
```

In `MapsSettings.svelte`, add an "Offline maps" `<Box>` when registered:

```svelte
{#if mapsState.registered}
  <Box title="Offline maps">
    <p class="prose">
      Download per-state vector tiles for off-grid use. The map will use
      these automatically where coverage exists; it falls back to online
      tiles for areas you haven't downloaded.
    </p>
    {#if downloadsState.completed.size === 0}
      <p class="form-hint">No states downloaded yet.</p>
    {:else}
      <ul class="downloaded-list">
        {#each [...downloadsState.completed].sort() as slug}
          <!-- list rows with size, date, delete, re-download -->
        {/each}
      </ul>
    {/if}
    <Button class="maps-cta" onclick={() => (pickerOpen = true)}>
      Add a state
    </Button>
  </Box>
  <StatePicker bind:open={pickerOpen} />
{/if}
```

Mobile-first principles apply throughout (hit targets ≥44px, the picker takes 90vh on mobile, search input has 16px font).

- [ ] **Step 1-3: implement, build, smoke-test**
- [ ] **Step 4: Commit**

```bash
git commit -m "ui(maps): add state picker and downloaded-states list"
```

---

### Task 8: Activate the third radio option

**Files:**
- Modify: `graywolf/web/src/routes/MapsSettings.svelte`

The source picker block (Task 13 of Plan 1) has the offline radio always disabled. Update the disabled-rule:

```js
function isDisabled(src) {
  if (src.value === 'graywolf-offline') {
    // Disabled until at least one state is downloaded
    return downloadsState.completed.size === 0;
  }
  if (src.value === 'graywolf' && !mapsState.registered) return true;
  return false;
}
```

Update the third entry's `sublabel` to reflect what it does now:

```js
{ value: 'graywolf-offline', label: 'Graywolf private maps (offline)', sublabel: 'Pre-downloaded state tiles. Falls back to online for areas not covered.' }
```

When the user picks `graywolf-offline`, the store should still be `mapsState.setSource('graywolf')` — the offline preference is implicit (the map uses local PMTiles wherever they exist). This means we DON'T need a third source value in the backend. Plan 2's "offline" radio is just a UI affordance; the underlying source is "graywolf" plus the autodetection in the map.

ACTUALLY — better: split into two sources at the backend level: `'graywolf'` (online-preferred, falls back never) and `'graywolf-offline'` (offline-preferred, falls back online if no coverage). Then the user can choose between always-online (saves disk) and prefer-offline (faster + works without internet).

Decision (per Chris in the brainstorming Q&A): **always prefer downloaded offline tiles when they exist because it will be faster.** So we don't need a separate "online-only" source — when the user picks `'graywolf'`, the map automatically uses offline where available, online otherwise. The third radio option becomes a status indicator (it's selected automatically when at least one state is downloaded) rather than a chooser.

Cleanest: the third radio is decorative (locked-on selected when downloads exist, unselectable otherwise), the second radio is "Graywolf private maps" full stop. When the user picks "Graywolf private maps" AND has downloads, the map auto-uses offline. When they pick "Graywolf private maps" with no downloads, the map uses online only. When they pick "OSM", neither.

Even cleaner: **drop the third radio entirely.** Replace it with a status line under the second radio that says "Using offline tiles for: Georgia, Texas (3 of 51 states downloaded)" when applicable.

Pick whichever resonates. The plan as written assumed a third radio; you can simplify if it makes the UX cleaner.

- [ ] **Step 1: Implement the chosen approach**
- [ ] **Step 2: Build, smoke-test**
- [ ] **Step 3: Commit**

```bash
git commit -m "ui(maps): activate offline-tiles UI affordance when downloads exist"
```

---

### Task 9: Mobile QA pass on the new download UX

Same mobile-audit pattern as Plan 1 Task 14. Walk every breakpoint (320 / 375 / 768 / 1280px) and verify:
- The state picker as a bottom sheet on phones; modal/inline on desktop
- Progress bars are bold and clear
- Delete buttons can't be hit accidentally (separate from re-download)
- Long state names (e.g., "District of Columbia") wrap or ellipsis cleanly
- Search input doesn't trigger iOS auto-zoom (font-size ≥16px)

Fix anything that needs fixing. Commit:

```bash
git commit -m "ui(maps): mobile QA pass on offline downloads UX"
```

---

## Phase 3: Map Auto-Detection

### Task 10: Federated tile protocol

**Files:**
- Create: `graywolf/web/src/lib/map/sources/gw-federated-protocol.js`

Register a custom MapLibre protocol `gw-tile://` whose handler decides per-tile whether to read from a local PMTiles archive or fall through to the network.

```js
import { PMTiles } from 'pmtiles';
import { STATE_BOUNDS } from '../../maps/state-bounds.js';

// Map of slug -> PMTiles instance (lazy-init on first use)
const archives = new Map();

function getArchive(slug) {
  let a = archives.get(slug);
  if (!a) {
    a = new PMTiles(`/tiles/${slug}.pmtiles`);
    archives.set(slug, a);
  }
  return a;
}

// tileToBBox returns the geographic bounds of a tile (Web Mercator).
function tileToBBox(z, x, y) {
  const n = Math.pow(2, z);
  const lonW = (x / n) * 360 - 180;
  const lonE = ((x + 1) / n) * 360 - 180;
  const latN = (Math.atan(Math.sinh(Math.PI * (1 - (2 * y) / n))) * 180) / Math.PI;
  const latS = (Math.atan(Math.sinh(Math.PI * (1 - (2 * (y + 1)) / n))) * 180) / Math.PI;
  return [latS, lonW, latN, lonE];
}

function bboxIntersects([sLat, sLon, nLat, nLon], [[bSLat, bSLon], [bNLat, bNLon]]) {
  if (nLat < bSLat || sLat > bNLat) return false;
  if (nLon < bSLon || sLon > bNLon) return false;
  return true;
}

// completedSlugsProvider: callback returning a Set<string> of slugs
//   currently downloaded. Re-evaluated on each tile request.
// fallbackTileURL: function (z, x, y) => online tile URL (with auth)
// fetchOnline: function (url, abortSignal) => Promise<Uint8Array>

export function registerFederatedProtocol({ completedSlugsProvider, fetchOnline }) {
  return {
    request(params, abortController) {
      // params.url is "gw-tile://<z>/<x>/<y>"
      const m = /^gw-tile:\/\/(\d+)\/(\d+)\/(\d+)$/.exec(params.url);
      if (!m) return Promise.reject(new Error('bad gw-tile URL'));
      const [z, x, y] = [+m[1], +m[2], +m[3]];
      const tileBBox = tileToBBox(z, x, y);
      const completed = completedSlugsProvider();

      for (const slug of completed) {
        const bounds = STATE_BOUNDS[slug];
        if (!bounds) continue;
        if (bboxIntersects(tileBBox, bounds)) {
          return getArchive(slug).getZxy(z, x, y).then((tile) => {
            if (tile && tile.data) return { data: tile.data };
            // Tile not in this archive (zoom out of range, e.g.) — fall through.
            return fetchOnline(z, x, y, abortController.signal).then((data) => ({ data }));
          });
        }
      }
      // No coverage: fetch from network.
      return fetchOnline(z, x, y, abortController.signal).then((data) => ({ data }));
    },
  };
}
```

Wire it into `maplibre-map.svelte`. When `mapsState.source === 'graywolf'` AND `downloadsState.completed.size > 0`, use a custom-built style whose source URLs point at `gw-tile://...` instead of `https://maps.nw5w.com/...`. The protocol handler does the dispatching.

This means we generate the style.json client-side rather than fetching the americana-roboto one. Two options:
- **A**: Fetch the americana-roboto style.json, walk its sources, replace each source's `tiles` URLs with `gw-tile://{z}/{x}/{y}`, return that as the active style. Cache the rewritten style for the session.
- **B**: Hardcode a graywolf-style style.json in JS that references the gw-tile:// protocol. Simpler but loses easy upstream style updates.

Pick **A** — it's only 20 lines of glue and stays in sync with whatever the upstream americana style does.

- [ ] **Step 1-4: implement, smoke-test, commit**

```bash
git commit -m "ui(map): add gw-tile:// protocol for offline-aware tile dispatching"
```

---

### Task 11: Activate offline mode in maplibre-map.svelte

**Files:**
- Modify: `graywolf/web/src/lib/map/maplibre-map.svelte`

Update `buildStyle()` to check `downloadsState.completed.size > 0` AND fetch+rewrite the upstream style.json when in offline-aware mode. Use the protocol from Task 10 for the `tiles` URLs.

- [ ] **Step 1: Compute the active style**

```js
import { downloadsState } from '../maps/downloads-store.svelte.js';
import { registerFederatedProtocol } from './sources/gw-federated-protocol.js';

// Register once at module load (idempotent).
maplibregl.addProtocol('gw-tile', registerFederatedProtocol({
  completedSlugsProvider: () => downloadsState.completed,
  fetchOnline: async (z, x, y, signal) => {
    const url = `https://maps.nw5w.com/${z}/${x}/${y}.mvt`;
    const headers = bearerToken ? { Authorization: `Bearer ${bearerToken}` } : {};
    const res = await fetch(url, { headers, signal });
    if (!res.ok) throw new Error(`tile ${z}/${x}/${y} ${res.status}`);
    return new Uint8Array(await res.arrayBuffer());
  },
}).request);

async function buildStyle() {
  if (mapsState.source === 'graywolf' && mapsState.registered && downloadsState.completed.size > 0) {
    return await buildOfflineAwareStyle();
  }
  if (mapsState.source === 'graywolf' && mapsState.registered) {
    return graywolfVectorStyle(); // returns the upstream URL — online-only
  }
  return osmRasterStyle();
}

async function buildOfflineAwareStyle() {
  const res = await fetch('https://maps.nw5w.com/style/americana-roboto/style.json');
  const style = await res.json();
  for (const [, src] of Object.entries(style.sources)) {
    if (src.type === 'vector') {
      delete src.url; // remove the TileJSON pointer
      src.tiles = ['gw-tile://{z}/{x}/{y}']; // use our federated protocol
    }
  }
  return style;
}
```

Update the `$effect` that calls `setStyle` to handle the async case (since `buildStyle` is now async). MapLibre's `setStyle` accepts both an object and a URL synchronously, so:

```js
$effect(() => {
  const _src = mapsState.source;
  const _reg = mapsState.registered;
  const _dlcount = downloadsState.completed.size; // track changes here too
  if (!map) return;
  buildStyle().then((style) => map.setStyle(style));
});
```

Refresh the downloads list on mount (so the map knows which states are downloaded before the first style build):

```js
onMount(async () => {
  await downloadsState.refresh();
  await syncToken();
  // ... rest of existing setup
});
```

- [ ] **Step 2: Smoke-test**

With at least one state downloaded:
1. Open the map; pan into the downloaded state
2. Check the network tab: tile requests show as `gw-tile://...` (protocol calls aren't visible) but the actual fetches go to `/tiles/<slug>.pmtiles` with `Range:` headers
3. Pan outside the state's bbox; tile requests fetch from `maps.nw5w.com` with bearer
4. Delete the downloaded state; the map's next tile fetch goes back to the upstream

- [ ] **Step 3: Commit**

```bash
git commit -m "ui(map): wire offline-aware style with gw-tile protocol fallback"
```

---

## Phase 4: Plan 1 Follow-ups

### Task 12: Code-split `maplibre-gl` and `pmtiles` via `manualChunks`

**Files:**
- Modify: `graywolf/web/vite.config.js`

The current build emits a single ~425 KB gzipped JS bundle that ships on every route, including ones that don't render the map. Split MapLibre + pmtiles + the map source/layer modules into a separate chunk that only loads on `/map` and `/preferences/maps`.

- [ ] **Step 1: Update vite config**

```js
// vite.config.js
export default defineConfig({
  // ...
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules/maplibre-gl') ||
              id.includes('node_modules/pmtiles') ||
              id.includes('/lib/map/')) {
            return 'vendor-map';
          }
          // Optional: chonky-ui in its own chunk (it's also chunky)
          if (id.includes('node_modules/@chrissnell/chonky-ui')) {
            return 'vendor-chonky';
          }
        },
      },
    },
  },
});
```

The `/map` and `/preferences/maps` routes import from `lib/map/` and `lib/maps/`, so the dynamic-import trigger is automatic — Vite emits `vendor-map.js` as a separate chunk that the bundler loads on demand.

- [ ] **Step 2: Build, measure**

```bash
cd graywolf/web && npm run build
```

Compare bundle sizes. The main `index-*.js` chunk should drop substantially (target: under 250 KB gzipped). The `vendor-map-*.js` should be ~250 KB gzipped (MapLibre + pmtiles).

- [ ] **Step 3: Verify route-level loading**

Open the browser, load `/`, check network tab — `vendor-map` should NOT load. Click `Live Map` — `vendor-map` loads on demand.

- [ ] **Step 4: Commit**

```bash
git add graywolf/web/vite.config.js
git commit -m "ui(build): code-split maplibre-gl and pmtiles into vendor-map chunk"
```

---

### Task 13: Hide MapLibre NavigationControl on `≤768px`

**Files:**
- Modify: `graywolf/web/src/lib/map/maplibre-map.svelte`

On touch devices, the MapLibre +/- zoom buttons are redundant with pinch-zoom. Hide them at `≤768px` to free up the top-right corner for the FAB and prevent accidental taps.

- [ ] **Step 1: Add CSS**

In the existing `<style>` block, append:

```css
@media (max-width: 768px) {
  :global(.maplibregl-ctrl-top-right .maplibregl-ctrl-group) {
    display: none;
  }
}
```

- [ ] **Step 2: Verify**

Resize Chrome DevTools to 375px, navigate to `/map`. Confirm the +/- buttons are gone, the FAB stays at top-right, and pinch-zoom works.

- [ ] **Step 3: Commit**

```bash
git add graywolf/web/src/lib/map/maplibre-map.svelte
git commit -m "ui(map): hide NavigationControl zoom buttons on mobile"
```

---

## Phase 5: Final QA

### Task 14: End-to-end verification

This is "manual walkthrough" territory and the user (Chris) does it. Automated checks the implementer can do:

- [ ] `cd graywolf && go test ./pkg/configstore/... ./pkg/mapscache/... ./pkg/webapi/... ./pkg/app/...`
- [ ] `cd graywolf/web && npm run build`
- [ ] `cd graywolf/web && npx svelte-check`
- [ ] `cd graywolf && GOOS=linux go build ./...; GOOS=windows go build ./...; GOOS=darwin go build ./...`
- [ ] Walk through the offline flow with a real state download on a development machine; verify the file lands in `<tile-cache-dir>/<slug>.pmtiles`, the map uses it after pan, and Range requests work in the network tab

If any of those fails, fix and recommit.

---

## Out of scope (future work)

- Multi-country: the state-list and bounds are US-only. International states (e.g., Canadian provinces) ship when `maps.nw5w.com` adds the corresponding extracts.
- Resume / range-resume on partial downloads: the integration spec notes the upstream Worker doesn't support Range yet, so this is gated by upstream work.
- Auto-update of stale PMTiles archives: the upstream regenerates monthly. Add a "check for newer extract" UI affordance later — for Plan 2, operators manually re-download.
- Bandwidth controls / download scheduling: nice-to-have for users on metered connections.

---

## Self-review checklist (for the implementer)

- [ ] Every task has actual code, not placeholders
- [ ] All download endpoint error codes surface a verbatim message + GH issues link (consistent with Plan 1 patterns)
- [ ] PMTiles file serving correctly handles HTTP `Range:` headers (use `http.ServeFile`)
- [ ] The federated protocol falls back to network on tile-not-in-archive (out-of-range zoom, edge of bbox)
- [ ] Mobile-first design principles honored on every UI surface
- [ ] Plan 1 follow-ups (manualChunks, NavigationControl hide) actually land in this plan
- [ ] No breaking changes to Plan 1's `/api/preferences/maps` endpoints — they continue to work as-is
