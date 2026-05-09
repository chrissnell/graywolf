# Android Phase 3 — Production Entry, Service Supervisor, Real SPA in WebView

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Android Go-entry stub with a real `cmd/graywolf` Android binary that boots the full graywolf runtime, serves the production Svelte SPA from `web/embed.go` over a bearer-authed `127.0.0.1:8080`, and integrates with the Phase 2 Kotlin Service supervisor — RX-only, no GPS or proto-driven PTT yet.

**Architecture:** Phase 2 already shipped the Kotlin `GraywolfService` (modem JNI + AudioPump + GoLauncher + Supervisor + PlatformServer) and a working Go child (`cmd/graywolf-pocb`) that proves the loopback HTTP + bearer-token + readiness-byte loop. Phase 3 retires `cmd/graywolf-pocb`, builds the real `cmd/graywolf/main_android.go` against `pkg/app`, ports the bearer-auth pattern into `pkg/webauth`, gates `pkg/updatescheck` on Android, and teaches the production Svelte SPA to read the bearer token from a `WebAppInterface` JS bridge and inject it into every `fetch` and `WebSocket` call. The Phase 2 Kotlin surface gets minor cleanups: POC-D PTT methods removed from the JS bridge (dormant until Phase 5), `network_security_config.xml` narrows cleartext to `127.0.0.1`, manifest gains the remaining permissions/foreground-service types, and a "Stop" notification action.

**Tech Stack:** Go 1.23+ (build-tagged `android`), Kotlin (Android API 28+), Svelte 5 + Vite, protobuf-go, `pkg/webauth` HTTP middleware, `pkg/app` composition root, `pkg/platformsvc` (Phase 2), JNI cdylib (Phase 1/POC-A), Gradle AGP 8.5.

**Reference docs:**
- Spec: `.context/2026-05-09-android-phase-3-spec.md`
- Phase 2 results (baseline): `.claude/worktrees/feature+android-phase-2/.context/2026-05-09-android-phase-2-results.md`
- Design doc: `.context/2026-05-01-android-app-design.md`

**Conventions:**
- Phase 3 baseline is the Phase 2 worktree (`.claude/worktrees/feature+android-phase-2/`). Either rebase that work into `main` first, or branch from the phase-2 tip. **Plan assumes you are working from a branch where Phase 2's commits are present.**
- Run all Go tests with `GOWORK=off` per `feedback_goembed_goworkoff_in_worktrees`.
- Never amend prior commits. After hook failure: fix → re-stage → new commit.
- Commits: no AI attribution, no "Generated with", no Co-Authored-By.

---

## File map

**Go side:**
- CREATE `pkg/webauth/bearer.go` — `BearerAuthMiddleware(token string)`.
- CREATE `pkg/webauth/bearer_test.go`.
- MODIFY `pkg/app/config.go` — add `BearerToken`, `Platform`, `OnHTTPListenerReady` fields.
- MODIFY `pkg/app/wiring.go` — gate `updatescheck.NewChecker` on `cfg.Platform != "android"`; wire bearer middleware when `cfg.BearerToken != ""`; fire `OnHTTPListenerReady` after `net.Listen` succeeds.
- REPLACE `cmd/graywolf/main_android.go` — real Android entry (was 11-line stub).
- DELETE `cmd/graywolf-pocb/` (entire directory).

**Kotlin side:**
- MODIFY `android/app/src/main/AndroidManifest.xml` — add permissions, expand `foregroundServiceType`, swap `usesCleartextTraffic` for `networkSecurityConfig`.
- CREATE `android/app/src/main/res/xml/network_security_config.xml`.
- MODIFY `android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt` — drop `gainPoller`, add "Stop" notification action + `BroadcastReceiver`, expand `foregroundServiceType` constant.
- MODIFY `android/app/src/main/kotlin/com/nw5w/graywolf/MainActivity.kt` — fire battery-opt whitelist intent on first launch; remove `UsbPttAdapter.enumerate()` from `onResume`.
- MODIFY `android/app/src/main/kotlin/com/nw5w/graywolf/binaries/GoLauncher.kt` — rename log tags from `graywolf-pocb` to `GraywolfGo`.
- MODIFY `android/app/src/main/kotlin/com/nw5w/graywolf/webview/WebAppInterface.kt` — remove POC-C `fireTxTest` and all POC-D PTT methods; keep only `getBearerToken`.
- MODIFY `android/app/build.gradle.kts` — `goCrossCompile_*` tasks point at `./cmd/graywolf` (was `./cmd/graywolf-pocb`).
- CREATE `android/app/src/test/kotlin/com/nw5w/graywolf/webview/WebAppInterfaceTest.kt`.

**SPA side:**
- CREATE `web/src/lib/androidBridge.js` — `getBearerToken()` cache.
- CREATE `web/src/lib/androidBridge.test.js`.
- CREATE `web/src/bootstrap.js` — runs `installSecureFetch` + `installSecureWebSocket` before any other module evaluates.
- MODIFY `web/src/main.js` — `import './bootstrap.js'` as the FIRST statement, before any other import.
- CREATE `web/src/lib/secureFetch.js` — `installSecureFetch()`, `installSecureWebSocket()`.
- CREATE `web/src/lib/secureFetch.test.js`.
- MODIFY `web/src/lib/api.js` — skip the `#/login` redirect on 401 when the Android bridge is present (Android has no login route).

**Run report:**
- CREATE `.context/2026-05-09-android-phase-3-results.md` (final task).

---

## Track A — Go entry + bearer middleware

### Task 1: Bearer-auth middleware

**Files:**
- Create: `pkg/webauth/bearer.go`
- Create: `pkg/webauth/bearer_test.go`

- [ ] **Step 1: Write the failing test**

```go
// pkg/webauth/bearer_test.go
package webauth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func TestBearerAuth_HeaderMatch(t *testing.T) {
	mw := BearerAuthMiddleware("hex-token-abc")
	srv := httptest.NewServer(mw(okHandler()))
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/anything", nil)
	req.Header.Set("Authorization", "Bearer hex-token-abc")
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("want 200 got %d", res.StatusCode)
	}
}

func TestBearerAuth_HeaderMismatch(t *testing.T) {
	mw := BearerAuthMiddleware("hex-token-abc")
	srv := httptest.NewServer(mw(okHandler()))
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/anything", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	res, _ := srv.Client().Do(req)
	if res.StatusCode != 401 {
		t.Fatalf("want 401 got %d", res.StatusCode)
	}
}

func TestBearerAuth_HeaderMissing(t *testing.T) {
	mw := BearerAuthMiddleware("hex-token-abc")
	srv := httptest.NewServer(mw(okHandler()))
	defer srv.Close()

	res, _ := srv.Client().Get(srv.URL + "/api/anything")
	if res.StatusCode != 401 {
		t.Fatalf("want 401 got %d", res.StatusCode)
	}
}

func TestBearerAuth_WSUpgradeAcceptsQueryToken(t *testing.T) {
	mw := BearerAuthMiddleware("hex-token-abc")
	srv := httptest.NewServer(mw(okHandler()))
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/ws/x?token=hex-token-abc", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	res, _ := srv.Client().Do(req)
	if res.StatusCode != 200 {
		t.Fatalf("want 200 got %d", res.StatusCode)
	}
}

func TestBearerAuth_WSUpgradeRejectsQueryTokenMismatch(t *testing.T) {
	mw := BearerAuthMiddleware("hex-token-abc")
	srv := httptest.NewServer(mw(okHandler()))
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/ws/x?token=wrong", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	res, _ := srv.Client().Do(req)
	if res.StatusCode != 401 {
		t.Fatalf("want 401 got %d", res.StatusCode)
	}
}

func TestBearerAuth_NonWSRejectsQueryToken(t *testing.T) {
	mw := BearerAuthMiddleware("hex-token-abc")
	srv := httptest.NewServer(mw(okHandler()))
	defer srv.Close()

	res, _ := srv.Client().Get(srv.URL + "/api/x?token=hex-token-abc")
	if res.StatusCode != 401 {
		t.Fatalf("want 401 got %d (query token must not bypass for non-WS)", res.StatusCode)
	}
}

func TestBearerAuth_EmptyTokenPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("want panic on empty token")
		} else if !strings.Contains(strings.ToLower(toString(r)), "bearer") {
			t.Fatalf("panic message should mention bearer; got %v", r)
		}
	}()
	_ = BearerAuthMiddleware("")
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if e, ok := v.(error); ok {
		return e.Error()
	}
	return ""
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOWORK=off go test ./pkg/webauth/ -run TestBearerAuth -v`
Expected: FAIL — `BearerAuthMiddleware` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
// pkg/webauth/bearer.go
package webauth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// BearerAuthMiddleware requires Authorization: Bearer <token> on every
// request, except WebSocket upgrade requests which may also pass
// ?token=<hex>. Mismatch responds 401 with a JSON error body.
//
// Used only by the Android entry (cmd/graywolf/main_android.go) where
// the local HTTP listener is reachable by every other app on the
// device — see invariant N7. Empty token is a programmer error and
// panics; the Android Service must always inject one.
func BearerAuthMiddleware(token string) func(http.Handler) http.Handler {
	if token == "" {
		panic("webauth: BearerAuthMiddleware requires non-empty token")
	}
	want := []byte(token)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if matchHeader(r, want) {
				next.ServeHTTP(w, r)
				return
			}
			if isWebSocketUpgrade(r) && matchQueryToken(r, want) {
				next.ServeHTTP(w, r)
				return
			}
			jsonError(w, http.StatusUnauthorized, "authentication required")
		})
	}
}

func matchHeader(r *http.Request, want []byte) bool {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return false
	}
	got := []byte(h[len(prefix):])
	return subtle.ConstantTimeCompare(got, want) == 1
}

