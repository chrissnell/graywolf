package webapi

import (
	"encoding/json"
	"net/http"

	"github.com/chrissnell/graywolf/pkg/igate"
)

// IgateToggleRequest is the POST body for /api/igate/simulation.
type IgateToggleRequest struct {
	Enabled bool `json:"enabled"`
}

// RegisterIgate installs /api/igate (GET status) and
// /api/igate/simulation (POST toggle) on mux. Both callbacks may be
// nil, in which case the endpoints return 503. RegisterRoutes
// intentionally omits /api/igate so this helper owns the path.
func RegisterIgate(srv *Server, mux *http.ServeMux, toggle func(bool) error, status func() igate.Status) {
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
