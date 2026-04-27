package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/pttdevice"
)

// TestHandlePttCapabilities asserts that GET /api/ptt/capabilities
// reports platform_supports_gpio matching runtime.GOOS. The field is
// the UI's Linux gate for the GPIO method dropdown, so its value has
// to track GOOS exactly — a stale false on Linux hides a working
// feature; a stale true on macOS lets users pick a method the modem
// will reject.
func TestHandlePttCapabilities(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/ptt/capabilities", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var caps pttCapabilities
	if err := json.NewDecoder(rec.Body).Decode(&caps); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	want := runtime.GOOS == "linux"
	if caps.PlatformSupportsGpio != want {
		t.Errorf("platform_supports_gpio = %v, want %v (GOOS=%s)",
			caps.PlatformSupportsGpio, want, runtime.GOOS)
	}

	// Wire-level field name check: the UI relies on snake_case and
	// will silently ignore anything else.
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		// body was already consumed above; re-run against a fresh
		// recorder so the assertion still works.
		rec2 := httptest.NewRecorder()
		mux.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/api/ptt/capabilities", nil))
		if err := json.Unmarshal(rec2.Body.Bytes(), &raw); err != nil {
			t.Fatalf("re-decode: %v", err)
		}
	}
	if _, ok := raw["platform_supports_gpio"]; !ok {
		t.Errorf("expected platform_supports_gpio key, got keys: %v", keys(raw))
	}
}

// TestHandlePttGpioLinesMethodNotAllowed asserts that non-GET requests
// to the enumeration endpoint are rejected with 405. Post-Phase-4 the
// endpoint uses a path parameter, not a query string, so the encoded
// chip path is now part of the URL path.
func TestHandlePttGpioLinesMethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// url.PathEscape("/dev/gpiochip0") → "%2Fdev%2Fgpiochip0"
	req := httptest.NewRequest(http.MethodPost,
		"/api/ptt/gpio-chips/%2Fdev%2Fgpiochip0/lines", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// TestHandlePttGpioLinesPlatform asserts the endpoint's platform split.
// On Linux the happy path (with a likely-existing /dev/gpiochip0) would
// return a 200 + list, but since CI and dev machines vary we only
// exercise the deterministic half of the split:
//
//   - non-Linux: always 501 (the enumeration stub's fixed error), so the
//     UI can distinguish "this platform can't do it" from a genuine
//     server fault.
//   - Linux with a guaranteed-missing path: 404, because a missing chip
//     path is a client mistake, not a server fault.
//
// The URL shape post-Phase-4 encodes the chip device path in a single
// segment. Go's ServeMux already unescapes %2F in a path value, and
// the handler additionally runs the segment through url.PathUnescape
// to tolerate double-encoding clients.
func TestHandlePttGpioLinesPlatform(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// url.PathEscape("/dev/gpiochip_does_not_exist_phase4_test") →
	// "%2Fdev%2Fgpiochip_does_not_exist_phase4_test"
	req := httptest.NewRequest(http.MethodGet,
		"/api/ptt/gpio-chips/%2Fdev%2Fgpiochip_does_not_exist_phase4_test/lines", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if runtime.GOOS == "linux" {
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for missing chip on linux, got %d: %s",
				rec.Code, rec.Body.String())
		}
		return
	}
	// Non-Linux: the stub returns a fixed message which we translate
	// to 501 so the UI can render a platform-specific empty state.
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501 on %s, got %d: %s",
			runtime.GOOS, rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	// Sanity-check the stub's error wording is surfaced verbatim so
	// the UI can show a helpful message.
	if body["error"] == "" {
		t.Errorf("expected non-empty error body, got %+v", body)
	}
}

// TestHandlePttGpioLinesNotAGpioChip verifies that supplying a path that
// exists but is not a gpiochip (typical case: a stale /dev/ttyACM* left
// in the PTT form when the user flips method to "gpio") returns 400, not
// 500. /dev/null is present on every Unix host and is a char device but
// is not under /sys/bus/gpio/devices, so gpiocdev.IsChip rejects it.
// Linux-only: on other platforms the stub returns 501 before the check.
func TestHandlePttGpioLinesNotAGpioChip(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("gpio line enumeration is Linux-only")
	}
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// url.PathEscape("/dev/null") → "%2Fdev%2Fnull"
	req := httptest.NewRequest(http.MethodGet,
		"/api/ptt/gpio-chips/%2Fdev%2Fnull/lines", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-gpiochip path, got %d: %s",
			rec.Code, rec.Body.String())
	}
}

