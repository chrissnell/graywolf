package mapscache

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
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
	kind, a, b, ok := parseSlug(slug)
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
	}
	if token == "" {
		return raw, nil
	}
	q := url.Values{}
	q.Set("t", token)
	return raw + "?" + q.Encode(), nil
}

// parseSlug is a copy of webapi.parseSlug, kept here to avoid an import
// cycle. They MUST stay in lockstep -- see webapi/slug_test.go and
// mapscache/manager_test.go for parallel coverage.
func parseSlug(s string) (kind, a, b string, ok bool) {
	if s == "" || len(s) > 80 {
		return "", "", "", false
	}
	parts := strings.Split(s, "/")
	switch parts[0] {
	case "state":
		if len(parts) != 2 || !reSlugLeaf.MatchString(parts[1]) {
			return "", "", "", false
		}
		return "state", parts[1], "", true
	case "country":
		if len(parts) != 2 || !reISO2.MatchString(parts[1]) || parts[1] == "cn" || parts[1] == "ru" {
			return "", "", "", false
		}
		return "country", parts[1], "", true
	case "province":
		if len(parts) != 3 || !reISO2.MatchString(parts[1]) || !reSlugLeaf.MatchString(parts[2]) {
			return "", "", "", false
		}
		if parts[1] == "cn" || parts[1] == "ru" {
			return "", "", "", false
		}
		return "province", parts[1], parts[2], true
	}
	return "", "", "", false
}

var (
	reSlugLeaf = regexp.MustCompile(`^[a-z][a-z0-9-]{0,49}$`)
	reISO2     = regexp.MustCompile(`^[a-z]{2}$`)
)

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

	// Persist a "downloading" row up front so a GET right after Start
	// returns the right state even before the goroutine grabs the
	// semaphore.
	_ = m.store.UpsertMapsDownload(ctx, configstore.MapsDownload{
		Slug:   slug,
		Status: "downloading",
	})

	go m.run(dlCtx, a)
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
// in subdirs and skips non-pmtiles files. Designed to run once on
// startup; safe to re-run.
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
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".pmtiles") {
			continue
		}
		slug := strings.TrimSuffix(name, ".pmtiles")
		if !reSlugLeaf.MatchString(slug) {
			continue
		}
		if err := os.MkdirAll(stateDir, 0o755); err != nil {
			return err
		}
		oldPath := filepath.Join(m.cacheDir, name)
		newPath := filepath.Join(stateDir, name)
		if err := os.Rename(oldPath, newPath); err != nil {
			return fmt.Errorf("migrate %s: %w", name, err)
		}
	}
	return nil
}

func (m *Manager) fail(ctx context.Context, slug string, err error) {
	_ = m.store.UpsertMapsDownload(ctx, configstore.MapsDownload{
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
