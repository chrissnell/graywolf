// Package updatescheck polls GitHub's releases API once per day and
// caches the latest-release status in memory so the webapi layer can
// serve it without outbound network traffic on the request path.
//
// The package owns one background goroutine (Checker.Run) that honors
// a configstore-backed toggle and a reload channel for toggle flips.
// The HTTP handler reads a snapshot under RLock via Snapshot().
package updatescheck

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/releasenotes"
)

// Status string values exposed to the webapi layer. Mirrors the enum
// in design decision D6 so the handler is a pure projection with no
// business logic.
const (
	StatusDisabled  = "disabled"
	StatusPending   = "pending"
	StatusCurrent   = "current"
	StatusAvailable = "available"
)

// DefaultBaseURL is the production GitHub API base. Tests override via
// NewChecker's baseURL argument to point at an httptest.NewServer.
const DefaultBaseURL = "https://api.github.com"

// defaultStartupDelay is the wait from Run() starting to the first
// evaluate() call. Small enough that a freshly-upgraded operator sees
// the banner resolve within one pageview; large enough that a tight
// systemctl restart loop doesn't burn HTTP cycles every cycle (D3).
const defaultStartupDelay = 10 * time.Second

// defaultTickInterval is the cadence between daily evaluate() calls
// after the startup tick has fired (D3).
const defaultTickInterval = 10 * time.Minute

// defaultHTTPTimeout caps the single GitHub round-trip. Referenced in
// the in-flight-across-toggle-off doc comment below — when the toggle
// flips off mid-request, the request is allowed to complete because
// it's bounded by this timeout, and the next evaluate() overwrites
// status regardless of the outcome.
const defaultHTTPTimeout = 10 * time.Second

// githubOwner and githubRepo locate the upstream release feed. Kept
// as named constants so a fork only needs to edit two lines instead
// of hunting the release path and the User-Agent URL separately.
const (
	githubOwner = "chrissnell"
	githubRepo  = "graywolf"
)

// githubReleasePath is the relative path hit on c.baseURL. Note this
// is specifically /releases/latest, which excludes prereleases by
// design — beta tags on GitHub never trigger a banner for GA users.
var githubReleasePath = "/repos/" + githubOwner + "/" + githubRepo + "/releases/latest"

// Status is the publicly-visible shape of the cached check result.
// Returned by Snapshot() and projected one-to-one into the /api/updates/status
// response DTO in the webapi layer.
type Status struct {
	Status    string    // one of the Status* constants above
	Current   string    // running version, stripped of -dirty / -beta.N suffixes
	Latest    string    // tag_name from GitHub, stripped of a leading "v"
	URL       string    // html_url from GitHub, unmodified
	CheckedAt time.Time // time of most recent successful check; zero when none yet
}

// Store is the minimal configstore contract this package needs. Kept
// as an interface so tests don't need a real SQLite DB.
type Store interface {
	GetUpdatesConfig(ctx context.Context) (configstore.UpdatesConfig, error)
}

// Checker polls GitHub's releases API and serves the result via
// Snapshot(). Safe for concurrent use: Run mutates under Lock, Snapshot
// reads under RLock.
type Checker struct {
	version string // already suffix-stripped in NewChecker
	store   Store
	client  *http.Client
	baseURL string
	logger  *slog.Logger

	// startupDelay and tickInterval are unexported so tests can shrink
	// them via the in-package setters; production always uses the
	// defaults established in NewChecker.
	startupDelay time.Duration
	tickInterval time.Duration

	mu     sync.RWMutex
	status Status
}

// githubReleaseResponse is the slice of the /releases/latest payload
// we care about. Every other field is defensively ignored so GitHub
// API shape drift degrades to a no-op (risk #2 in the plan).
type githubReleaseResponse struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// NewChecker constructs a Checker. The version arg is stripped of any
// -dirty / -beta.N suffix once at construction so Status.Current is
// always bare MAJOR.MINOR.PATCH, even on dev builds. baseURL is
// DefaultBaseURL in production; tests pass httptest.Server.URL. logger
// is optional and falls back to slog.Default().
func NewChecker(version string, store Store, baseURL string, logger *slog.Logger) *Checker {
	if logger == nil {
		logger = slog.Default()
	}
	stripped := stripSuffix(version)
	return &Checker{
		version:      stripped,
		store:        store,
		client:       &http.Client{Timeout: defaultHTTPTimeout},
		baseURL:      baseURL,
		logger:       logger,
		startupDelay: defaultStartupDelay,
		tickInterval: defaultTickInterval,
		// Initial cached status is "pending" so a webapi handler
		// calling Snapshot() before the first check completes gets a
		// meaningful value instead of the Go zero Status{} (which would
		// stringly-render as "" and break the UI's enum branch).
		status: Status{Status: StatusPending},
	}
}