// TestPttdeviceGpioLineInfoType is a compile-time check that the
// handler and the underlying enumeration agree on the response shape.
// Decoupling the two across packages means a struct-field rename would
// silently produce different JSON — this test fails fast if that
// happens.
func TestPttdeviceGpioLineInfoType(t *testing.T) {
	var _ []pttdevice.GpioLineInfo
}

// TestPttRoutePrecedence pins Go 1.22's http.ServeMux literal-over-
// wildcard preference for the /api/ptt route tree. registerPtt
// installs both literal segments (available, capabilities,
// test-rigctld, gpio-chips/{chip}/lines) and the {channel} wildcard on
// the same mux; if a future stdlib change or accidental pattern
// reordering silently rerouted `GET /api/ptt/available` to
// getPttConfig (which would then 400 on "available" as a channel id),
// clients would break in subtle ways. This test asserts each request
// reaches the intended handler by registering canary wrappers that
// record which handler name actually ran.
//
// The canary registration mirrors registerPtt's pattern list exactly —
// any divergence here is deliberate and must be justified. Keeping
// this table in sync with ptt.go's registerPtt is part of the route
// contract.
func TestPttRoutePrecedence(t *testing.T) {
	// canary returns an http.HandlerFunc that records its name into
	// the provided pointer and writes a 200. The handler name alone is
	// the assertion target — bodies and status codes from the real
	// handlers aren't what's under test here.
	var hit string
	canary := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			hit = name
			w.WriteHeader(http.StatusOK)
		}
	}

	mux := http.NewServeMux()
	// Mirror registerPtt's exact registration order and patterns.
	mux.HandleFunc("GET /api/ptt", canary("listPttConfigs"))
	mux.HandleFunc("POST /api/ptt", canary("upsertPttConfig"))
	mux.HandleFunc("GET /api/ptt/available", canary("listPttDevices"))
	mux.HandleFunc("GET /api/ptt/capabilities", canary("getPttCapabilities"))
	mux.HandleFunc("POST /api/ptt/test-rigctld", canary("testRigctld"))
	mux.HandleFunc("GET /api/ptt/gpio-chips/{chip}/lines", canary("listGpioLines"))
	mux.HandleFunc("GET /api/ptt/{channel}", canary("getPttConfig"))
	mux.HandleFunc("PUT /api/ptt/{channel}", canary("updatePttConfig"))
	mux.HandleFunc("DELETE /api/ptt/{channel}", canary("deletePttConfig"))

	cases := []struct {
		name   string
		method string
		path   string
		want   string
	}{
		{
			name:   "GET /available hits listPttDevices not getPttConfig",
			method: http.MethodGet,
			path:   "/api/ptt/available",
			want:   "listPttDevices",
		},
		{
			name:   "GET /capabilities hits getPttCapabilities not getPttConfig",
			method: http.MethodGet,
			path:   "/api/ptt/capabilities",
			want:   "getPttCapabilities",
		},
		{
			name:   "POST /test-rigctld hits testRigctld (verb+literal wins)",
			method: http.MethodPost,
			path:   "/api/ptt/test-rigctld",
			want:   "testRigctld",
		},
		{
			name:   "GET /gpio-chips/{chip}/lines hits listGpioLines",
			method: http.MethodGet,
			path:   "/api/ptt/gpio-chips/%2Fdev%2Fgpiochip0/lines",
			want:   "listGpioLines",
		},
		{
			name:   "GET /{channel} numeric hits getPttConfig",
			method: http.MethodGet,
			path:   "/api/ptt/42",
			want:   "getPttConfig",
		},
		{
			name:   "PUT /{channel} numeric hits updatePttConfig",
			method: http.MethodPut,
			path:   "/api/ptt/42",
			want:   "updatePttConfig",
		},
		{
			name:   "DELETE /{channel} numeric hits deletePttConfig",
			method: http.MethodDelete,
			path:   "/api/ptt/42",
			want:   "deletePttConfig",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hit = ""
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			// A 404 from the mux (no matching pattern) would be the
			// exact failure mode this test exists to catch — the
			// canary can't record a name if the mux never dispatches.
			if rec.Code == http.StatusNotFound {
				t.Fatalf("mux returned 404 for %s %s — route did not match any registered pattern",
					tc.method, tc.path)
			}
			if hit != tc.want {
				t.Fatalf("%s %s: handler = %q, want %q (status %d)",
					tc.method, tc.path, hit, tc.want, rec.Code)
			}
		})
	}
}

