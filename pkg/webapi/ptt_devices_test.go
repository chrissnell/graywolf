package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chrissnell/graywolf/pkg/pttdevice"
)

// TestListPttDevicesShapeOnDesktop asserts the /api/ptt/available
// response shape carries the fields the new SPA depends on:
// `recommended`, `type`, and `path`.
//
// Tasks 5.1/5.2 added new Android-only `usb-*` types. This test locks
// the desktop contract so a future struct rename or accidental type
// promotion can't silently break the SPA, which keys its UI off `type`
// and `recommended`.
func TestListPttDevicesShapeOnDesktop(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/ptt/available", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var out []struct {
		Path        string `json:"path"`
		Type        string `json:"type"`
		Recommended bool   `json:"recommended"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Desktop hosts may legitimately have zero PTT devices in a CI
	// container — just assert the contract holds for whatever shows up.
	for _, d := range out {
		switch d.Type {
		case "serial", "gpio", "cm108":
			// recognized desktop types
		default:
			t.Errorf("unexpected desktop type: %q", d.Type)
		}
	}
}

// TestListPttDevicesUsesInjectedSource asserts that when a PttDeviceSource
// is installed (as it is on Android in pkg/app/wiring.go), the handler
// returns its rows instead of falling back to pttdevice.Enumerate().
// Locks the dispatch contract — a regression that silently bypassed the
// injection would show up here.
func TestListPttDevicesUsesInjectedSource(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.SetPttDeviceSource(fakePttDeviceSource{rows: []pttdevice.AvailableDevice{
		{Path: "fake://injected", Type: "usb-cp2102n", Name: "Test Fixture", Recommended: true},
	}})
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/ptt/available", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var out []struct {
		Path        string `json:"path"`
		Type        string `json:"type"`
		Name        string `json:"name"`
		Recommended bool   `json:"recommended"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("injected source should return 1 row; got %d", len(out))
	}
	if out[0].Path != "fake://injected" || out[0].Type != "usb-cp2102n" {
		t.Errorf("injected source not consulted; got %+v", out[0])
	}
}

type fakePttDeviceSource struct {
	rows []pttdevice.AvailableDevice
}

func (f fakePttDeviceSource) ListPttDevices(_ context.Context) []pttdevice.AvailableDevice {
	return f.rows
}
