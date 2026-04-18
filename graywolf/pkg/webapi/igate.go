package webapi

import (
	"net/http"

	"github.com/chrissnell/graywolf/pkg/igate"
	"github.com/chrissnell/graywolf/pkg/webtypes"
)

// IgateToggleRequest is the POST body for /api/igate/simulation.
type IgateToggleRequest struct {
	Enabled bool `json:"enabled"`
}

// IgateSimulationResponse is the success body returned by
// POST /api/igate/simulation. It mirrors the toggled value so clients
// can confirm the requested state was applied.
type IgateSimulationResponse struct {
	SimulationMode bool `json:"simulation_mode"`
}

// RegisterIgate installs /api/igate (GET status) and
// /api/igate/simulation (POST toggle) on mux using Go 1.22+
// method-scoped patterns. Both callbacks may be nil, in which case the
// endpoints return 503. RegisterRoutes intentionally omits /api/igate
// so this helper owns the path.
//
// Signature shape (mux second) is shared with every out-of-band
// RegisterXxx in this package — see RegisterPackets, RegisterPosition,
// RegisterStations. Keep callers consistent.
//
// Operation IDs in the swag annotation blocks below are frozen against
// constants in pkg/webapi/docs/op_ids.go; `make docs-lint` enforces the
// correspondence.
func RegisterIgate(srv *Server, mux *http.ServeMux, toggle func(bool) error, status func() igate.Status) {
	if srv == nil || mux == nil {
		return
	}
	mux.HandleFunc("GET /api/igate", getIgateStatus(status))
	mux.HandleFunc("POST /api/igate/simulation", setIgateSimulation(srv, toggle))
}

// getIgateStatus returns the current igate runtime status.
//
// @Summary  Get igate status
// @Tags     igate
// @ID       getIgateStatus
// @Produce  json
// @Success  200 {object} igate.Status
// @Failure  503 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /igate [get]
func getIgateStatus(status func() igate.Status) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if status == nil {
			writeJSON(w, http.StatusServiceUnavailable, webtypes.ErrorResponse{Error: "igate not available"})
			return
		}
		writeJSON(w, http.StatusOK, status())
	}
}

// setIgateSimulation toggles the igate's simulation mode. The request
// body carries the desired enabled state; the response echoes it back.
//
// @Summary      Toggle igate simulation mode
// @Description  When enabled, the igate logs packets it would have sent
// @Description  (RF-to-APRS-IS gating and IS-to-RF beacons) instead of
// @Description  transmitting them. Useful for validating filter rules
// @Description  and bandwidth behaviour before going live.
// @Tags     igate
// @ID       setIgateSimulation
// @Accept   json
// @Produce  json
// @Param    body body     webapi.IgateToggleRequest true "Desired simulation mode"
// @Success  200  {object} webapi.IgateSimulationResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Failure  503  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /igate/simulation [post]
func setIgateSimulation(srv *Server, toggle func(bool) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if toggle == nil {
			writeJSON(w, http.StatusServiceUnavailable, webtypes.ErrorResponse{Error: "igate not available"})
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
		writeJSON(w, http.StatusOK, IgateSimulationResponse{SimulationMode: req.Enabled})
	}
}
