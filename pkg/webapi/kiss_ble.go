package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/chrissnell/graywolf/pkg/webtypes"
)

// BLEDevice is one discovered BLE TNC peripheral streamed by
// GET /api/kiss/ble-device-scan. Covers BLE KISS TNC devices (Mobilinkd, NUS-based) and
// NUS-based devices (BTECH UV-PRO, VERO VR-N76, Radioddity GA-5WB).
type BLEDevice struct {
	Addr string `json:"addr"`
	Name string `json:"name"`
	RSSI int16  `json:"rssi"`
}

// BLEScanner is the narrow interface the SSE scan handler
// consumes. Non-Android builds wire a real BLE scanner backed by
// kiss.ScanBLEMobilinkd (see pkg/app/blesource_desktop.go); Android
// builds leave it nil and the handler returns 501.
type BLEScanner interface {
	Scan(ctx context.Context, discovered func(BLEDevice)) error
}

// SetBLEScanner installs the BLE scanner post-construction.
// Called from pkg/app on non-Android builds; nil on Android so the
// handler responds 501 Not Implemented.
func (s *Server) SetBLEScanner(sc BLEScanner) {
	s.bleScanner = sc
}

// BLERepairer is the narrow interface the repairble handler uses.
// Android builds wire the live platformsvc client; other platforms leave it
// nil and the handler returns 501 Not Implemented.
type BLERepairer interface {
	BLERepair(ctx context.Context, mac string) error
}

// SetBLERepairer installs the BLE repairer post-construction.
func (s *Server) SetBLERepairer(r BLERepairer) {
	s.bleRepairer = r
}

// bleScanMu prevents concurrent BLE scans. One scan at a time: a
// second request while a scan is active receives 409 Conflict.
var bleScanMu sync.Mutex

// handleBLEScan streams discovered Mobilinkd TNC3/TNC4 BLE devices
// as Server-Sent Events. Each discovered peripheral yields a "data:" event
// with a JSON-encoded BLEMobilinkdDevice object. After the scan timeout a
// final "event: done" event is sent and the stream closes.
//
// Query parameters:
//
//	timeout  Duration in seconds (default 15, max 60). Example: ?timeout=10
//
// @Summary  Scan for Mobilinkd BLE TNC devices (desktop only)
// @Tags     kiss
// @ID       scanBLEMobilinkd
// @Produce  text/event-stream
// @Param    timeout  query  int  false  "Scan duration in seconds (default 15, max 60)"
// @Success  200 "SSE stream of BLEMobilinkdDevice objects"
// @Failure  409 {object} webtypes.ErrorResponse "scan already in progress"
// @Failure  501 {object} webtypes.ErrorResponse "not available on this platform"
// @Security CookieAuth
// @Router   /kiss/ble-device-scan [get]
func (s *Server) handleBLEScan(w http.ResponseWriter, r *http.Request) {
	if s.bleScanner == nil {
		writeJSON(w, http.StatusNotImplemented, webtypes.ErrorResponse{
			Error: "BLE scanning is not available on this platform (Android: use Bluetooth Serial; macOS release builds: rebuild with CGO_ENABLED=1)",
		})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	if !bleScanMu.TryLock() {
		writeJSON(w, http.StatusConflict, webtypes.ErrorResponse{
			Error: "a BLE scan is already in progress",
		})
		return
	}
	defer bleScanMu.Unlock()

	timeout := 15 * time.Second
	if ts := r.URL.Query().Get("timeout"); ts != "" {
		if secs, err := strconv.Atoi(ts); err == nil && secs > 0 {
			if secs > 60 {
				secs = 60
			}
			timeout = time.Duration(secs) * time.Second
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Connection", "keep-alive")

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	err := s.bleScanner.Scan(ctx, func(dev BLEDevice) {
		data, _ := json.Marshal(dev)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	})
	if err != nil && r.Context().Err() == nil {
		// Stream a best-effort error event before closing.
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
		flusher.Flush()
		return
	}

	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
	flusher.Flush()
}
