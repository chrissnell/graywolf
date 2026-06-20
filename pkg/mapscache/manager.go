package mapscache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/mapsslug"
)

// DefaultMapsBaseURL is the production tile/download host. Override
// per env via New if you ever spin up a staging endpoint.
const DefaultMapsBaseURL = "https://maps.nw5w.com"

// Manager orchestrates per-state PMTiles downloads. Active downloads
// run in goroutines; the in-memory `inflight` map exposes live byte
// counters so the HTTP handler can report progress without hammering
// the store. Completion (or failure) writes the final state to the
// store and removes the inflight entry. At most maxConcurrent
// downloads run at once.
type Manager struct {
	cacheDir      string
	store         *configstore.Store
	tokenProvider func(context.Context) string
	mapsBaseURL   string
	httpClient    *http.Client
	maxConcurrent int

	mu       sync.Mutex
	inflight map[string]*activeDownload
	sem      chan struct{}
}

type activeDownload struct {
	slug       string
	bytesTotal atomic.Int64
	bytesDone  atomic.Int64
	cancel     context.CancelFunc
	startedAt  time.Time
}

// Status is a snapshot of one slug's download state.
type Status struct {
	Slug            string    `json:"slug"`
	State           string    `json:"state"` // "absent" | "pending" | "downloading" | "complete" | "error"
	BytesTotal      int64     `json:"bytes_total"`
	BytesDownloaded int64     `json:"bytes_downloaded"`
	DownloadedAt    time.Time `json:"downloaded_at,omitempty"`
	ErrorMessage    string    `json:"error_message,omitempty"`
}

// ErrAlreadyInflight is returned by Start when another download for
// the same slug is already running. Callers may ignore this and tell
// the user the download is already happening.
var ErrAlreadyInflight = errors.New("download already in flight")

// New constructs a Manager. tokenProvider is called per-download to
// fetch the current bearer token (it may change if the user
// re-registers); mapsBaseURL should be DefaultMapsBaseURL in
// production; maxConcurrent caps how many downloads run in parallel
// (defaults to 2 if non-positive — be polite to the upstream).
func New(cacheDir string, store *configstore.Store, tokenProvider func(context.Context) string, mapsBaseURL string, maxConcurrent int) *Manager {
	if maxConcurrent < 1 {
		maxConcurrent = 2
	}
	return &Manager{
		cacheDir:      cacheDir,
		store:         store,
		tokenProvider: tokenProvider,
		mapsBaseURL:   mapsBaseURL,
		// No HTTP timeout — these are large transfers. Cancellation
		// happens via the goroutine context; the caller's Stop or a
		// process shutdown both close the response body cleanly.
		httpClient:    &http.Client{},
		maxConcurrent: maxConcurrent,
		inflight:      make(map[string]*activeDownload),
		sem:           make(chan struct{}, maxConcurrent),
	}
}

// PathFor returns the on-disk path of slug's PMTiles archive. For
// namespaced slugs, the slashes become subdirectory separators:
//
//	state/colorado            -> <cache>/state/colorado.pmtiles
//	country/de                -> <cache>/country/de.pmtiles
//	province/ca/british-...   -> <cache>/province/ca/british-...pmtiles
func (m *Manager) PathFor(slug string) string {
	return filepath.Join(m.cacheDir, filepath.FromSlash(slug)+".pmtiles")
}

// urlForSlug returns the absolute download URL for a namespaced slug.
// Token is appended as ?t= when non-empty (matches the Worker contract).
// Returns an error for any slug that does not match the legal grammar.
func (m *Manager) urlForSlug(slug, token string) (string, error) {
	kind, a, b, ok := mapsslug.Parse(slug)
	if !ok {
		return "", fmt.Errorf("invalid slug %q", slug)
	}
	base := strings.TrimRight(m.mapsBaseURL, "/")
	var raw string
	switch kind {
	case "state":
		raw = fmt.Sprintf("%s/download/state/%s.pmtiles", base, a)
	case "country":
		raw = fmt.Sprintf("%s/download/country/%s.pmtiles", base, a)
	case "province":
		raw = fmt.Sprintf("%s/download/province/%s/%s.pmtiles", base, a, b)
	case "world":
		raw = fmt.Sprintf("%s/download/world.pmtiles", base)
	}
	if token == "" {
		return raw, nil
	}
	q := url.Values{}
	q.Set("t", token)
	return raw + "?" + q.Encode(), nil
}

