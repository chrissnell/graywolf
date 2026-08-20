package webapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/webtypes"
)

// fakeBLEScanner is a test double for BLEMobilinkdScanner.
type fakeBLEScanner struct {
	devices []BLEMobilinkdDevice
	err     error
	delay   time.Duration // per-device delay to simulate real scanning
}

func (f *fakeBLEScanner) Scan(ctx context.Context, discovered func(BLEMobilinkdDevice)) error {
	if f.err != nil {
		return f.err
	}
	for _, d := range f.devices {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if f.delay > 0 {
			select {
			case <-time.After(f.delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		discovered(d)
	}
	return nil
}

// TestBLEMobilinkdScan_NoSource returns 501 when no scanner is wired.
func TestBLEMobilinkdScan_NoSource(t *testing.T) {
	srv, _ := newTestServer(t)
	// No SetBLEMobilinkdScanner call — scanner is nil.

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/kiss/ble-mobilinkd-scan", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %s", rec.Code, rec.Body)
	}
	var body webtypes.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if body.Error == "" {
		t.Fatal("expected non-empty error message")
	}
}

// TestBLEMobilinkdScan_StreamsDevices verifies that each discovered
// peripheral arrives as a separate SSE "data:" line, and that a
// "event: done" frame is emitted when scanning completes.
func TestBLEMobilinkdScan_StreamsDevices(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.SetBLEMobilinkdScanner(&fakeBLEScanner{
		devices: []BLEMobilinkdDevice{
			{Addr: "AA:BB:CC:DD:EE:01", Name: "Mobilinkd TNC4", RSSI: -60},
			{Addr: "AA:BB:CC:DD:EE:02", Name: "Mobilinkd TNC3", RSSI: -72},
		},
	})

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/kiss/ble-mobilinkd-scan?timeout=2", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("expected text/event-stream content-type, got %q", ct)
	}

	// Parse SSE events. Track the current event type so we can distinguish
	// "data:" lines belonging to device events vs. the "done" event.
	var dataLines []string
	var doneEvent bool
	currentEvent := ""
	scanner := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			if currentEvent != "done" {
				dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
			}
		} else if line == "" {
			if currentEvent == "done" {
				doneEvent = true
			}
			currentEvent = ""
		}
	}

	if len(dataLines) != 2 {
		t.Fatalf("expected 2 data lines, got %d: %v", len(dataLines), dataLines)
	}
	if !doneEvent {
		t.Fatal("expected 'event: done' line not found")
	}
	if !strings.Contains(dataLines[0], "TNC4") {
		t.Errorf("first data line should contain TNC4: %s", dataLines[0])
	}
}

// TestBLEMobilinkdScan_ScannerError streams an error event and returns 200.
func TestBLEMobilinkdScan_ScannerError(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.SetBLEMobilinkdScanner(&fakeBLEScanner{
		err: errors.New("bluetooth adapter not available"),
	})

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/kiss/ble-mobilinkd-scan?timeout=2", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (SSE), got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Errorf("expected 'event: error' in SSE body; got: %s", body)
	}
}
