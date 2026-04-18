package webapi

import (
	"net/http"

	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// registerSmartBeacon installs the /api/smart-beacon route tree on mux
// using Go 1.22+ method-scoped patterns. The resource is a singleton
// (global curve parameters applied to every beacon with
// smart_beacon=true), so there is no list/create/delete pair — just
// GET (always returns 200, defaulting to beacon.DefaultSmartBeacon()
// when no row exists) and PUT (upsert + reload signal).
func (s *Server) registerSmartBeacon(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/smart-beacon", s.getSmartBeacon)
	mux.HandleFunc("PUT /api/smart-beacon", s.updateSmartBeacon)
}

// getSmartBeacon returns the active SmartBeacon curve parameters.
//
// @Summary     Get SmartBeacon configuration
// @Description Returns the global SmartBeacon parameters that control
// @Description transmit cadence for every beacon with smart_beacon=true.
// @Description Returns defaults when no configuration has been saved yet.
// @Tags        beacons
// @ID          getSmartBeacon
// @Produce     json
// @Success     200 {object} dto.SmartBeaconConfigResponse
// @Failure     500 {object} webtypes.ErrorResponse
// @Security    CookieAuth
// @Router      /smart-beacon [get]
func (s *Server) getSmartBeacon(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetSmartBeaconConfig(r.Context())
	if err != nil {
		s.internalError(w, r, "get smart beacon", err)
		return
	}
	if c == nil {
		writeJSON(w, http.StatusOK, dto.SmartBeaconConfigDefaults())
		return
	}
	writeJSON(w, http.StatusOK, dto.SmartBeaconConfigFromModel(*c))
}

// updateSmartBeacon replaces the singleton SmartBeacon configuration
// and signals the beacon reload pipeline so running trackers pick up
// the new curve without a restart.
//
// @Summary     Update SmartBeacon configuration
// @Description Replaces the global SmartBeacon curve parameters. On
// @Description success, re-reads the persisted row and returns it so
// @Description the client sees the stored shape.
// @Tags        beacons
// @ID          updateSmartBeacon
// @Accept      json
// @Produce     json
// @Param       body body     dto.SmartBeaconConfigRequest true "SmartBeacon configuration"
// @Success     200  {object} dto.SmartBeaconConfigResponse
// @Failure     400  {object} webtypes.ErrorResponse
// @Failure     500  {object} webtypes.ErrorResponse
// @Security    CookieAuth
// @Router      /smart-beacon [put]
func (s *Server) updateSmartBeacon(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[dto.SmartBeaconConfigRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	m := dto.SmartBeaconConfigToModel(req)
	if err := s.store.UpsertSmartBeaconConfig(r.Context(), &m); err != nil {
		s.internalError(w, r, "upsert smart beacon", err)
		return
	}
	s.signalSmartBeaconReload()
	writeJSON(w, http.StatusOK, dto.SmartBeaconConfigFromModel(m))
}

// signalSmartBeaconReload performs a non-blocking send on the
// smart-beacon reload channel; coalesces if a previous signal is
// still buffered. Matches signalGpsReload / signalDigipeaterReload.
func (s *Server) signalSmartBeaconReload() {
	if s.smartBeaconReload == nil {
		return
	}
	select {
	case s.smartBeaconReload <- struct{}{}:
	default:
	}
}
