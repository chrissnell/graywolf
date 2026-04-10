package webapi

import (
	"encoding/json"
	"net/http"
	"time"
)

// IgateStatus mirrors pkg/igate.Status without importing pkg/igate
// (which would drag in filters, prometheus, etc. — keeping webapi
// dependency-light means the orchestrator wires the status provider
// as a plain function). Field names match the iGate's JSON shape so
// the UI consumes a single schema.
type IgateStatus struct {
	Connected      bool      `json:"connected"`
	Server         string    `json:"server"`
	Callsign       string    `json:"callsign"`
	SimulationMode bool      `json:"simulation_mode"`
	LastConnected  time.Time `json:"last_connected,omitempty"`
	Gated          uint64    `json:"rf_to_is_gated"`
	Downlinked     uint64    `json:"is_to_rf_gated"`
	Filtered       uint64    `json:"packets_filtered"`
	DroppedOffline uint64    `json:"rf_to_is_dropped"`
}

// IgateToggleRequest is the POST body for /api/igate/simulation.
type IgateToggleRequest struct {
	Enabled bool `json:"enabled"`
}

// RegisterIgate installs /api/igate (GET status) and
// /api/igate/simulation (POST toggle) on the Server's mux. It is
// called from cmd/graywolf after the iGate is constructed; both
// callbacks may be nil, in which case the endpoints return 503.
//
// The orchestrator is responsible for removing the placeholder
// /api/igate stub in webapi.go before wiring this up; attempting to
// register duplicate patterns would panic at mux installation.
func RegisterIgate(srv *Server, mux *http.ServeMux, toggle func(bool) error, status func() IgateStatus) {
	if srv == nil || mux == nil {
		return
	}
	mux.HandleFunc("/api/igate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if status == nil {
			http.Error(w, "igate not available", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, status())
	})
	mux.HandleFunc("/api/igate/simulation", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if toggle == nil {
			http.Error(w, "igate not available", http.StatusServiceUnavailable)
			return
		}
		var req IgateToggleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if err := toggle(req.Enabled); err != nil {
			srv.logger.Warn("igate toggle failed", "err", err)
			http.Error(w, "toggle failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"simulation_mode": req.Enabled})
	})
}
