package webapi

import (
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
			methodNotAllowed(w)
			return
		}
		if status == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "igate not available"})
			return
		}
		writeJSON(w, http.StatusOK, status())
	})
	mux.HandleFunc("/api/igate/simulation", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		if toggle == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "igate not available"})
			return
		}
		req, err := decodeJSON[IgateToggleRequest](r)
		if err != nil {
			badRequest(w, "invalid json")
			return
		}
		if err := toggle(req.Enabled); err != nil {
			srv.internalError(w, r, "igate toggle", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"simulation_mode": req.Enabled})
	})
}