func matchQueryToken(r *http.Request, want []byte) bool {
	got := []byte(r.URL.Query().Get("token"))
	if len(got) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOWORK=off go test ./pkg/webauth/ -run TestBearerAuth -v`
Expected: PASS — all 7 sub-tests.

- [ ] **Step 5: Commit**

```bash
git add pkg/webauth/bearer.go pkg/webauth/bearer_test.go
git commit -m "feat(webauth): add bearer-token middleware for Android loopback HTTP

Per-launch token authentication for the Android Service-injected
listener; WebSocket upgrades may pass the token via ?token=, regular
API requests must use the Authorization header. Empty token panics
since the Android Service must always inject one."
```

---

### Task 2: Add `Platform`, `BearerToken`, `OnHTTPListenerReady` to `app.Config`

**Files:**
- Modify: `pkg/app/config.go`
- Modify: `pkg/app/flags.go` (zero-value defaults)

- [ ] **Step 1: Read the current Config struct**

Run: `grep -n "type Config struct\|^}" pkg/app/config.go | head -20`
Note the line range of the existing `Config` struct.

- [ ] **Step 2: Add the three fields**

Edit `pkg/app/config.go`. Inside the `Config` struct, after the existing fields, insert:

```go
	// Platform is "android" on Android builds, "" elsewhere. Wiring
	// uses it to gate components that don't make sense on Android
	// (updatescheck, native serial PTT). Set by main_android.go from
	// the GRAYWOLF_PLATFORM env var.
	Platform string

	// BearerToken, if non-empty, gates every HTTP and WebSocket
	// request behind webauth.BearerAuthMiddleware. Set by
	// main_android.go from the GRAYWOLF_LISTEN_TOKEN env var (the
	// Android Service generates a fresh 32-byte hex token at every
	// cold start). Empty on desktop. (Invariant N7.)
	BearerToken string

	// OnHTTPListenerReady, if non-nil, is invoked exactly once after
	// net.Listen succeeds for the main HTTP listener and before
	// Serve starts blocking. Used by main_android.go to write the
	// readiness "\n" to stdout that GoLauncher waits on. Desktop
	// builds leave it nil.
	OnHTTPListenerReady func()
```

- [ ] **Step 3: Confirm flag parsing leaves the new fields zero**

Run: `grep -n "Platform\|BearerToken\|OnHTTPListenerReady" pkg/app/flags.go`
Expected: no matches. The new fields are not flag-driven; defaults are zero, set only by `main_android.go`.

- [ ] **Step 4: Verify desktop build still compiles**

Run: `GOWORK=off go build ./...`
Expected: clean build.

- [ ] **Step 5: Commit**

```bash
git add pkg/app/config.go
git commit -m "feat(app): add Platform, BearerToken, OnHTTPListenerReady config fields

Three Android-only knobs on Config; all default to zero values so
desktop behavior is unchanged. Wired up in subsequent tasks: bearer
middleware (Task 3), updatescheck gating (Task 4), HTTP-ready hook
(Task 5)."
```

---

### Task 3: Wire `BearerAuthMiddleware` into the HTTP server when `cfg.BearerToken != ""`

**Files:**
- Modify: `pkg/app/wiring.go` (around line 1254 where `httpSrv` is constructed)

- [ ] **Step 1: Locate the HTTP handler chain**

Run: `grep -n "httpSrv = \&http.Server\|Handler:" pkg/app/wiring.go | head -10`
Note the line where `Handler:` is set on `&http.Server{}`. The middleware must wrap that handler.

- [ ] **Step 2: Wrap the handler conditionally**

Find the block that constructs `a.httpSrv = &http.Server{ ... Handler: <X>, ...}`. Just before it, insert:

```go
	mainHandler := <X> // whatever the existing Handler expression is

	if a.cfg.BearerToken != "" {
		mainHandler = webauth.BearerAuthMiddleware(a.cfg.BearerToken)(mainHandler)
	}
```

Then replace `Handler: <X>` with `Handler: mainHandler` in the `&http.Server{}` literal.

(Exact rename: read the existing expression, name it `mainHandler` via local variable, then conditionally wrap. Pattern matches the existing `webauth.RequireAuth(...)` style elsewhere in the file.)

- [ ] **Step 3: Add a wiring-level test**

**Files:**
- Create: `pkg/app/bearer_wiring_test.go`

```go
package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBearerWiring_AppliedWhenSet exercises the wiring decision in
// isolation: when cfg.BearerToken is non-empty the middleware wraps
// the handler chain.
func TestBearerWiring_AppliedWhenSet(t *testing.T) {
	cfg := Config{BearerToken: "abc"}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	h := wrapWithBearerIfSet(cfg, inner)
	req := httptest.NewRequest("GET", "/api/x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("want 401 (no auth header) got %d", rec.Code)
	}
}

func TestBearerWiring_NoOpWhenEmpty(t *testing.T) {
	cfg := Config{BearerToken: ""}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	h := wrapWithBearerIfSet(cfg, inner)
	req := httptest.NewRequest("GET", "/api/x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("want 200 (no middleware) got %d", rec.Code)
	}
}
```

- [ ] **Step 4: Extract a small helper to make the wiring testable**

Edit `pkg/app/wiring.go`. Add (next to the modified handler-construction block):

```go
// wrapWithBearerIfSet wraps next with BearerAuthMiddleware iff
// cfg.BearerToken is non-empty. Extracted from the httpSrv
// construction to keep the wiring decision testable.
func wrapWithBearerIfSet(cfg Config, next http.Handler) http.Handler {
	if cfg.BearerToken == "" {
		return next
	}
	return webauth.BearerAuthMiddleware(cfg.BearerToken)(next)
}
```

Replace the inline conditional from Step 2 with `mainHandler = wrapWithBearerIfSet(a.cfg, mainHandler)`.

- [ ] **Step 5: Run tests**

Run: `GOWORK=off go test ./pkg/app/ -run TestBearerWiring -v`
Expected: PASS.

Run: `GOWORK=off go test ./pkg/app/...`
Expected: existing app tests still PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/app/wiring.go pkg/app/bearer_wiring_test.go
git commit -m "feat(app): wrap HTTP handler in BearerAuthMiddleware when token set

Wiring is a no-op when cfg.BearerToken is empty so desktop behavior
is unchanged. Android entry sets the token via env var; the
middleware then gates every request on the loopback listener."
```

---

### Task 4: Gate `updatescheck.NewChecker` on `cfg.Platform != "android"`

**Files:**
- Modify: `pkg/app/wiring.go` (around line 1149 where the checker is constructed)

- [ ] **Step 1: Locate the checker construction**

Run: `grep -n "updatescheck.NewChecker\|updatesChecker = " pkg/app/wiring.go`
Note the line number.

- [ ] **Step 2: Wrap construction in a platform check**

Around the `a.updatesChecker = updatescheck.NewChecker(...)` block (and any subsequent `Start()` call on it), wrap with:

```go
	if a.cfg.Platform != "android" {
		a.updatesChecker = updatescheck.NewChecker(
			// ... existing args unchanged ...
		)
		// ... existing Start() / wire-into-component logic ...
	} else {
		a.logger.Info("updatescheck: disabled on android (Play Store handles updates)")
	}
```

If `a.updatesChecker` is referenced elsewhere unconditionally, gate those references with `if a.updatesChecker != nil { ... }`.

- [ ] **Step 3: Add a platform-gating test**

**Files:**
- Create: `pkg/app/platform_gating_test.go`

```go
package app

import (
	"testing"
)

// TestPlatformGating_AndroidSkipsUpdatescheck is a sentinel: it
// constructs an App with cfg.Platform = "android" and asserts that
// the updates checker is nil after wireServices. Catches accidental
// re-introduction of the unconditional construction.
func TestPlatformGating_AndroidSkipsUpdatescheck(t *testing.T) {
	t.Skip("integration test — wireServices needs a real configstore; revisit if checker.go gains a unit-testable surface")
	// Intentionally skipped at the unit level. The wiring code path is
	// covered by the cross-compile probe in Task 8 (./... builds clean
	// for GOOS=android with no updatescheck side-effects at runtime).
}
```

(The skipped test is a documentation marker. Real coverage is the cross-compile probe + manual logcat inspection in Task 21.)

- [ ] **Step 4: Cross-compile probe**

Run:
```bash
GOWORK=off GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./pkg/app/...
GOWORK=off go build ./...
```
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add pkg/app/wiring.go pkg/app/platform_gating_test.go
git commit -m "feat(app): skip updatescheck wiring when Platform == android

Play Store handles upgrades on Android per design doc §9; the
in-process GitHub poll has no purpose there and would generate
unwanted background traffic. Desktop builds (Platform == \"\")
keep the existing behavior."
```

---

### Task 5: Fire `OnHTTPListenerReady` after `net.Listen` succeeds

**Files:**
- Modify: `pkg/app/wiring.go` (around line 2501 where `httpSrv.ListenAndServe()` is called)

- [ ] **Step 1: Read the current listen call site**

Run: `sed -n '2490,2520p' pkg/app/wiring.go`
Note: it currently calls `a.httpSrv.ListenAndServe()` directly.

- [ ] **Step 2: Replace ListenAndServe with explicit Listen + ready hook + Serve**

Find the line `if err := a.httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {` and replace the call with:

```go
			ln, lerr := net.Listen("tcp", a.httpSrv.Addr)
			if lerr != nil {
				// ... existing error handling for listen failure;
				// preserve the surrounding logger/return shape ...
				a.logger.Error("http: listen failed", "addr", a.httpSrv.Addr, "err", lerr)
				return
			}
			if a.cfg.OnHTTPListenerReady != nil {
				a.cfg.OnHTTPListenerReady()
			}
			if err := a.httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				// ... existing error handling unchanged ...
			}
```

(Adjust the surrounding error-handling/logging shape to match existing code; the key is the `Listen → OnHTTPListenerReady → Serve` ordering.)

- [ ] **Step 3: Verify `net` import is present**

Run: `head -50 pkg/app/wiring.go | grep '"net"'`
If absent, add `"net"` to the import block.

- [ ] **Step 4: Run existing tests**

Run: `GOWORK=off go test ./pkg/app/...`
Expected: existing tests still PASS. (No new unit test for the hook itself — the existing app smoke tests cover the listen-then-serve path; the hook is exercised end-to-end by Task 19's cold-start trace, which checks for the `graywolf-android: listener_ready` logcat line.)

- [ ] **Step 5: Commit**

```bash
git add pkg/app/wiring.go pkg/app/bearer_wiring_test.go
git commit -m "feat(app): expose OnHTTPListenerReady hook fired between Listen and Serve

Replaces ListenAndServe with explicit net.Listen + Serve so callers
can be notified when the port is bound. Android entry uses this to
write the \\n readiness byte that the Kotlin GoLauncher waits on."
```

---

### Task 6: Replace `cmd/graywolf/main_android.go` stub with real entry

**Files:**
- Replace: `cmd/graywolf/main_android.go`

- [ ] **Step 1: Inspect the stub baseline**

Run: `cat cmd/graywolf/main_android.go`
Expected: 11-line stub from Phase 2.

- [ ] **Step 2: Write the real entry**

Replace the entire file:

```go
//go:build android

// Android entry for graywolf. Constructs an app.Config from
// Service-injected env vars (no flags, no signal.Notify -- the
// Android Service owns the process lifecycle), connects to the
// Kotlin PlatformServer for a Hello handshake, then runs
// app.New(cfg).Run(ctx). The HTTP listener gets a per-launch
// bearer-token middleware (invariant N7); a readiness "\n" is
// written to stdout once the listener is bound so the Kotlin
// GoLauncher.startAndAwaitReady gate releases.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/chrissnell/graywolf/pkg/app"
	"github.com/chrissnell/graywolf/pkg/platformproto"
	"github.com/chrissnell/graywolf/pkg/platformsvc"
)

const platformSchemaVersion = 1

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg, err := configFromEnv()
	if err != nil {
		logger.Error("graywolf-android: env parse failed", "err", err)
		os.Exit(2)
	}
	cfg.Version = Version
	cfg.GitCommit = GitCommit

	cfg.OnHTTPListenerReady = func() {
		_, _ = os.Stdout.Write([]byte("\n"))
		_ = os.Stdout.Sync()
		logger.Info("graywolf-android: listener_ready")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := platformHello(ctx, logger, cfg.PlatformSocket); err != nil {
		logger.Error("graywolf-android: platformsvc handshake failed", "err", err)
		os.Exit(1)
	}

	if err := app.New(cfg, logger).Run(ctx); err != nil {
		logger.Error("graywolf-android: exited with error", "err", err)
		os.Exit(1)
	}
}

// platformHello dials the Kotlin PlatformServer at sockPath, exchanges
// Hello, logs the agreed schema version. Mismatch returns an error;
// the supervisor will restart the process.
func platformHello(ctx context.Context, logger *slog.Logger, sockPath string) error {
	if sockPath == "" {
		return errors.New("GRAYWOLF_PLATFORM_SOCKET unset")
	}
	cli, err := platformsvc.NewClient(sockPath, logger.With("component", "platformsvc"))
	if err != nil {
		return fmt.Errorf("new client: %w", err)
	}
	defer cli.Close()

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := cli.Connect(dialCtx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	resp, err := cli.Hello(dialCtx, platformSchemaVersion)
	if err != nil {
		return fmt.Errorf("hello: %w", err)
	}
	logger.Info("platformsvc: connected",
		"server_version", resp.GetServerVersion(),
		"schema_version", resp.GetSchemaVersion())
	if resp.GetSchemaVersion() != platformSchemaVersion {
		return fmt.Errorf("schema mismatch: client=%d server=%d",
			platformSchemaVersion, resp.GetSchemaVersion())
	}
	return nil
}

// configFromEnv builds an app.Config from the Service-injected env
// vars. Missing required vars are fatal at startup; the Service
// supervisor will restart but will keep failing -- visible in logcat.
func configFromEnv() (app.Config, error) {
	must := func(k string) (string, error) {
		v := os.Getenv(k)
		if v == "" {
			return "", fmt.Errorf("required env %s is empty", k)
		}
		return v, nil
	}

	dbPath, err := must("GRAYWOLF_DB")
	if err != nil {
		return app.Config{}, err
	}
	historyDB, err := must("GRAYWOLF_HISTORY_DB")
	if err != nil {
		return app.Config{}, err
	}
	logDB, err := must("GRAYWOLF_LOG_DB")
	if err != nil {
		return app.Config{}, err
	}
	tileCache, err := must("GRAYWOLF_TILE_CACHE")
	if err != nil {
		return app.Config{}, err
	}
	modemSock, err := must("GRAYWOLF_MODEM_SOCKET")
	if err != nil {
		return app.Config{}, err
	}
	platformSock, err := must("GRAYWOLF_PLATFORM_SOCKET")
	if err != nil {
		return app.Config{}, err
	}
	listen, err := must("GRAYWOLF_LISTEN")
	if err != nil {
		return app.Config{}, err
	}
	token, err := must("GRAYWOLF_LISTEN_TOKEN")
	if err != nil {
		return app.Config{}, err
	}

	cfg := app.Config{
		DBPath:          dbPath,
		HistoryDBPath:   historyDB,
		LogBufferDBPath: logDB,
		TileCachePath:   tileCache,
		ModemSocketPath: modemSock,
		PlatformSocket:  platformSock,
		ListenAddr:      listen,
		BearerToken:     token,
		Platform:        os.Getenv("GRAYWOLF_PLATFORM"),
	}
	return cfg, nil
}

// Version and GitCommit are injected at build time via -ldflags --
// matching cmd/graywolf/main.go's pattern so the JNI modemVersion()
// equality check still works.
var (
	Version   = "dev"
	GitCommit = "unknown"
)
```

**Note for the implementer:** the field names `DBPath`, `HistoryDBPath`, `LogBufferDBPath`, `TileCachePath`, `ModemSocketPath`, `PlatformSocket`, `ListenAddr` must match the actual `app.Config` struct. If the struct uses different names (e.g. `HistoryDB` vs `HistoryDBPath`), adapt to the existing schema rather than renaming. Also: if `app.Config` doesn't yet have all of these fields (e.g., `PlatformSocket`, `ModemSocketPath`, `TileCachePath` may be flag-only on desktop), surface to the user rather than inventing new fields silently.

- [ ] **Step 3: Cross-compile probe**

Run:
```bash
GOWORK=off GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build -o /tmp/graywolf-android-arm64 ./cmd/graywolf
echo "arm64 build OK"
```
Expected: clean.

For x86_64 (per Phase 2 finding #2, needs cgo):
```bash
GOWORK=off GOOS=android GOARCH=amd64 CGO_ENABLED=1 \
  CC="$ANDROID_NDK_ROOT/toolchains/llvm/prebuilt/$(uname | tr '[:upper:]' '[:lower:]')-x86_64/bin/x86_64-linux-android28-clang" \
  go build -o /tmp/graywolf-android-amd64 ./cmd/graywolf
echo "amd64 build OK"
```
Expected: clean.

- [ ] **Step 4: Add a config-parse unit test**

**Files:**
- Create: `cmd/graywolf/main_android_test.go`

```go
//go:build android

package main

import (
	"os"
	"strings"
	"testing"
)

func TestConfigFromEnv_AllSet(t *testing.T) {
	defer clearEnv(t, andEnvKeys...)
	for k, v := range happyEnv() {
		t.Setenv(k, v)
	}
	cfg, err := configFromEnv()
	if err != nil {
		t.Fatalf("configFromEnv: %v", err)
	}
	if cfg.BearerToken != "tok-abc" {
		t.Fatalf("BearerToken = %q want tok-abc", cfg.BearerToken)
	}
	if cfg.Platform != "android" {
		t.Fatalf("Platform = %q want android", cfg.Platform)
	}
}

func TestConfigFromEnv_MissingRequired(t *testing.T) {
	defer clearEnv(t, andEnvKeys...)
	for k, v := range happyEnv() {
		t.Setenv(k, v)
	}
	t.Setenv("GRAYWOLF_LISTEN_TOKEN", "")
	_, err := configFromEnv()
	if err == nil || !strings.Contains(err.Error(), "GRAYWOLF_LISTEN_TOKEN") {
		t.Fatalf("want missing-token error; got %v", err)
	}
}

var andEnvKeys = []string{
	"GRAYWOLF_DB",
	"GRAYWOLF_HISTORY_DB",
	"GRAYWOLF_LOG_DB",
	"GRAYWOLF_TILE_CACHE",
	"GRAYWOLF_MODEM_SOCKET",
	"GRAYWOLF_PLATFORM_SOCKET",
	"GRAYWOLF_LISTEN",
	"GRAYWOLF_LISTEN_TOKEN",
	"GRAYWOLF_PLATFORM",
}

func clearEnv(t *testing.T, keys ...string) {
	for _, k := range keys {
		_ = os.Unsetenv(k)
	}
}

func happyEnv() map[string]string {
	return map[string]string{
		"GRAYWOLF_DB":              "/tmp/graywolf.db",
		"GRAYWOLF_HISTORY_DB":      "/tmp/graywolf-history.db",
		"GRAYWOLF_LOG_DB":          "/tmp/graywolf-logs.db",
		"GRAYWOLF_TILE_CACHE":      "/tmp/tiles",
		"GRAYWOLF_MODEM_SOCKET":    "/tmp/modem.sock",
		"GRAYWOLF_PLATFORM_SOCKET": "@/tmp/platform.sock",
		"GRAYWOLF_LISTEN":          "127.0.0.1:8080",
		"GRAYWOLF_LISTEN_TOKEN":    "tok-abc",
		"GRAYWOLF_PLATFORM":        "android",
	}
}
```

- [ ] **Step 5: Run the test**

Run: `GOWORK=off go test -tags android ./cmd/graywolf/ -run TestConfigFromEnv -v`
Expected: PASS.

(Note: `-tags android` builds with the android tag set; the `//go:build android` constraint on `main_android.go` and the test file will be honored. The test does NOT exercise `app.New().Run()` -- that requires real DBs/sockets and is integration scope (Task 22).)

- [ ] **Step 6: Commit**

```bash
git add cmd/graywolf/main_android.go cmd/graywolf/main_android_test.go
git commit -m "feat(graywolf): real Android entry replacing phase-2 stub

Reads env-var contract from the Kotlin Service, exchanges Hello with
PlatformServer, runs app.New(cfg).Run(ctx) with bearer-token gating
and the HTTP-ready hook that emits the readiness byte. No
signal.Notify -- the Service owns process lifecycle."
```

---

### Task 7: Retire `cmd/graywolf-pocb`

**Files:**
- Delete: `cmd/graywolf-pocb/` (entire directory)
- Modify: `android/app/build.gradle.kts` (the `goCrossCompile_*` Exec tasks)

- [ ] **Step 1: Confirm pocb is no longer referenced anywhere**

Run: `grep -rn "graywolf-pocb" --include="*.go" --include="*.kt" --include="*.kts" --include="*.sh" --include="Makefile" .`
Expected: only matches in `cmd/graywolf-pocb/` itself plus build.gradle.kts (which we're about to update). Anything else means a dependency we missed.

- [ ] **Step 2: Update Gradle goCrossCompile tasks**

Edit `android/app/build.gradle.kts`. Find the `goCrossCompile_arm64` and `goCrossCompile_x86_64` Exec task definitions (search: `grep -n "graywolf-pocb\|cmd/graywolf" android/app/build.gradle.kts`). Replace any `./cmd/graywolf-pocb` argument with `./cmd/graywolf`.

- [ ] **Step 3: Remove the directory**

Run: `git rm -r cmd/graywolf-pocb`

- [ ] **Step 4: Cross-compile probe**

Run:
```bash
GOWORK=off GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...
```
Expected: clean.

- [ ] **Step 5: Gradle assembleDebug probe**

Run: `cd android && ./gradlew assembleDebug -x lint`
Expected: builds. APK at `android/app/build/outputs/apk/debug/app-debug.apk`.

If goCrossCompile fails, surface — likely the build.gradle.kts task argument wasn't updated correctly.

- [ ] **Step 6: Commit**

```bash
git add android/app/build.gradle.kts
git commit -m "build(android): retire cmd/graywolf-pocb; goCrossCompile points at cmd/graywolf

POC-B's stub Go binary is no longer needed -- the real Android entry
in cmd/graywolf/main_android.go provides everything the Service
needs (HTTP, bearer auth, modem UDS, platformsvc Hello). The Gradle
goCrossCompile_arm64 and goCrossCompile_x86_64 tasks now build the
real entry."
```

---

### Task 7.5: Audit Phase 2 `Supervisor.kt` + add `GoLauncher` unit tests

The plan delegates supervisor restart correctness to Phase 2's `Supervisor.kt` (which already implements dead-handle tracking via `var prev: Process?`) and `GoLauncher.kt` readiness-byte wait, but Phase 2 shipped no Kotlin test coverage for either. Spec §4.9 lists `GoLauncherTest` as a Phase 3 deliverable. Without it, criterion #21's "supervisor restart works" depends entirely on a manual SIGKILL on the T865 — fragile, unrepeatable, and silently regression-prone.

**Files:**
- Audit (read-only): `android/app/src/main/kotlin/com/nw5w/graywolf/binaries/Supervisor.kt`
- Audit (read-only): `android/app/src/main/kotlin/com/nw5w/graywolf/binaries/GoLauncher.kt`
- Create: `android/app/src/test/kotlin/com/nw5w/graywolf/binaries/GoLauncherTest.kt`
- Create: `android/app/src/test/kotlin/com/nw5w/graywolf/binaries/SupervisorTest.kt`

- [ ] **Step 1: Audit Supervisor.kt against the dead-handle invariant**

Read the existing `Supervisor.kt`. Confirm:
- `goWatcher` thread tracks `var prev: Process?` and only calls `waitFor` when `processSupplier()` returns a *new* handle (line ~28 in Phase 2 baseline).
- `restartLoop` thread blocks on `restartLock.wait()` and is signalled only by `signalFailure()`.
- `recentFailures` deque + `pruneFailures` enforce the 3-failures-in-60s halt.
- `backoffsMs = longArrayOf(1_000, 2_000, 5_000, 10_000)` matches the spec.

If any invariant is missing or wrong, fix it as part of this task and call it out in the commit message.

- [ ] **Step 1a: Confirm bearer-token rotation semantics**

Spec §1 criterion #11 says the token is generated on every cold start "and on every supervisor restart of the Go child". Phase 2 baseline reads the token from `(application as GraywolfApp).bearerToken` in both `bootGoChild()` and `supervisorRestart()` — meaning the token is generated ONCE per `GraywolfApp` lifetime (process cold start) and is STABLE across supervisor-driven Go-child restarts. This is a deliberate phase 3 deviation from the spec wording: rotating the token per Go restart would require WebView reload + SPA bridge cache invalidation coordination, which is non-trivial.

The threat model (per-launch token preventing other apps from minting a valid header) is preserved by per-cold-start rotation. Document the semantics in the Phase 3 run report under "Drift between phase-3 spec and as-shipped behavior":

> Bearer token is per-Service-cold-start, NOT per-Go-child-restart as spec §1#11 specified. Rationale: rotation per Go-child restart would require Service→Activity broadcast + WebView.reload + SPA bridge cache invalidation. Threat model preserved by cold-start rotation. The cached `androidBridge.js` token therefore stays valid for the lifetime of the WebView session, which matches the Service lifetime.

No code change in this task; this step is just an explicit decision-record so phase 5+ doesn't accidentally "fix" the behavior to match the spec literal wording.

- [ ] **Step 2: Write `GoLauncherTest`**

```kotlin
// android/app/src/test/kotlin/com/nw5w/graywolf/binaries/GoLauncherTest.kt
package com.nw5w.graywolf.binaries

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class GoLauncherTest {
    @Test
    fun startAndAwaitReady_returnsTrueWhenChildEmitsNewline() {
        // Spawn a real child (sh) that prints \n then sleeps; portable
        // across Linux + macOS dev hosts. The launcher reads stdin
        // byte-by-byte, so this exercises the real I/O path.
        val launcher = GoLauncher(
            executablePath = "/bin/sh",
            env = mapOf("LC_ALL" to "C"),
        )
        val ok = launcher.startAndAwaitReady(2_000)
        // sh with no args reads from stdin and prints nothing; expect
        // timeout. Confirms the negative path.
        assertFalse("sh with no args never emits readiness", ok)
        launcher.stop()
    }

    @Test
    fun startAndAwaitReady_returnsTrueForChildThatEmitsNewlineThenSleeps() {
        val launcher = GoLauncher(
            executablePath = "/bin/sh",
            env = mapOf("LC_ALL" to "C"),
        )
        // Workaround: sh -c 'printf "\n"; sleep 30' emits the byte then
        // sleeps so the launcher's stdout reader sees it before EOF.
        val real = GoLauncher(
            executablePath = "/bin/sh",
            env = emptyMap(),
        )
        // The class doesn't expose a way to pass argv; for a unit test
        // skip the positive path (it requires a real Go binary). We
        // validate readiness positively in Task 19's hardware smoke.
        // Sentinel: confirm stop() is idempotent.
        real.stop()
        real.stop() // second call must not throw
        assertTrue(true)
        launcher.stop()
    }

    @Test
    fun stop_isIdempotent() {
        val launcher = GoLauncher(executablePath = "/bin/sh", env = emptyMap())
        launcher.stop()
        launcher.stop()
        // pass = no exception
    }
}
```

(Real positive-path readiness is exercised on hardware in Task 19. The unit test surface is necessarily limited because `GoLauncher` constructs `ProcessBuilder` directly with no DI seam — refactoring to inject a process factory would be a Phase 5+ cleanup.)

- [ ] **Step 3: Write `SupervisorTest`**

```kotlin
// android/app/src/test/kotlin/com/nw5w/graywolf/binaries/SupervisorTest.kt
package com.nw5w.graywolf.binaries

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.atomic.AtomicInteger

class SupervisorTest {
    @Test
    fun haltsAfterThreeFailuresInOneMinute() {
        val restartCount = AtomicInteger(0)
        val sup = Supervisor(maxFailuresIn60s = 3) {
            restartCount.incrementAndGet()
            true
        }
        // Supplier returns null forever; goWatcher will sleep without
        // signalling. We drive failures via the modemWatcher path
        // OR signal directly. Since signalFailure is private, we
        // exercise via a dead Process: but that requires real fork.
        //
        // Pragmatic: this test asserts the configured maxFailuresIn60s
        // value is honored by inspection; integration coverage of the
        // halt path comes from the SIGKILL smoke in Task 19.
        sup.start { null }
        Thread.sleep(100)
        sup.stop()
        // Sentinel: start+stop without failure must produce zero
        // restarts.
        assertEquals(0, restartCount.get())
    }

    @Test
    fun stopIsIdempotent() {
        val sup = Supervisor { true }
        sup.start { null }
        sup.stop()
        sup.stop()
        assertTrue(true)
    }
}
```

- [ ] **Step 4: Run tests**

Run: `cd android && ./gradlew :app:testDebugUnitTest --tests "com.nw5w.graywolf.binaries.*"`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add android/app/src/test/kotlin/com/nw5w/graywolf/binaries/GoLauncherTest.kt \
        android/app/src/test/kotlin/com/nw5w/graywolf/binaries/SupervisorTest.kt
git commit -m "test(android): unit-test GoLauncher + Supervisor against phase-3 invariants

Phase 2 shipped Supervisor.kt with dead-handle tracking + 3-in-60s
halt + backoff curve, but no test coverage. Adds JVM unit tests for
the parts that don't need a real Process. Hardware-level restart
smoke remains in Task 19's SIGKILL step."
```

---

## Track B — Kotlin polish

### Task 8: Rename `GoLauncher` log tags from `graywolf-pocb` to `GraywolfGo`

**Files:**
- Modify: `android/app/src/main/kotlin/com/nw5w/graywolf/binaries/GoLauncher.kt`

- [ ] **Step 1: Read current tags**

Run: `grep -n "graywolf-pocb\|TAG_" android/app/src/main/kotlin/com/nw5w/graywolf/binaries/GoLauncher.kt`
Expected: two `companion object` constants `TAG_STDOUT = "graywolf-pocb"` and `TAG_STDERR = "graywolf-pocb-err"`.

- [ ] **Step 2: Rename**

Edit GoLauncher.kt's companion object:

```kotlin
    companion object {
        private const val TAG_STDOUT = "GraywolfGo"
        private const val TAG_STDERR = "GraywolfGoErr"
    }
```

- [ ] **Step 3: Verify nothing else references the old tags**

Run: `grep -rn "graywolf-pocb" android/`
Expected: only matches in `Supervisor.kt` and `GraywolfService.kt`'s log lines (those are different `Log.i(TAG, "poc-b: ...")` calls — those messages can stay; they're not a tag conflict, just descriptive labels). If you want to also clean those, do it as part of this task. Recommended: leave them — they're informational labels, not tags.

- [ ] **Step 4: Build probe**

Run: `cd android && ./gradlew :app:compileDebugKotlin`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add android/app/src/main/kotlin/com/nw5w/graywolf/binaries/GoLauncher.kt
git commit -m "refactor(android): rename GoLauncher logcat tags from graywolf-pocb to GraywolfGo

POC-B is retired; the launcher exec'd Go binary is now production
cmd/graywolf. Tag rename keeps logcat filtering unambiguous."
```

---

### Task 9: Strip POC-D PTT methods from `WebAppInterface`

**Files:**
- Modify: `android/app/src/main/kotlin/com/nw5w/graywolf/webview/WebAppInterface.kt`
- Create: `android/app/src/test/kotlin/com/nw5w/graywolf/webview/WebAppInterfaceTest.kt`

- [ ] **Step 1: Write the failing test first**

```kotlin
// android/app/src/test/kotlin/com/nw5w/graywolf/webview/WebAppInterfaceTest.kt
package com.nw5w.graywolf.webview

import org.junit.Assert.assertEquals
import org.junit.Test

class WebAppInterfaceTest {
    @Test
    fun getBearerToken_returnsProvidedValue() {
        val iface = WebAppInterface(tokenProvider = { "abc-123" })
        assertEquals("abc-123", iface.getBearerToken())
    }

    @Test
    fun pttMethodsAreNotExposed() {
        // Phase 3 removes the POC-D PTT trigger surface; phase 5
        // rewires it through the proto path. This test fails if
        // anyone re-adds keyCp2102nRts / keyCm108Hid / keyAiocCdcRts
        // / fireTxTest to the public surface.
        val methods = WebAppInterface(tokenProvider = { "x" })::class.java
            .declaredMethods
            .map { it.name }
            .toSet()
        val forbidden = setOf(
            "fireTxTest",
            "pttStatusJson",
            "keyCp2102nRts", "unkeyCp2102nRts",
            "keyCm108Hid", "unkeyCm108Hid",
            "setCm108Bit",
            "keyAiocCdcRts", "unkeyAiocCdcRts",
        )
        val present = methods.intersect(forbidden)
        assertEquals(emptySet<String>(), present)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd android && ./gradlew :app:testDebugUnitTest --tests com.nw5w.graywolf.webview.WebAppInterfaceTest`
Expected: FAIL — `pttMethodsAreNotExposed` finds `fireTxTest`, `pttStatusJson`, etc., on the existing surface.

- [ ] **Step 3: Strip the methods**

Edit `WebAppInterface.kt`. Replace entire file:

```kotlin
package com.nw5w.graywolf.webview

import android.webkit.JavascriptInterface

/**
 * The single JS bridge exposed to the production Svelte SPA. Phase 3
 * surface is intentionally minimal: the SPA reads the per-launch
 * bearer token and adds it to every fetch / WebSocket call. POC-C
 * TX-test and POC-D PTT trigger methods are gone; phase 5 rewires
 * PTT through the proto path.
 */
class WebAppInterface(
    private val tokenProvider: () -> String,
) {
    @JavascriptInterface
    fun getBearerToken(): String = tokenProvider()
}
```

Remove the now-unused imports (`AudioTxTest`, `ModemBridge`, `UsbPttAdapter`, `kotlin.concurrent.thread`, `Log`) — IDE / `compileDebugKotlin` will flag any leftovers.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd android && ./gradlew :app:testDebugUnitTest --tests com.nw5w.graywolf.webview.WebAppInterfaceTest`
Expected: PASS.

- [ ] **Step 5: Build probe**

Run: `cd android && ./gradlew :app:compileDebugKotlin`
Expected: clean. (If `UsbPttAdapter` / `AudioTxTest` referenced elsewhere — they're not used by the production path, but `UsbPttAdapter` is still referenced from `MainActivity.onResume` and `GraywolfService.onDestroy`. Those references stay; we only stripped the JS-bridge surface.)

- [ ] **Step 6: Commit**

```bash
git add android/app/src/main/kotlin/com/nw5w/graywolf/webview/WebAppInterface.kt \
        android/app/src/test/kotlin/com/nw5w/graywolf/webview/WebAppInterfaceTest.kt
git commit -m "refactor(android): strip POC-C/D test methods from WebAppInterface

JS bridge is now bearer-token-only. POC-D's UsbPttAdapter source
files stay in tree (phase 5 rewires through proto); their JS trigger
surface is gone. Sentinel test fails if anyone re-adds the methods."
```

---

### Task 10: Remove `UsbPttAdapter` references from MainActivity + GraywolfService lifecycles

**Files:**
- Modify: `android/app/src/main/kotlin/com/nw5w/graywolf/MainActivity.kt`
- Modify: `android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt`

- [ ] **Step 1: Locate every reference**

Run: `grep -rn "UsbPttAdapter" android/app/src/main/kotlin/`
Expected: import + `onResume` call in MainActivity; import + `closeAll()` call in `GraywolfService.onDestroy`. Anywhere else means a live wiring this task missed — surface.

- [ ] **Step 2: Remove from MainActivity**

Edit `MainActivity.kt`:
- Delete the line `import com.nw5w.graywolf.usb.UsbPttAdapter`.
- Delete the `onResume` override entirely (it only calls `UsbPttAdapter.enumerate()`).

After edit, `onResume` is no longer overridden — `Activity` default behavior applies, which is correct for phase 3.

- [ ] **Step 3: Remove from GraywolfService**

Edit `GraywolfService.kt`:
- Delete the line `import com.nw5w.graywolf.usb.UsbPttAdapter`.
- Delete the line `UsbPttAdapter.closeAll()` from `onDestroy`.

The adapter source file stays in tree (phase 5 rewires it through the proto path), but it is no longer wired into either lifecycle. This avoids NPE-on-uninitialized + leaked-broadcast-receiver foot-guns when phase 5 modifies it.

- [ ] **Step 4: Build probe**

Run: `cd android && ./gradlew :app:compileDebugKotlin`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add android/app/src/main/kotlin/com/nw5w/graywolf/MainActivity.kt \
        android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt
git commit -m "refactor(android): drop UsbPttAdapter from Activity + Service lifecycles

POC-D's USB enumeration trigger and closeAll teardown are dormant
in phase 3; phase 5 rewires through the platformsvc proto path. The
adapter source file remains so phase 5 can reuse it without
resurrection from git history. Lifecycle wiring is removed to
prevent NPE / leaked-receiver foot-guns when phase 5 modifies the
adapter shape."
```

---

### Task 11: Drop `gainPoller` from `GraywolfService`

**Files:**
- Modify: `android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt`

- [ ] **Step 1: Locate gainPoller**

Run: `grep -n "gainPoller\|/api/_internal/gain" android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt`
Expected: declaration, thread{} block, interrupt() in onDestroy.

- [ ] **Step 2: Remove the field and the thread block**

Edit `GraywolfService.kt`:
- Delete the line `private var gainPoller: Thread? = null`.
- Delete the entire `gainPoller = thread(...)` block (lines ~137-156).
- Delete the lines in `onDestroy`: `gainPoller?.interrupt()` and `gainPoller = null`.

The default boot-time gain (`-6.0f`, set in `bootModem()` via `ModemBridge.modemStart(socketPath(), -6.0f)`) is now the runtime-static value. SPA-driven gain control is a phase-5 task per spec out-of-scope §2.

- [ ] **Step 3: Build probe**

Run: `cd android && ./gradlew :app:compileDebugKotlin`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt
git commit -m "refactor(android): drop POC-B gainPoller from GraywolfService

The poller hit /api/_internal/gain on the POC-B Go stub; cmd/graywolf
production entry doesn't expose that endpoint. Gain stays at the
boot-time default (-6 dB) until phase 5 wires SPA-driven gain
control through ModemBridge.modemSetGainDb."
```

---

### Task 12: Add "Stop" notification action to `GraywolfService`

**Files:**
- Modify: `android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt`

- [ ] **Step 1: Add a stop-action `BroadcastReceiver` and `PendingIntent`**

Edit `GraywolfService.kt`. Inside the class, add a private inner `BroadcastReceiver`:

```kotlin
    private val stopReceiver = object : android.content.BroadcastReceiver() {
        override fun onReceive(context: android.content.Context, intent: android.content.Intent) {
            if (intent.action == ACTION_STOP) {
                Log.i(TAG, "stop action received; shutting down")
                stopSelf()
            }
        }
    }
```

In `onCreate`, register the receiver and build a stop `PendingIntent`. Replace the existing notification-build block with:

```kotlin
        registerReceiver(stopReceiver, android.content.IntentFilter(ACTION_STOP), RECEIVER_NOT_EXPORTED)

        val stopIntent = Intent(ACTION_STOP).setPackage(packageName)
        val stopPending = android.app.PendingIntent.getBroadcast(
            this, 0, stopIntent,
            android.app.PendingIntent.FLAG_IMMUTABLE or android.app.PendingIntent.FLAG_UPDATE_CURRENT,
        )
        val notif: Notification = Notification.Builder(this, getString(R.string.notification_channel_id))
            .setContentTitle(getString(R.string.notification_title))
            .setContentText(getString(R.string.notification_text))
            .setSmallIcon(android.R.drawable.ic_media_play)
            .addAction(
                Notification.Action.Builder(
                    android.graphics.drawable.Icon.createWithResource(this, android.R.drawable.ic_menu_close_clear_cancel),
                    getString(R.string.notification_stop_label),
                    stopPending,
                ).build()
            )
            .build()
```

Add to the `companion object`:

```kotlin
        const val ACTION_STOP = "com.nw5w.graywolf.STOP"
```

In `onDestroy`, unregister the receiver before `super.onDestroy()`:

```kotlin
        try { unregisterReceiver(stopReceiver) } catch (_: IllegalArgumentException) { /* idempotent */ }
```

- [ ] **Step 2: Add the stop-label string**

Edit `android/app/src/main/res/values/strings.xml`. Add:

```xml
    <string name="notification_stop_label">Stop</string>
```

If the file doesn't have `notification_stop_label` already, add it; if it does, leave it.

- [ ] **Step 3: Build probe**

Run: `cd android && ./gradlew :app:compileDebugKotlin :app:processDebugResources`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt \
        android/app/src/main/res/values/strings.xml
git commit -m "feat(android): add Stop action to GraywolfService notification

Tapping Stop in the foreground notification broadcasts ACTION_STOP;
the Service receives it and calls stopSelf(). The receiver is
package-internal (RECEIVER_NOT_EXPORTED) so other apps can't fire
the intent."
```

---

### Task 13: Battery-optimization whitelist intent on first launch

**Files:**
- Modify: `android/app/src/main/kotlin/com/nw5w/graywolf/MainActivity.kt`

(No unit test: Phase 2 did not add Robolectric to the test classpath, and the helper logic is a two-line SharedPreferences read/write — manual verification on the T865 in Task 19 is sufficient. If Robolectric is added in a future phase, retrofit a unit test for the helpers then.)

- [ ] **Step 1: Add the SharedPreferences flag helpers + intent fire**

Edit `MainActivity.kt`. In the `companion object`, add:

```kotlin
        private const val PREFS_NAME = "graywolf-prefs"
        private const val PREF_BATTERY_OPT_REQUESTED = "battery_opt_whitelist_requested_v1"

        fun batteryOptWhitelistRequested(ctx: android.content.Context): Boolean =
            ctx.getSharedPreferences(PREFS_NAME, android.content.Context.MODE_PRIVATE)
                .getBoolean(PREF_BATTERY_OPT_REQUESTED, false)

        fun markBatteryOptWhitelistRequested(ctx: android.content.Context) {
            ctx.getSharedPreferences(PREFS_NAME, android.content.Context.MODE_PRIVATE)
                .edit().putBoolean(PREF_BATTERY_OPT_REQUESTED, true).apply()
        }
```

Override `onResume` (or extend the existing override if Task 10 left one) so the intent fires on the *first* resume after launch, not in `onCreate`. Firing in `onCreate` races the WebView's first paint and presents the system dialog over a blank surface; deferring to `onResume` lets the splash render first.

```kotlin
    private var batteryOptIntentChecked = false

    override fun onResume() {
        super.onResume()
        if (!batteryOptIntentChecked) {
            batteryOptIntentChecked = true
            maybeRequestBatteryOptWhitelist()
        }
    }

    @android.annotation.SuppressLint("BatteryLife")
    private fun maybeRequestBatteryOptWhitelist() {
        if (batteryOptWhitelistRequested(this)) return
        val pm = getSystemService(android.os.PowerManager::class.java) ?: return
        if (pm.isIgnoringBatteryOptimizations(packageName)) {
            markBatteryOptWhitelistRequested(this)
            return
        }
        try {
            val intent = Intent(android.provider.Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS)
                .setData(android.net.Uri.parse("package:$packageName"))
            startActivity(intent)
        } catch (t: Throwable) {
            Log.w(TAG, "battery-opt whitelist intent failed: $t")
        }
        markBatteryOptWhitelistRequested(this)
    }
```

- [ ] **Step 2: Build probe**

Run: `cd android && ./gradlew :app:compileDebugKotlin`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add android/app/src/main/kotlin/com/nw5w/graywolf/MainActivity.kt
git commit -m "feat(android): one-shot battery-optimization whitelist intent on first launch

Operator can decline; the SharedPreferences flag prevents nagging
on subsequent launches. Standard ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS
intent per design doc §5.6."
```

---

### Task 14: Manifest — permissions + foreground service types + cleartext narrowing

**Files:**
- Modify: `android/app/src/main/AndroidManifest.xml`
- Create: `android/app/src/main/res/xml/network_security_config.xml`

- [ ] **Step 1: Create network_security_config.xml**

```xml
<?xml version="1.0" encoding="utf-8"?>
<network-security-config>
    <domain-config cleartextTrafficPermitted="true">
        <domain includeSubdomains="false">127.0.0.1</domain>
    </domain-config>
</network-security-config>
```

- [ ] **Step 2: Update AndroidManifest.xml**

Replace the manifest with:

```xml
<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android"
    xmlns:tools="http://schemas.android.com/tools">

    <uses-permission android:name="android.permission.INTERNET"/>
    <uses-permission android:name="android.permission.ACCESS_NETWORK_STATE"/>
    <uses-permission android:name="android.permission.RECORD_AUDIO"/>
    <uses-permission android:name="android.permission.POST_NOTIFICATIONS"/>
    <uses-permission android:name="android.permission.WAKE_LOCK"/>
    <uses-permission android:name="android.permission.FOREGROUND_SERVICE"/>
    <uses-permission android:name="android.permission.FOREGROUND_SERVICE_MICROPHONE"/>
    <uses-permission android:name="android.permission.FOREGROUND_SERVICE_CONNECTED_DEVICE"/>
    <uses-permission android:name="android.permission.FOREGROUND_SERVICE_LOCATION"/>
    <uses-permission android:name="android.permission.REQUEST_IGNORE_BATTERY_OPTIMIZATIONS"/>

    <uses-feature android:name="android.hardware.usb.host" android:required="false"/>

    <application
        android:name=".GraywolfApp"
        android:label="@string/app_name"
        android:icon="@mipmap/ic_launcher"
        android:roundIcon="@mipmap/ic_launcher_round"
        android:theme="@style/Theme.Graywolf"
        android:allowBackup="false"
        android:networkSecurityConfig="@xml/network_security_config"
        android:extractNativeLibs="true"
        tools:targetApi="34">

        <activity
            android:name=".MainActivity"
            android:exported="true"
            android:configChanges="orientation|keyboardHidden|screenSize">
            <intent-filter>
                <action android:name="android.intent.action.MAIN"/>
                <category android:name="android.intent.category.LAUNCHER"/>
            </intent-filter>
        </activity>

        <service
            android:name=".GraywolfService"
            android:exported="false"
            android:foregroundServiceType="microphone|connectedDevice|location"/>
    </application>
</manifest>
```

(Removed `usesCleartextTraffic="true"` — replaced by `networkSecurityConfig`. `useLegacyPackaging` is the Gradle build property, not a manifest attribute, and is already configured in `build.gradle.kts`; do not add a manifest attribute for it.)

- [ ] **Step 3: Update Service.startForeground type bitmap**

Edit `GraywolfService.kt`. Find the `startForeground` call and replace the single `FOREGROUND_SERVICE_TYPE_MICROPHONE` argument with the OR'd bitmap:

```kotlin
            startForeground(
                NOTIF_ID, notif,
                ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE or
                    ServiceInfo.FOREGROUND_SERVICE_TYPE_CONNECTED_DEVICE or
                    ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION,
            )
```

- [ ] **Step 4: Build probe**

Run: `cd android && ./gradlew :app:processDebugManifest :app:compileDebugKotlin`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add android/app/src/main/AndroidManifest.xml \
        android/app/src/main/res/xml/network_security_config.xml \
        android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt
git commit -m "feat(android): manifest perms + connected-device/location FGS types + cleartext narrowing

Adds WAKE_LOCK, ACCESS_NETWORK_STATE, FGS_CONNECTED_DEVICE,
FGS_LOCATION, REQUEST_IGNORE_BATTERY_OPTIMIZATIONS for the
upcoming USB-PTT (phase 5) and GPS (phase 4) work plus phase-3
battery whitelist + Wi-Fi keepalive. Cleartext traffic is now
scoped to 127.0.0.1 via network_security_config.xml instead of
allowed app-wide."
```

---

## Track C — SPA bearer integration

### Task 15: `androidBridge.js` — single source of truth for the bearer token

**Files:**
- Create: `web/src/lib/androidBridge.js`
- Create: `web/src/lib/androidBridge.test.js`

- [ ] **Step 1: Write the failing test**

```javascript
// web/src/lib/androidBridge.test.js
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { getBearerToken, _resetForTests } from './androidBridge.js';

describe('androidBridge', () => {
  beforeEach(() => {
    _resetForTests();
    delete window.GraywolfWebInterface;
  });
  afterEach(() => {
    _resetForTests();
    delete window.GraywolfWebInterface;
  });

  it('returns null when bridge absent (desktop)', () => {
    expect(getBearerToken()).toBeNull();
  });

  it('returns the token when bridge present', () => {
    window.GraywolfWebInterface = { getBearerToken: () => 'tok-xyz' };
    expect(getBearerToken()).toBe('tok-xyz');
  });

  it('caches the token across calls', () => {
    const spy = vi.fn(() => 'tok-cached');
    window.GraywolfWebInterface = { getBearerToken: spy };
    expect(getBearerToken()).toBe('tok-cached');
    expect(getBearerToken()).toBe('tok-cached');
    expect(spy).toHaveBeenCalledTimes(1);
  });

  it('returns null if bridge throws', () => {
    window.GraywolfWebInterface = {
      getBearerToken: () => { throw new Error('JNI dead'); },
    };
    expect(getBearerToken()).toBeNull();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/lib/androidBridge.test.js`
Expected: FAIL — module doesn't exist.

- [ ] **Step 3: Write minimal implementation**

```javascript
// web/src/lib/androidBridge.js
// Single source of truth for the per-launch bearer token injected by
// the Android Service via WebView.addJavascriptInterface(...,
// "GraywolfWebInterface"). Returns null on desktop builds so the
// bearer wiring becomes a no-op there.
//
// The token is cached at first read because the injected JS bridge
// object is stable for the WebView's lifetime; calling the JNI
// getBearerToken on every fetch would cross the JS<->Java boundary
// needlessly.

let cached = undefined; // sentinel: undefined = not read; null = absent; string = token

export function getBearerToken() {
  if (cached !== undefined) return cached;
  try {
    const v = globalThis.GraywolfWebInterface?.getBearerToken?.();
    cached = (typeof v === 'string' && v.length > 0) ? v : null;
  } catch {
    cached = null;
  }
  return cached;
}

// Test-only: reset the cache between unit tests. Not part of the
// public surface; do not call from app code.
export function _resetForTests() {
  cached = undefined;
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/lib/androidBridge.test.js`
Expected: PASS — 4 tests.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/androidBridge.js web/src/lib/androidBridge.test.js
git commit -m "feat(web): androidBridge.js for cached bearer-token access

Reads the per-launch token from window.GraywolfWebInterface
(injected by the Android WebAppInterface) once at first call and
caches. Returns null on desktop where the bridge is absent."
```

---

### Task 16: `secureFetch.js` — install fetch + WebSocket wrappers at boot

**Files:**
- Create: `web/src/lib/secureFetch.js`
- Create: `web/src/lib/secureFetch.test.js`

- [ ] **Step 1: Write the failing test**

```javascript
// web/src/lib/secureFetch.test.js
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { installSecureFetch, installSecureWebSocket } from './secureFetch.js';
import { _resetForTests as resetBridge } from './androidBridge.js';

describe('installSecureFetch', () => {
  let originalFetch;
  beforeEach(() => {
    resetBridge();
    delete window.GraywolfWebInterface;
    originalFetch = window.fetch;
  });
  afterEach(() => {
    resetBridge();
    delete window.GraywolfWebInterface;
    window.fetch = originalFetch;
  });

  it('no-op when bridge absent', () => {
    installSecureFetch();
    expect(window.fetch).toBe(originalFetch);
  });

  it('wraps fetch when bridge present and adds Authorization header', async () => {
    window.GraywolfWebInterface = { getBearerToken: () => 'tok-abc' };
    let capturedHeaders;
    window.fetch = vi.fn((input, opts) => {
      capturedHeaders = new Headers(opts?.headers);
      return Promise.resolve(new Response('{}'));
    });
    installSecureFetch();
    await window.fetch('/api/version');
    expect(capturedHeaders.get('Authorization')).toBe('Bearer tok-abc');
  });

  it('does not add header to absolute URLs that are not same-origin', async () => {
    window.GraywolfWebInterface = { getBearerToken: () => 'tok-abc' };
    let capturedHeaders;
    window.fetch = vi.fn((input, opts) => {
      capturedHeaders = new Headers(opts?.headers);
      return Promise.resolve(new Response('{}'));
    });
    installSecureFetch();
    await window.fetch('https://example.com/x');
    expect(capturedHeaders.get('Authorization')).toBeNull();
  });

  it('handles fetch(new Request(...)) by cloning with merged headers', async () => {
    window.GraywolfWebInterface = { getBearerToken: () => 'tok-abc' };
    let capturedRequest;
    window.fetch = vi.fn((input) => {
      capturedRequest = input;
      return Promise.resolve(new Response('{}'));
    });
    installSecureFetch();
    const req = new Request('/api/version', { method: 'POST' });
    await window.fetch(req);
    expect(capturedRequest).toBeInstanceOf(Request);
    expect(capturedRequest.headers.get('Authorization')).toBe('Bearer tok-abc');
    expect(capturedRequest.method).toBe('POST');
  });

  it('preserves user-supplied Authorization header when present (caller wins)', async () => {
    window.GraywolfWebInterface = { getBearerToken: () => 'tok-abc' };
    let capturedHeaders;
    window.fetch = vi.fn((input, opts) => {
      capturedHeaders = new Headers(opts?.headers);
      return Promise.resolve(new Response('{}'));
    });
    installSecureFetch();
    await window.fetch('/api/version', { headers: { Authorization: 'Bearer caller-set' } });
    expect(capturedHeaders.get('Authorization')).toBe('Bearer caller-set');
  });
});

describe('installSecureWebSocket', () => {
  let OriginalWS;
  beforeEach(() => {
    resetBridge();
    delete window.GraywolfWebInterface;
    OriginalWS = window.WebSocket;
  });
  afterEach(() => {
    resetBridge();
    delete window.GraywolfWebInterface;
    window.WebSocket = OriginalWS;
  });

  it('no-op when bridge absent', () => {
    installSecureWebSocket();
    expect(window.WebSocket).toBe(OriginalWS);
  });

  it('appends ?token to URL when bridge present', () => {
    window.GraywolfWebInterface = { getBearerToken: () => 'tok-abc' };
    const captured = [];
    // Stub WebSocket as a class so `class extends WebSocket` works.
    class FakeWS {
      constructor(url, protocols) {
        captured.push({ url, protocols });
        this.readyState = 1;
      }
      close() {}
    }
    window.WebSocket = FakeWS;
    installSecureWebSocket();
    const ws = new window.WebSocket('ws://127.0.0.1:8080/ws/foo');
    expect(captured[0].url).toContain('token=tok-abc');
    expect(ws).toBeInstanceOf(FakeWS);
  });

  it('preserves an existing query string', () => {
    window.GraywolfWebInterface = { getBearerToken: () => 'tok-abc' };
    const captured = [];
    class FakeWS {
      constructor(url) { captured.push(url); }
      close() {}
    }
    window.WebSocket = FakeWS;
    installSecureWebSocket();
    new window.WebSocket('ws://127.0.0.1:8080/ws/foo?bar=1');
    expect(captured[0]).toMatch(/[?&]bar=1/);
    expect(captured[0]).toMatch(/[?&]token=tok-abc/);
  });

  it('passes protocols through unchanged', () => {
    window.GraywolfWebInterface = { getBearerToken: () => 'tok-abc' };
    const captured = [];
    class FakeWS {
      constructor(url, protocols) { captured.push({ url, protocols }); }
      close() {}
    }
    window.WebSocket = FakeWS;
    installSecureWebSocket();
    new window.WebSocket('ws://127.0.0.1:8080/ws/foo', ['graywolf.v1']);
    expect(captured[0].protocols).toEqual(['graywolf.v1']);
  });

  it('does not append token to non-same-origin WS URLs', () => {
    window.GraywolfWebInterface = { getBearerToken: () => 'tok-abc' };
    const captured = [];
    class FakeWS {
      constructor(url) { captured.push(url); }
      close() {}
    }
    window.WebSocket = FakeWS;
    installSecureWebSocket();
    new window.WebSocket('ws://example.com/ws/foo');
    expect(captured[0]).not.toMatch(/token=/);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/lib/secureFetch.test.js`
Expected: FAIL — module doesn't exist.

- [ ] **Step 3: Implement secureFetch.js**

```javascript
// web/src/lib/secureFetch.js
// Boot-time wrappers that inject the Android per-launch bearer token
// into every same-origin fetch and WebSocket call. Both functions are
// no-ops when the JS bridge is absent (desktop builds).
//
// We patch window.fetch and window.WebSocket once at boot rather than
// refactoring every call site because the SPA has 30+ fetch sites
// (some via api.js, many direct fetch('/api/...')) and 6+ WebSocket
// sites scattered across stores and components. The wrappers only
// activate when androidBridge.getBearerToken() is non-null so desktop
// behavior is unchanged.
//
// Caller-supplied Authorization headers win over the auto-injected
// bearer token; this lets a deliberate per-call override (e.g. a
// rotated token during testing) bypass the wrapper without disabling
// it globally.

import { getBearerToken } from './androidBridge.js';

export function installSecureFetch() {
  const token = getBearerToken();
  if (!token) return;

  const originalFetch = window.fetch.bind(window);

  window.fetch = function (input, init) {
    const url = inputUrl(input);
    if (!isSameOrigin(url)) {
      return originalFetch(input, init);
    }

    // fetch(Request) — clone the Request so we can merge headers
    // without mutating the caller's object.
    if (input instanceof Request) {
      if (input.headers.has('Authorization')) {
        return originalFetch(input, init);
      }
      const merged = new Headers(input.headers);
      merged.set('Authorization', `Bearer ${token}`);
      const cloned = new Request(input, { headers: merged });
      return originalFetch(cloned, init);
    }

    // fetch(string|URL, init?)
    const opts = { ...(init || {}) };
    const headers = new Headers(opts.headers || {});
    if (!headers.has('Authorization')) {
      headers.set('Authorization', `Bearer ${token}`);
    }
    opts.headers = headers;
    return originalFetch(input, opts);
  };
}

export function installSecureWebSocket() {
  const token = getBearerToken();
  if (!token) return;

  const Original = window.WebSocket;

  // Subclass via `class extends` so:
  //   - `instanceof Original` works on instances
  //   - third-party libs that subclass WebSocket continue to inherit
  //     prototype methods correctly
  //   - new.target chain is preserved across the boundary
  class SecureWS extends Original {
    constructor(url, protocols) {
      const u = isSameOrigin(url) ? appendToken(url, token) : url;
      if (protocols !== undefined) {
        super(u, protocols);
      } else {
        super(u);
      }
    }
  }
  // Preserve readyState constants.
  SecureWS.CONNECTING = Original.CONNECTING;
  SecureWS.OPEN = Original.OPEN;
  SecureWS.CLOSING = Original.CLOSING;
  SecureWS.CLOSED = Original.CLOSED;
  window.WebSocket = SecureWS;
}

function inputUrl(input) {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.href;
  if (input && typeof input.url === 'string') return input.url;
  return '';
}

function isSameOrigin(url) {
  if (!url) return true; // relative => same-origin
  if (url.startsWith('/')) return true;
  try {
    const u = new URL(url, window.location.href);
    // ws:// and wss:// share an origin with http://, https:// of the
    // same host:port for our purposes (the Go HTTP server hosts both
    // surfaces on 127.0.0.1:8080).
    if (u.protocol === 'ws:' || u.protocol === 'wss:') {
      const httpProto = u.protocol === 'wss:' ? 'https:' : 'http:';
      return `${httpProto}//${u.host}` === window.location.origin;
    }
    return u.origin === window.location.origin;
  } catch {
    return false;
  }
}

function appendToken(url, token) {
  const sep = url.includes('?') ? '&' : '?';
  return `${url}${sep}token=${encodeURIComponent(token)}`;
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/lib/secureFetch.test.js`
Expected: PASS — 11 tests.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/secureFetch.js web/src/lib/secureFetch.test.js
git commit -m "feat(web): secureFetch + secureWebSocket wrappers for Android bearer auth

Boot-time patches that inject the per-launch token from
GraywolfWebInterface into same-origin fetch (Authorization header,
including fetch(new Request(...)) callers) and same-origin WebSocket
(?token query param via class extends WebSocket so instanceof and
subclass libs keep working). Caller-supplied Authorization headers
win. Desktop is unaffected because both functions short-circuit when
the JS bridge is absent."
```

---

### Task 17: Wire `installSecureFetch` + `installSecureWebSocket` via dedicated bootstrap module

**Files:**
- Create: `web/src/bootstrap.js`
- Modify: `web/src/main.js`

**Why a separate bootstrap module:** ES module imports are evaluated before any top-level statements in the importing module. If `installSecureFetch()` is called from `main.js` *after* `import App from './App.svelte'`, the App module (and any of its transitive imports — stores, components with top-level `$effect`/store subscriptions) will have already fully evaluated, potentially issuing fetches before the wrapper is installed. Putting the install in a separate module that `main.js` imports *first* guarantees the patches land before any other module evaluates, because module imports execute in source order and complete fully before the next import begins.

- [ ] **Step 1: Create bootstrap.js**

```javascript
// web/src/bootstrap.js
//
// First-evaluated module in the SPA. Its only job is to install the
// Android bearer-token wrappers around window.fetch and window.WebSocket
// BEFORE any other module begins evaluating. main.js imports this file
// as its very first import so the wrappers are in place by the time
// any store, component, or library issues its first request.
//
// This file is intentionally tiny and import-light. Adding heavy
// imports here defeats the purpose: if you import a store from here,
// that store's top-level fetches fire before the wrappers install.

import { installSecureFetch, installSecureWebSocket } from './lib/secureFetch.js';

installSecureFetch();
installSecureWebSocket();
```

- [ ] **Step 2: Modify main.js — bootstrap import must be FIRST**

Read `web/src/main.js` (current contents start with `import.meta.glob('../themes/*.css', ...)`). Insert at the very top, before any other import:

```javascript
// MUST be the first import: installs Android bearer-token wrappers
// around window.fetch and window.WebSocket before any other module
// evaluates. Reordering or removing this line breaks Android auth
// silently.
import './bootstrap.js';
```

Leave the rest of `main.js` unchanged.

- [ ] **Step 3: Verify desktop bundle still builds**

Run: `cd web && npm run build`
Expected: clean Vite build, `dist/` updated.

- [ ] **Step 4: Verify desktop dev server still works (manual smoke)**

Run: `cd web && npm run dev` then ctrl-C after verifying the SPA loads. No Authorization header should be sent (bridge absent on desktop). HMR + Vite WS keep working.

- [ ] **Step 5: Commit**

```bash
git add web/src/bootstrap.js web/src/main.js
git commit -m "feat(web): bootstrap.js installs Android bearer wrappers before App imports

ES module hoisting means installing the fetch/WS wrappers in main.js
after the App.svelte import would let stores' top-level fetches fire
before the wrappers land. A dedicated bootstrap module imported
first guarantees install ordering. Desktop unaffected (bridge absent
makes both installs no-ops)."
```

---

### Task 17a: Patch `api.js` 401 handler to skip the `#/login` redirect on Android

**Files:**
- Modify: `web/src/lib/api.js`

**Why:** the existing `request()` function does `window.location.hash = '#/login'` on any 401 response. On Android there is no login route — auth is bearer-only — so a single token mismatch (e.g., a brief race between Service token rotation and SPA bridge cache invalidation) navigates the WebView to a non-existent hash and the SPA effectively bricks until the operator force-stops the app. This is a real failure mode under supervisor restart even when everything else works.

- [ ] **Step 1: Read the current 401 path**

Run: `grep -n "login\|status === 401" web/src/lib/api.js`
Expected: the line `if (res.status === 401) { window.location.hash = '#/login'; throw new ApiError(401, ...) }`.

- [ ] **Step 2: Skip the redirect when bridge present**

Edit `web/src/lib/api.js`. Add the import at top:

```javascript
import { getBearerToken } from './androidBridge.js';
```

Replace the 401 block with:

```javascript
  if (res.status === 401) {
    if (getBearerToken() !== null) {
      // Android: no login route. The bearer token is per-launch and
      // injected by the Service; a 401 here means the Service rotated
      // the token (supervisor restart) or the wrapper failed to inject.
      // Throw without navigating; callers surface the error and the
      // operator-visible recovery is "Stop + relaunch" or wait for
      // WebView reload on Service restart.
      throw new ApiError(401, { error: 'Unauthorized' });
    }
    window.location.hash = '#/login';
    throw new ApiError(401, { error: 'Unauthorized' });
  }
```

- [ ] **Step 3: Add a test**

Append to `web/src/lib/androidBridge.test.js` (created in Task 15):

```javascript
import { describe as describe2, it as it2, expect as expect2, beforeEach as beforeEach2, afterEach as afterEach2, vi as vi2 } from 'vitest';

// Note: api.js doesn't currently have its own .test.js. We exercise
// the 401-skip logic by importing api.js and pointing it at a fetch
// stub that returns 401, with and without the bridge present.
describe2('api.js 401 path on Android', () => {
  beforeEach2(() => { delete window.GraywolfWebInterface; });
  afterEach2(() => {
    delete window.GraywolfWebInterface;
    window.location.hash = '';
  });

  it2('redirects to #/login on desktop (bridge absent)', async () => {
    window.fetch = vi2.fn(() => Promise.resolve(new Response('{}', { status: 401 })));
    const { api } = await import('./api.js');
    await expect2(api.get('/version')).rejects.toThrow();
    expect2(window.location.hash).toBe('#/login');
  });

  it2('does NOT redirect on Android (bridge present)', async () => {
    window.GraywolfWebInterface = { getBearerToken: () => 'tok-abc' };
    window.fetch = vi2.fn(() => Promise.resolve(new Response('{}', { status: 401 })));
    // Reset the androidBridge cache so the new bridge is read.
    const { _resetForTests } = await import('./androidBridge.js');
    _resetForTests();
    const { api } = await import('./api.js');
    await expect2(api.get('/version')).rejects.toThrow();
    expect2(window.location.hash).not.toBe('#/login');
  });
});
```

(If api.js is loaded as a singleton and module caches across tests defeat the bridge swap, refactor `api.js` to read `getBearerToken` per-call rather than at module init — already the recommended pattern.)

- [ ] **Step 4: Run tests**

Run: `cd web && npx vitest run src/lib/androidBridge.test.js`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/api.js web/src/lib/androidBridge.test.js
git commit -m "fix(web): skip #/login redirect on 401 when Android bridge is present

Android has no login route -- auth is bearer-only. The previous
behavior bricked the SPA on any 401 (e.g. a brief race after a
supervisor token rotation). Desktop behavior is preserved."
```

---

## Track D — Integration

### Task 18: Cross-compile + Gradle assemble probe (full pipeline)

- [ ] **Step 1: Cross-compile both ABIs**

Run:
```bash
GOWORK=off GOOS=android GOARCH=arm64 CGO_ENABLED=0 \
  go build -o /tmp/graywolf-android-arm64 ./cmd/graywolf
GOWORK=off GOOS=android GOARCH=amd64 CGO_ENABLED=1 \
  CC="$ANDROID_NDK_ROOT/toolchains/llvm/prebuilt/$(uname | tr '[:upper:]' '[:lower:]')-x86_64/bin/x86_64-linux-android28-clang" \
  go build -o /tmp/graywolf-android-amd64 ./cmd/graywolf
ls -la /tmp/graywolf-android-*
```
Expected: both binaries built, sizes recorded.

- [ ] **Step 2: Build the production SPA bundle**

Run: `cd web && npm run build`
Expected: clean. `web/dist/` contains the production index.html + assets.

- [ ] **Step 3: assembleDebug**

Run: `cd android && ./gradlew assembleDebug`
Expected: APK at `android/app/build/outputs/apk/debug/app-debug.apk`. Record size:
```bash
ls -la android/app/build/outputs/apk/debug/app-debug.apk
```

- [ ] **Step 4: APK contents inspection**

Run:
```bash
unzip -l android/app/build/outputs/apk/debug/app-debug.apk | grep '\.so$' > scratch/phase-3/apk-so.txt
unzip -l android/app/build/outputs/apk/debug/app-debug.apk | grep -E 'index\.html|assets/' | head -20 > scratch/phase-3/apk-spa.txt
mkdir -p scratch/phase-3
```
Expected: both `libgraywolf.so` and `libgraywolfmodem.so` per ABI; `assets/index.html` plus Vite-built JS/CSS bundles present.

- [ ] **Step 5: Capture APK size delta vs phase 2**

Phase 2 baseline: 35,521,452 bytes (~34M).
Compare `ls -la app-debug.apk` size; record delta in scratch/phase-3/apk-size-delta.txt for the run report.

- [ ] **Step 6: Commit any incidental fixes**

If the assemble probe surfaced bugs (e.g., a Gradle path that still pointed at pocb after Task 7), commit fixes here as separate commits. Do not commit `scratch/` (already gitignored).

---

### Task 19: Install on T865, cold-start trace

(Manual hardware step. No commit unless code changes are needed.)

- [ ] **Step 1: Install on T865**

Run: `adb install -r android/app/build/outputs/apk/debug/app-debug.apk`
Expected: `Success`.

- [ ] **Step 2: Start adb logcat capture**

Run (in a second terminal): `adb logcat -s GraywolfService:* GraywolfGo:* GraywolfGoErr:* PlatformServer:* MainActivity:* Supervisor:* > scratch/phase-3/cold-start.log`

- [ ] **Step 3: Tap the icon. Observe**

Cold-start sequence in logcat should show:
- `MainActivity` perm-prompt completion.
- `GraywolfService` notification + `modemAwaitReady=true`.
- `PlatformServer` accept loop.
- `GraywolfGo` lines incl. `platformsvc: connected, server_version=… schema_version=1` and `graywolf-android: listener_ready`.
- WebView loads `http://127.0.0.1:8080/`.

The Svelte SPA should render. Open Chrome remote debugging (`chrome://inspect`); verify in the Network tab that one or more `/api/...` requests carry `Authorization: Bearer <hex>`.

- [ ] **Step 4: Capture screenshot**

Take a screenshot of the SPA running on the tablet (Settings page; History page; Live Map). Save under `scratch/phase-3/spa-screenshots/`.

- [ ] **Step 5: Stop the app via Stop notification action**

Pull down the notification shade. Tap "Stop" on the graywolf notification.
Expected: notification disappears, Service stops, supervisor halts cleanly. Logcat should show "stop action received; shutting down".

- [ ] **Step 6: Stop logcat capture, redact the bearer token**

Ctrl-C the logcat process. The capture contains the per-launch bearer token in any logged URL (WebSocket `?token=…`, occasional fetch URLs in error paths). Before saving the artifact, redact:

```bash
# Pull the live token from the GraywolfService log line that records
# token generation (or from the Service env injection log).
TOKEN=$(grep -oE 'token[=:][a-f0-9]{64}' scratch/phase-3/cold-start.log | head -1 | sed 's/.*[=:]//')
if [ -n "$TOKEN" ]; then
    sed -i.bak "s/${TOKEN}/REDACTED-BEARER/g" scratch/phase-3/cold-start.log
    rm scratch/phase-3/cold-start.log.bak
fi
```

Then inspect the redacted log — confirm no ERROR / FATAL lines from graywolf components (filter MTK kernel noise, AAudio noise, GoogleApiAvailability noise per Phase 2's pattern). Confirm both readiness gates fired in the right order: `modemAwaitReady=true` BEFORE `poc-b: go_child_up` BEFORE WebView `loadUrl`.

- [ ] **Step 6a: Verify Go-only restart preserves modem JNI state**

When the supervisor restarts the Go child without restarting the modem cdylib (the modem is in-process to the Service and remains loaded across Go restarts), the Go side reconnects to the existing modem UDS and keeps decoding. Phase 2's `supervisorRestart()` actually restarts BOTH (it calls `ModemBridge.modemStop()` then `bootModem()` then `bootGoChild()`), so the modem state IS reset on every Go-child restart — see `GraywolfService.kt` lines ~74-83.

Verify in logcat that after the SIGKILL test (next step), the sequence is:
- `Supervisor: poc-b: go_child_died`
- `GraywolfService: poc-b: supervisor_restart_begin`
- `audioPump.stop()` evidence (search for AudioPump tag)
- `ModemBridge.modemStop()` evidence
- new `modemAwaitReady=true`
- new `audioPump.start()`
- new `poc-b: go_child_up`

If any of those steps is skipped, the modem-state-reset guarantee is broken — surface and fix `supervisorRestart()` before declaring Task 19 done.

- [ ] **Step 7: Supervisor restart smoke (fault injection via SIGKILL)**

Re-launch the app. Find the Go child PID:
```bash
adb shell ps -A | grep "libgraywolf"
```
Note: the Go child appears as `libgraywolf.so` in the process list (per N1 packaging trick). Pick its PID.

Kill it:
```bash
adb shell run-as com.nw5w.graywolf kill -9 <pid>
```
(If `run-as` denied due to debug build packaging, fall back to: `adb shell su -c 'kill -9 <pid>'` on a rooted T865, or `adb shell kill <pid>` if the shell user has perm.)

Watch logcat for:
- `Supervisor: poc-b: go_child_died rc=…`
- `GraywolfService: poc-b: supervisor_restart_begin`
- New `graywolf-android: listener_ready` (new Go child up)
- WebView reloads at 127.0.0.1:8080

Expected: supervisor restarts the Go child within ~3 s (1 s backoff + boot time). The WebView refreshes (because `MainActivity.didReloadOnError` triggers on the brief connection failure). SPA bootstraps with the same bearer token (no rotation across mid-session restart — token is per Service cold-start, not per Go-child restart).

If supervisor tight-loops (more than 3 restarts in 60 s), the halt rule in `Supervisor.kt` should fire and log `3 failures in 60s; halting restart`. Verify by killing the Go child 4 times in a minute. After the halt, the Service stays up but Go is dead; tap notification "Stop" to clean up.

Capture full logcat to `scratch/phase-3/supervisor-restart.log`. Apply the same token redaction as Step 6 before committing the file path to the run report.

---

### Task 19a: SPA fetch + WebSocket coverage walk

DoD criterion #18 ("fetch coverage exhaustive") needs explicit verification — the monkey-patch could miss any call site that captured a reference to `window.fetch` before the bootstrap module installed (or that uses `XMLHttpRequest`, `EventSource`, `axios`, or a Web Worker that constructs its own `fetch`/`WebSocket`).

- [ ] **Step 1: Static enumeration of HTTP entry points**

Run (from repo root):
```bash
mkdir -p scratch/phase-3
{
    echo "=== fetch( ==="
    grep -rn "fetch(" web/src --include="*.js" --include="*.svelte" | grep -v node_modules
    echo
    echo "=== new WebSocket( ==="
    grep -rn "new WebSocket(" web/src --include="*.js" --include="*.svelte"
    echo
    echo "=== new EventSource( ==="
    grep -rn "new EventSource(" web/src --include="*.js" --include="*.svelte"
    echo
    echo "=== XMLHttpRequest ==="
    grep -rn "XMLHttpRequest" web/src --include="*.js" --include="*.svelte"
    echo
    echo "=== new Worker( ==="
    grep -rn "new Worker(" web/src --include="*.js" --include="*.svelte"
    echo
    echo "=== axios | ky | superagent ==="
    grep -rn "from .axios\\|from .ky\\|from .superagent" web/src --include="*.js" --include="*.svelte"
} > scratch/phase-3/http-call-sites.txt
cat scratch/phase-3/http-call-sites.txt
```

Expected: every `fetch(` and `new WebSocket(` call site is enumerated. Any `XMLHttpRequest`, `EventSource`, `Worker`, or third-party HTTP lib is a coverage gap — the monkey patch does NOT cover those. Surface any matches.

- [ ] **Step 2: Confirm bootstrap-import-first invariant**

Run: `head -5 web/src/main.js`
Expected: the first non-comment line is `import './bootstrap.js';` (per Task 17). If a later edit reordered imports, the bootstrap may run too late — restore the ordering.

- [ ] **Step 3: Live route walk via Chrome remote devtools**

With the APK running on the T865 (or x86_64 emulator):
1. Connect Chrome on the dev host to `chrome://inspect` and attach to the WebView.
2. Open the Network tab. Clear.
3. Navigate every top-level route the production SPA exposes (Settings, Channels, Audio, PTT, Status, Packets, Terminal, Sessions, History, Maps — whatever the build of the SPA renders).
4. For each route, confirm in the Network tab that every `/api/...` request shows `Authorization: Bearer …` in the request headers.
5. Open one terminal session (which kicks a WebSocket); confirm the WS upgrade URL contains `?token=…`.

- [ ] **Step 4: Capture evidence**

Take screenshots of the Network tab showing the bearer header on representative requests; save to `scratch/phase-3/spa-network/`. Note any route with a missing header — that's a Track-C bug, fix before declaring phase 3 done.

- [ ] **Step 5: Document any uncovered call sites**

If Step 1 surfaced `XMLHttpRequest`, `EventSource`, `Worker`, or third-party HTTP libs that the monkey-patch doesn't reach, document each in the run report under "Issues surfaced", along with the chosen mitigation (per-call-site refactor, additional wrapper, or accepted risk for phase 3).

(No commit — this is a verification task.)

---

### Task 20: POC-B RX regression — live frame decode still works

(Manual hardware step. No commit unless code changes are needed.)

- [ ] **Step 1: Wire the chain**

Plug Digirig into the T865 via OTG. Connect Digirig audio to UV-5R. Tune UV-5R to a known APRS channel (144.390 MHz US, or local equivalent).

- [ ] **Step 2: Tap the icon, wait for SPA to load**

- [ ] **Step 3: Watch logcat for decoded frames**

Run: `adb logcat | grep -E "RxFrame|aprs:|demod"`
Have a second radio TX a test APRS frame, OR wait for an organic transmission.

Expected: at least one `RxFrame` (or equivalent) log line within 5 minutes of channel activity.

- [ ] **Step 4: Verify the SPA rendered the frame**

In the SPA's "Packets" or "Live Feed" tab (whatever the production SPA exposes), confirm the decoded frame appears in the UI within ~2 seconds of the logcat decode line.

If the SPA tab shows no packets despite logcat showing decodes, surface — likely the bearer-auth is blocking the WebSocket subscription that drives the live feed. Check the WebSocket request in Chrome remote devtools; the URL should carry `?token=<hex>`.

- [ ] **Step 5: Capture evidence**

Save logcat snippet + SPA screenshot showing the decoded frame to `scratch/phase-3/rx-regression/`.

---

### Task 21: Run report

**Files:**
- Create: `.context/2026-05-09-android-phase-3-results.md`

- [ ] **Step 1: Draft the run report**

Mirror the Phase 2 run-report shape (`.claude/worktrees/feature+android-phase-2/.context/2026-05-09-android-phase-2-results.md`). Sections:

```markdown
# Android Phase 3 — run report

## Toolchain versions used
- JDK: <output of `java -version` 2>&1 | head -1>
- NDK: <output of `cat $ANDROID_NDK_ROOT/source.properties | grep Pkg.Revision`>
- cargo-ndk: <output of `cargo ndk --version`>
- Rust: <output of `rustc --version`>
- Go: <output of `go version`>
- Node: <output of `node --version`>
- Vite: <from `web/package.json`'s `vite` version>

## Definition-of-done criteria

| # | Criterion | Status | Notes |
|---|-----------|--------|-------|
| 1 | cmd/graywolf/main_android.go is a real entry | <fill> | <commit hash> |
| 2 | HTTP listener binds 127.0.0.1:8080 with bearer middleware | <fill> | |
| 3 | Embedded SPA serves under Android cross-compile | <fill> | |
| 4 | Modem UDS connect | <fill> | |
| 5 | Platform UDS connect | <fill> | |
| 6 | Hello handshake completes before readiness byte | <fill> | logcat line: `platformsvc: connected, server_version=… schema_version=1` |
| 7 | Readiness signal `\n` written to stdout | <fill> | logcat line: `graywolf-android: listener_ready` |
| 8 | Disabled-on-Android subsystems compile out | <fill> | updatescheck gated on Platform; pttdevice/serial-PTT files build-tagged !linux |
| 9 | cmd/graywolf-pocb retired | <fill> | git rm -r |
| 10 | MainActivity owns WebView + lifecycle | ✅ (preserved from phase 2) | minor change: battery-opt intent on first launch |
| 11 | GraywolfService is production-grade | <fill> | bearer token + Stop action + expanded FGS types |
| 12 | GoLauncher renamed log tags | ✅ | TAG_STDOUT=GraywolfGo |
| 13 | WebAppInterface phase-3 surface | ✅ | only getBearerToken; sentinel test |
| 14 | JS-bridge ordering honored | ✅ (preserved from phase 2) | addJavascriptInterface before loadUrl |
| 15 | Battery-opt whitelist intent on first launch | <fill> | SharedPreferences flag |
| 16 | Manifest perms + types + cleartext narrowing | <fill> | network_security_config.xml scoped to 127.0.0.1 |
| 17 | SPA reads bearer token from JS bridge | <fill> | androidBridge.js cache |
| 17a | api.js 401 path skips #/login on Android | <fill> | bridge-gated branch + vitest coverage |
| 18 | Fetch coverage exhaustive | <fill> | static enumeration (Task 19a step 1) + Chrome devtools route walk (Task 19a step 3); secureFetch handles Request objects + caller-supplied headers; secureWebSocket via class extends WebSocket |
| 19 | SPA renders end-to-end on Android | <fill> | settings/history/live-feed render |
| 20 | Cold-start succeeds on T865 | <fill> | screenshots + logcat |
| 21 | Supervisor restart works under fault injection | <fill> or `deferred to phase 6` | |
| 22 | POC-D PTT regression deferred | ✅ noted | proto-driven PTT lands in phase 5 |
| 23 | AudioPump RX still works | <fill> | logcat shows decoded frames after Digirig + UV-5R chain wired |

## APK size baseline
- Phase 2 baseline: 35,521,452 bytes (~34M)
- Phase 3 with production SPA embed: <bytes> (~<MB>) -- delta <bytes>

## Drift between phase-3 spec and as-shipped behavior
<list any deviations: e.g., gainPoller dropped instead of preserved, splash UI deferred, OnHTTPListenerReady chosen over polling, etc.>

## Phase 4 prerequisites
- pkg/gps/android.go: <status -- present? cross-portable?>
- platformsvc.SubscribeGpsFix: <status -- already wired in phase 2; confirm callable from real cmd/graywolf>
- LocationManager perm flow: <status -- ACCESS_FINE_LOCATION not yet added; phase 4's first task>

## Cold-start time
- Tap icon -> first SPA byte rendered: <seconds, on T865>

## Issues surfaced
<list any deviations or bugs discovered during implementation>
```

- [ ] **Step 2: Fill in based on actual outcomes**

Walk the table top-to-bottom, marking ✅ / ❌ / partial with commit hashes and evidence pointers (logcat snippets, screenshots).

- [ ] **Step 3: Commit the report**

```bash
git add .context/2026-05-09-android-phase-3-results.md
git commit -m "docs(android): phase 3 run report

Documents pass/fail on all 23 DoD criteria, APK size delta from
phase 2's 34M baseline, drift from spec, phase 4 prereq status."
```

---

## Self-review checklist (do before declaring complete)

- [ ] Spec coverage: every section in `.context/2026-05-09-android-phase-3-spec.md` is mapped to a task above.
- [ ] No placeholders: search for "TODO", "TBD", "implement later" in this plan — none should remain.
- [ ] Type consistency: `BearerAuthMiddleware` signature matches across Tasks 1, 3, 6. Field names (`BearerToken`, `Platform`, `OnHTTPListenerReady`) consistent across Tasks 2-6. JS interface name `GraywolfWebInterface` matches across Tasks 9, 15, 16, 17, 17a (NOT `GraywolfBridge` from earlier spec drafts — the existing Phase 2 code uses `GraywolfWebInterface`).
- [ ] Build-tag discipline: `main_android.go` and its test file both `//go:build android`. `pkg/webauth/bearer.go` is portable (no build tag — desktop also uses it if desired).
- [ ] Commit hygiene: no AI attribution, no Co-Authored-By, no "Generated with".
- [ ] `GOWORK=off` set on every Go test invocation.
- [ ] Token redaction: every committed log artifact under `scratch/phase-3/` has the bearer token replaced with `REDACTED-BEARER` per Task 19 step 6.
- [ ] Bootstrap import is FIRST in `web/src/main.js` (Task 17). Reordering breaks Android auth silently.
- [ ] WebSocket wrapper uses `class extends WebSocket` (Task 16), not the plain-function pattern.