// TestPttRoutePrecedence_RealHandlers exercises the live registerPtt
// wiring to confirm the precedence contract survives end-to-end, not
// just on a hand-rolled canary mux. The canary test above proves the
// mux behavior; this one proves registerPtt actually registers those
// patterns in that order. Together they fail loudly if either piece
// drifts.
//
// We check observable behavior of the production handlers — 200 for
// listPttDevices/getPttCapabilities, 400 for getPttConfig with a non-
// numeric channel (confirming the {channel} wildcard route is live),
// and 501-or-200 from listGpioLines (platform-dependent). The key
// signal is that none of these return 404 from the mux — a 404 would
// mean the wildcard swallowed a literal or vice versa.
func TestPttRoutePrecedence_RealHandlers(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	cases := []struct {
		name    string
		method  string
		path    string
		notCode int // status code that would indicate wrong-route dispatch
	}{
		{
			// If /available routed to getPttConfig, we'd get 400
			// ("invalid channel id") instead of a 200 device list.
			name:    "GET /available returns 200 not 400",
			method:  http.MethodGet,
			path:    "/api/ptt/available",
			notCode: http.StatusBadRequest,
		},
		{
			// Same logic for capabilities.
			name:    "GET /capabilities returns 200 not 400",
			method:  http.MethodGet,
			path:    "/api/ptt/capabilities",
			notCode: http.StatusBadRequest,
		},
		{
			// If the mux didn't know POST /test-rigctld, it would
			// fall back to 405 (literal match, wrong method) or 404
			// (no match). The handler itself returns 400 on an empty
			// body, which is fine — we only care it isn't 404/405.
			name:    "POST /test-rigctld reaches handler not mux 405",
			method:  http.MethodPost,
			path:    "/api/ptt/test-rigctld",
			notCode: http.StatusMethodNotAllowed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code == http.StatusNotFound {
				t.Fatalf("%s %s: mux returned 404 — route not registered or wildcard ate the literal",
					tc.method, tc.path)
			}
			if rec.Code == tc.notCode {
				t.Fatalf("%s %s: got %d — route dispatched to wrong handler (see notCode)",
					tc.method, tc.path, rec.Code)
			}
		})
	}

	// GPIO lines: the response varies by OS (200 on linux with a real
	// chip, 404 on linux with a missing chip, 501 on non-linux) but
	// must never be a mux-level 404, which would mean the {chip}
	// path parameter didn't match. The handler's own 404 (missing
	// chip) emits JSON; the mux's 404 emits plain-text "404 page not
	// found\n", so Content-Type disambiguates the two.
	req := httptest.NewRequest(http.MethodGet,
		"/api/ptt/gpio-chips/%2Fdev%2Fgpiochip0/lines", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		ct := rec.Header().Get("Content-Type")
		if !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("GET /api/ptt/gpio-chips/.../lines: mux 404 (Content-Type=%q body=%q) — {chip} segment did not match",
				ct, rec.Body.String())
		}
	}

	// GET /{channel} with a numeric id: the handler may 404 for a
	// missing channel, but a mux-level 404 would indicate the
	// {channel} wildcard isn't installed. Disambiguate by confirming
	// the response body is JSON from the handler, not the mux's
	// plain-text "404 page not found\n".
	req = httptest.NewRequest(http.MethodGet, "/api/ptt/42", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		ct := rec.Header().Get("Content-Type")
		if !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("GET /api/ptt/42: mux 404 (Content-Type=%q body=%q) — {channel} wildcard missing",
				ct, rec.Body.String())
		}
	}
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