// Run blocks until ctx is cancelled. Safe to call exactly once per
// Checker. Starts with a short timer for the startup delay, then
// enters a ticker loop over ctx.Done(), the tick channel, and
// reloadCh. Both the tick and reload branches call evaluate(), so
// "check on schedule" and "check on toggle flip" share one code path.
func (c *Checker) Run(ctx context.Context, reloadCh <-chan struct{}) error {
	startup := time.NewTimer(c.startupDelay)
	defer startup.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-startup.C:
	}

	c.evaluate(ctx)

	ticker := time.NewTicker(c.tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			c.evaluate(ctx)
		case <-reloadCh:
			// Coalesced reloads collapse to a single wakeup; the
			// authoritative toggle value comes from the SQLite re-read
			// inside evaluate(), not from counting channel sends. The
			// reload channel is buffered size 1 specifically so a burst
			// of off->on->off toggles doesn't deadlock the PUT handler.
			c.evaluate(ctx)
		}
	}
}

// Snapshot returns a copy of the current status under RLock. Safe to
// call concurrently with Run; the webapi handler calls this on every
// /api/updates/status request.
func (c *Checker) Snapshot() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// evaluate re-reads the toggle state, then either clears the cache to
// "disabled" or runs a live check. Called from the ticker branch and
// the reload branch of Run.
func (c *Checker) evaluate(ctx context.Context) {
	cfg, err := c.store.GetUpdatesConfig(ctx)
	if err != nil {
		// Store errors are logged but don't disturb cached status: a
		// transient SQLite blip shouldn't flip a known-good
		// "available" back to "pending". The next tick or reload tries
		// again.
		c.logger.Debug("updatescheck: failed to read updates config", "err", err)
		return
	}
	if !cfg.Enabled {
		c.mu.Lock()
		c.status = Status{
			Status:  StatusDisabled,
			Current: c.version,
		}
		c.mu.Unlock()
		return
	}
	c.check(ctx)
}

// check makes the single HTTP call to GitHub, parses the response,
// and updates the cached status on success. On any error path the
// cached status is left untouched — a fresh Checker stays at
// "pending" and a Checker with a prior known-good "available" retains
// that value across a transient blip.
func (c *Checker) check(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+githubReleasePath, nil)
	if err != nil {
		c.logger.Debug("updatescheck: build request failed", "err", err)
		return
	}
	// GitHub rejects unauthenticated calls without a User-Agent. The
	// UA encodes the running version so the graywolf maintainer can
	// (if desired) observe fleet version distribution from server
	// logs on api.github.com, though we don't rely on that.
	req.Header.Set("User-Agent", fmt.Sprintf("%s/%s (+https://github.com/%s/%s)", githubRepo, c.version, githubOwner, githubRepo))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	// In-flight request is allowed to complete across a toggle-off
	// flip; the client has defaultHTTPTimeout bounding it, and the
	// next evaluate() (triggered by reload) reads the now-off toggle
	// and overwrites status.Status = "disabled" regardless of what
	// this request returns. No per-call cancellation is needed.
	resp, err := c.client.Do(req)
	if err != nil {
		c.logger.Debug("updatescheck: HTTP request failed", "err", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Include rate-limit headers so a 403 rate-limited scenario
		// is diagnosable from logs without needing a code change.
		// GitHub sets these on every authenticated-or-not response;
		// empty values are harmless noise.
		c.logger.Debug("updatescheck: non-2xx response",
			"status", resp.StatusCode,
			"rate_limit_remaining", resp.Header.Get("X-RateLimit-Remaining"),
			"rate_limit_reset", resp.Header.Get("X-RateLimit-Reset"))
		return
	}

	var body githubReleaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		c.logger.Debug("updatescheck: decode failed", "err", err)
		return
	}

	latest := stripTagPrefix(body.TagName)
	if latest == "" {
		// Empty tag from GitHub is not a protocol violation per se,
		// but we have nothing useful to cache — retain prior state.
		c.logger.Debug("updatescheck: empty tag_name in response")
		return
	}

	newStatus := StatusCurrent
	if releasenotes.Compare(latest, c.version) > 0 {
		newStatus = StatusAvailable
	}

	c.mu.Lock()
	c.status = Status{
		Status:    newStatus,
		Current:   c.version,
		Latest:    latest,
		URL:       body.HTMLURL,
		CheckedAt: time.Now().UTC(),
	}
	c.mu.Unlock()
}

// stripSuffix reduces a version like "0.10.11-dirty" or
// "0.11.0-beta.3" to "0.10.11" / "0.11.0". Duplicates the private
// strip() helper in pkg/releasenotes/semver.go rather than exporting
// it — the ten-line contract is stable and the duplication avoids
// scope creep into a neighboring package.
func stripSuffix(s string) string {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9') && c != '.' {
			return s[:i]
		}
	}
	return s
}

// stripTagPrefix drops a leading "v" from a GitHub tag name. Tag
// names on graywolf releases are "v0.11.0"; the Compare function
// expects bare digits.
func stripTagPrefix(tag string) string {
	if len(tag) > 0 && (tag[0] == 'v' || tag[0] == 'V') {
		return tag[1:]
	}
	return tag
}

// setStartupDelay and setTickInterval exist for tests only. Keeping
// them unexported and in-package means production code can't
// accidentally shorten the cadence.
func (c *Checker) setStartupDelay(d time.Duration) { c.startupDelay = d }
func (c *Checker) setTickInterval(d time.Duration) { c.tickInterval = d }