// Status returns a snapshot for slug. Reads in-memory live counters
// when an active download is in progress; falls back to the persisted
// row otherwise. Returns State=="absent" with an empty Status if no
// row exists.
func (m *Manager) Status(ctx context.Context, slug string) (Status, error) {
	m.mu.Lock()
	a, isActive := m.inflight[slug]
	m.mu.Unlock()
	if isActive {
		return Status{
			Slug:            slug,
			State:           "downloading",
			BytesTotal:      a.bytesTotal.Load(),
			BytesDownloaded: a.bytesDone.Load(),
		}, nil
	}
	row, err := m.store.GetMapsDownload(ctx, slug)
	if err != nil {
		return Status{}, err
	}
	if row.ID == 0 {
		return Status{Slug: slug, State: "absent"}, nil
	}
	return statusFromRow(row), nil
}

// List returns the status of every state with a persisted row plus
// any in-flight downloads not yet persisted (first-time downloads
// before the upstream Content-Length comes back). Inflight entries
// override persisted ones so re-downloads show live progress.
func (m *Manager) List(ctx context.Context) ([]Status, error) {
	rows, err := m.store.ListMapsDownloads(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Status, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, r := range rows {
		m.mu.Lock()
		a, active := m.inflight[r.Slug]
		m.mu.Unlock()
		if active {
			out = append(out, Status{
				Slug: r.Slug, State: "downloading",
				BytesTotal: a.bytesTotal.Load(), BytesDownloaded: a.bytesDone.Load(),
			})
		} else {
			out = append(out, statusFromRow(r))
		}
		seen[r.Slug] = true
	}
	m.mu.Lock()
	for slug, a := range m.inflight {
		if !seen[slug] {
			out = append(out, Status{
				Slug: slug, State: "downloading",
				BytesTotal: a.bytesTotal.Load(), BytesDownloaded: a.bytesDone.Load(),
			})
		}
	}
	m.mu.Unlock()
	return out, nil
}

// Start kicks off a download for slug. Returns ErrAlreadyInflight if
// another download for the same slug is already running. Idempotent
// otherwise — re-downloads succeed by replacing the file atomically.
// The caller does not block: this returns as soon as the goroutine
// is spawned.
//
// bbox is the catalog-supplied bbox snapshot in [west, south, east,
// north] degrees, persisted into the maps_downloads row so the render
// path can serve offline tiles without consulting the remote catalog
// after a reboot. Pass nil only when no bbox is available; the startup
// backfill will fill it in from the pmtiles header later.
func (m *Manager) Start(ctx context.Context, slug string, bbox *[4]float64, maxZoom int) error {
	m.mu.Lock()
	if _, busy := m.inflight[slug]; busy {
		m.mu.Unlock()
		return ErrAlreadyInflight
	}
	dlCtx, cancel := context.WithCancel(context.Background())
	a := &activeDownload{slug: slug, cancel: cancel, startedAt: time.Now()}
	m.inflight[slug] = a
	m.mu.Unlock()

	// Persist a "downloading" row up front so a GET right after Start
	// returns the right state even before the goroutine grabs the
	// semaphore.
	row := configstore.MapsDownload{
		Slug:    slug,
		Status:  "downloading",
		MaxZoom: maxZoom,
	}
	if bbox != nil {
		encoded := encodeBBox(*bbox)
		row.BBox = &encoded
	}
	_ = m.store.UpsertMapsDownload(ctx, row)

	go m.run(dlCtx, a)
	return nil
}

// encodeBBox writes [w,s,e,n] as the JSON array used on the wire and
// in the maps_downloads.bbox column. We hand-format instead of going
// through encoding/json so the result is deterministic (no insertion
// of spaces, stable float formatting via 'f' verb with -1 precision
// — the shortest decimal representation that round-trips).
func encodeBBox(b [4]float64) string {
	return fmt.Sprintf("[%s,%s,%s,%s]",
		strconv.FormatFloat(b[0], 'f', -1, 64),
		strconv.FormatFloat(b[1], 'f', -1, 64),
		strconv.FormatFloat(b[2], 'f', -1, 64),
		strconv.FormatFloat(b[3], 'f', -1, 64),
	)
}

// BackfillBBoxes scans every completed maps_downloads row whose BBox
// column is NULL and fills it in by reading the bbox from the
// corresponding on-disk pmtiles archive header. Idempotent: rows that
// already have a bbox are skipped without touching the filesystem.
// Per-row failures (missing archive, malformed header) are logged at
// WARN and don't abort the pass — the render path falls back to the
// catalog for those slugs and a later re-download repopulates them.
//
// Called once at startup from pkg/app/wiring.go after
// MigrateMapsDownloadSlugs. Cheap on warm installs (every row is
// already populated); only does work on the first start after upgrade.
func (m *Manager) BackfillBBoxes(ctx context.Context) error {
	rows, err := m.store.ListMapsDownloads(ctx)
	if err != nil {
		return fmt.Errorf("list downloads: %w", err)
	}
	for _, r := range rows {
		if r.Status != "complete" || r.BBox != nil {
			continue
		}
		path := m.PathFor(r.Slug)
		bbox, err := ReadArchiveBBox(path)
		if err != nil {
			slog.Warn("mapscache bbox backfill: skipping",
				"slug", r.Slug, "path", path, "err", err)
			continue
		}
		encoded := encodeBBox(bbox)
		r.BBox = &encoded
		if err := m.store.UpsertMapsDownload(ctx, r); err != nil {
			slog.Warn("mapscache bbox backfill: upsert failed",
				"slug", r.Slug, "err", err)
		}
	}
	return nil
}

// Delete cancels any in-flight download for slug, removes the
// persisted row, and best-effort removes the on-disk file. Idempotent
// on absent slugs.
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

// run executes a single download. It blocks on the semaphore so
// maxConcurrent downloads run simultaneously across the manager.
func (m *Manager) run(ctx context.Context, a *activeDownload) {
	m.sem <- struct{}{}
	defer func() { <-m.sem }()
	defer func() {
		m.mu.Lock()
		// Only delete if we still own the entry. A concurrent Delete
		// may have already removed it.
		if cur, ok := m.inflight[a.slug]; ok && cur == a {
			delete(m.inflight, a.slug)
		}
		m.mu.Unlock()
	}()

	fullURL, err := m.urlForSlug(a.slug, m.tokenProvider(ctx))
	if err != nil {
		m.fail(ctx, a.slug, err)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		m.fail(ctx, a.slug, err)
		return
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		m.fail(ctx, a.slug, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		m.fail(ctx, a.slug, fmt.Errorf("upstream %d %s", resp.StatusCode, http.StatusText(resp.StatusCode)))
		return
	}
	a.bytesTotal.Store(resp.ContentLength)
	if resp.ContentLength > 0 {
		_ = m.store.UpsertMapsDownload(ctx, configstore.MapsDownload{
			Slug: a.slug, Status: "downloading", BytesTotal: resp.ContentLength,
		})
	}

	finalPath := m.PathFor(a.slug)
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		m.fail(ctx, a.slug, err)
		return
	}
	written, err := writeAtomic(finalPath, resp.Body, func(n int64) { a.bytesDone.Store(n) })
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

// MigrateLegacyArchives moves legacy bare-slug files
// (<cache>/colorado.pmtiles) into the new namespaced state subdir
// (<cache>/state/colorado.pmtiles). Idempotent: skips files already
// in subdirs and skips non-pmtiles files.
//
// Collision policy: if the namespaced target already exists, the legacy
// file is removed rather than overwriting the (presumably newer)
// namespaced file. Otherwise os.Rename clobbers a file an operator may
// have already redownloaded under the new layout. Designed to run once
// on startup; safe to re-run.
func (m *Manager) MigrateLegacyArchives(ctx context.Context) error {
	_ = ctx
	if m.cacheDir == "" {
		return nil
	}
	entries, err := os.ReadDir(m.cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	stateDir := filepath.Join(m.cacheDir, "state")
	// Repair the world archive an earlier release's migration wrongly
	// relocated into the state/ subdir. Keeps the file in step with the
	// DB slug restored by MigrateMapsDownloadSlugs.
	if err := m.repairWorldArchive(stateDir); err != nil {
		return err
	}
	leafRE := mapsslug.LeafRegexp()
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".pmtiles") {
			continue
		}
		slug := strings.TrimSuffix(name, ".pmtiles")
		if slug == "world" {
			// The global world archive is a legitimate top-level slug,
			// not a legacy bare state file -- leave it in place.
			continue
		}
		if !leafRE.MatchString(slug) {
			continue
		}
		if err := os.MkdirAll(stateDir, 0o755); err != nil {
			return err
		}
		oldPath := filepath.Join(m.cacheDir, name)
		newPath := filepath.Join(stateDir, name)
		if _, err := os.Stat(newPath); err == nil {
			// Namespaced target already exists; drop the legacy file
			// rather than clobbering newer data.
			if err := os.Remove(oldPath); err != nil {
				return fmt.Errorf("migrate %s: remove legacy: %w", name, err)
			}
			continue
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("migrate %s: stat target: %w", name, err)
		}
		if err := os.Rename(oldPath, newPath); err != nil {
			return fmt.Errorf("migrate %s: %w", name, err)
		}
	}
	return nil
}

// repairWorldArchive moves a misplaced state/world.pmtiles back to the
// top-level world.pmtiles. The world archive's legitimate slug is bare
// "world"; an earlier release's legacy migration treated it as a state
// slug and relocated the file under state/. Idempotent: a no-op when the
// misplaced file is absent. If the canonical file already exists, the
// misplaced copy is removed rather than clobbering it.
func (m *Manager) repairWorldArchive(stateDir string) error {
	misplaced := filepath.Join(stateDir, "world.pmtiles")
	if _, err := os.Stat(misplaced); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("repair world archive: stat legacy: %w", err)
	}
	canonical := filepath.Join(m.cacheDir, "world.pmtiles")
	if _, err := os.Stat(canonical); err == nil {
		if err := os.Remove(misplaced); err != nil {
			return fmt.Errorf("repair world archive: remove legacy: %w", err)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("repair world archive: stat target: %w", err)
	}
	if err := os.Rename(misplaced, canonical); err != nil {
		return fmt.Errorf("repair world archive: %w", err)
	}
	return nil
}

func (m *Manager) fail(ctx context.Context, slug string, err error) {
	// A user-initiated cancel (Delete) cancels the download context,
	// which surfaces here as an error from http.Do or writeAtomic -- but
	// it is not a real failure. Delete has already removed the persisted
	// row and the on-disk file, so there is nothing to report; recording
	// an "error" row would resurrect a phantom entry and flash a spurious
	// failure in the UI. cancel() returns before the goroutine unblocks,
	// so ctx.Err() is reliably non-nil on the cancel path. This guard is
	// the sole gate for the cancel case.
	if ctx.Err() != nil {
		return
	}
	// Persist the failure on a fresh context. A genuine download error
	// must be recorded even though the download's own context is the one
	// that just errored out -- tying this write to that context would
	// make error reporting depend on the driver's ctx handling and could
	// silently drop the row. The guard above, not the context, is what
	// suppresses the cancel case.
	_ = m.store.UpsertMapsDownload(context.Background(), configstore.MapsDownload{
		Slug: slug, Status: "error", ErrorMessage: err.Error(),
	})
}

func statusFromRow(r configstore.MapsDownload) Status {
	return Status{
		Slug:            r.Slug,
		State:           r.Status,
		BytesTotal:      r.BytesTotal,
		BytesDownloaded: r.BytesDownloaded,
		DownloadedAt:    r.DownloadedAt,
		ErrorMessage:    r.ErrorMessage,
	}
}
