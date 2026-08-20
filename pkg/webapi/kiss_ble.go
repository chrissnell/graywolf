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

// BLEMobilinkdDevice is one discovered BLE TNC peripheral streamed by
// GET /api/kiss/ble-mobilinkd-scan. Covers Mobilinkd TNC3/TNC4 and
// NUS-based devices (BTECH UV-PRO, VERO VR-N76, Radioddity GA-5WB).
type BLEMobilinkdDevice struct {
	Addr string `json:"addr"`
	Name string `json:"name"`
	RSSI int16  `json:"rssi"`
}

// BLEMobilinkdScanner is the narrow interface the SSE scan handler
// consumes. Non-Android builds wire a real BLE scanner backed by
// kiss.ScanBLEMobilinkd (see pkg/app/blesource_desktop.go); Android
// builds leave it nil and the handler returns 501.
type BLEMobilinkdScanner interface {
	Scan(ctx context.Context, discovered func(BLEMobilinkdDevice)) error
}

// SetBLEMobilinkdScanner installs the BLE scanner post-construction.
// Called from pkg/app on non-Android builds; nil on Android so the
// handler responds 501 Not Implemented.
func (s *Server) SetBLEMobilinkdScanner(sc BLEMobilinkdScanner) {
	s.bleMobilinkdScanner = sc
}

// bleScanMu prevents concurrent BLE scans. One scan at a time: a
// second request while a scan is active receives 409 Conflict.
var bleScanMu sync.Mutex

// handleBLEMobilinkdScan streams discovered Mobilinkd TNC3/TNC4 BLE devices
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
// @Router   /kiss/ble-mobilinkd-scan [get]
func (s *Server) handleBLEMobilinkdScan(w http.ResponseWriter, r *http.Request) {
	if s.bleMobilinkdScanner == nil {
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

	err := s.bleMobilinkdScanner.Scan(ctx, func(dev BLEMobilinkdDevice) {
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
