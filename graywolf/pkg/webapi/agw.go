package webapi

import (
	"net/http"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// registerAgw installs the /api/agw route on mux using Go 1.22+
// method-scoped patterns.
func (s *Server) registerAgw(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/agw", s.getAgw)
	mux.HandleFunc("PUT /api/agw", s.updateAgw)
}

// getAgw returns the singleton AGW config. If no config row has been
// written yet the zero-value DTO is returned with 200 so the UI
// always gets a valid body to render defaults from.
//
// @Summary  Get AGW config
// @Tags     agw
// @ID       getAgw
// @Produce  json
// @Success  200 {object} dto.AgwResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /agw [get]
func (s *Server) getAgw(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetAgwConfig(r.Context())
	if err != nil {
		s.internalError(w, r, "get agw config", err)
		return
	}
	if c == nil {
		writeJSON(w, http.StatusOK, dto.AgwFromModel(configstore.AgwConfig{}))
		return
	}
	writeJSON(w, http.StatusOK, dto.AgwFromModel(*c))
}

// updateAgw replaces the singleton AGW config.
//
// @Summary  Update AGW config
// @Tags     agw
// @ID       updateAgw
// @Accept   json
// @Produce  json
// @Param    body body     dto.AgwRequest true "AGW config"
// @Success  200  {object} dto.AgwResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /agw [put]
func (s *Server) updateAgw(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[dto.AgwRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	m := req.ToModel()
	if err := s.store.UpsertAgwConfig(r.Context(), &m); err != nil {
		s.internalError(w, r, "upsert agw config", err)
		return
	}
	s.signalAgwReload()
	writeJSON(w, http.StatusOK, dto.AgwFromModel(m))
}

// signalAgwReload performs a non-blocking send on the AGW reload
// channel; coalesces if a previous signal is still buffered.
func (s *Server) signalAgwReload() {
	if s.agwReload == nil {
		return
	}
	select {
	case s.agwReload <- struct{}{}:
	default:
	}
}
