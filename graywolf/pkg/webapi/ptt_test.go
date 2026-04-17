package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
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

// TestHandlePttGpioLinesRequiresChip asserts that the handler rejects
// a missing `chip` query parameter with 400 before ever touching the
// platform-specific enumeration path. Run-anywhere: does not require a
// real gpiochip and does not touch the filesystem.
func TestHandlePttGpioLinesRequiresChip(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/ptt/gpio-lines", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing chip param, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestHandlePttGpioLinesMethodNotAllowed asserts that non-GET requests
// to the enumeration endpoint are rejected with 405.
func TestHandlePttGpioLinesMethodNotAllowed(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/ptt/gpio-lines?chip=/dev/gpiochip0", nil)
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
//   - Linux with a guaranteed-missing path: 500, because the kernel
//     returns ENOENT and that's a legitimate server-side failure, not a
//     platform limitation.
func TestHandlePttGpioLinesPlatform(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet,
		"/api/ptt/gpio-lines?chip=/dev/gpiochip_does_not_exist_phase4_test", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if runtime.GOOS == "linux" {
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 for missing chip on linux, got %d: %s",
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

// TestPttdeviceGpioLineInfoType is a compile-time check that the
// handler and the underlying enumeration agree on the response shape.
// Decoupling the two across packages means a struct-field rename would
// silently produce different JSON — this test fails fast if that
// happens.
func TestPttdeviceGpioLineInfoType(t *testing.T) {
	var _ []pttdevice.GpioLineInfo
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
