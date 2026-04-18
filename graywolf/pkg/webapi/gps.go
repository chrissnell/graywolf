package webapi

import (
	"net/http"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/gps"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// registerGps installs the /api/gps route tree on mux using Go 1.22+
// method-scoped patterns.
func (s *Server) registerGps(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/gps", s.getGps)
	mux.HandleFunc("PUT /api/gps", s.updateGps)
	mux.HandleFunc("GET /api/gps/available", s.listAvailableGps)
}

// getGps returns the singleton GPS config. If no config row has been
// written yet the zero-value DTO is returned with 200 so the UI
// always gets a valid body to render defaults from.
//
// @Summary  Get GPS config
// @Tags     gps
// @ID       getGps
// @Produce  json
// @Success  200 {object} dto.GPSResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /gps [get]
func (s *Server) getGps(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetGPSConfig(r.Context())
	if err != nil {
		s.internalError(w, r, "get gps config", err)
		return
	}
	if c == nil {
		writeJSON(w, http.StatusOK, dto.GPSFromModel(configstore.GPSConfig{}))
		return
	}
	writeJSON(w, http.StatusOK, dto.GPSFromModel(*c))
}

// updateGps replaces the singleton GPS config.
//
// @Summary  Update GPS config
// @Tags     gps
// @ID       updateGps
// @Accept   json
// @Produce  json
// @Param    body body     dto.GPSRequest true "GPS config"
// @Success  200  {object} dto.GPSResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /gps [put]
func (s *Server) updateGps(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[dto.GPSRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	m := req.ToModel()
	if err := s.store.UpsertGPSConfig(r.Context(), &m); err != nil {
		s.internalError(w, r, "upsert gps config", err)
		return
	}
	s.signalGpsReload()
	writeJSON(w, http.StatusOK, dto.GPSFromModel(m))
}

// listAvailableGps returns the list of serial ports the OS can see.
//
// @Summary  List available GPS serial ports
// @Tags     gps
// @ID       listAvailableGps
// @Produce  json
// @Success  200 {array}  gps.SerialPortInfo
// @Security CookieAuth
// @Router   /gps/available [get]
func (s *Server) listAvailableGps(w http.ResponseWriter, r *http.Request) {
	ports, err := gps.EnumerateSerialPorts()
	if err != nil {
		s.logger.Warn("enumerate serial ports", "err", err)
		writeJSON(w, http.StatusOK, []gps.SerialPortInfo{})
		return
	}
	writeJSON(w, http.StatusOK, ports)
}

// signalGpsReload performs a non-blocking send on the GPS reload
// channel; coalesces if a previous signal is still buffered.
func (s *Server) signalGpsReload() {
	if s.gpsReload == nil {
		return
	}
	select {
	case s.gpsReload <- struct{}{}:
	default:
	}
}
