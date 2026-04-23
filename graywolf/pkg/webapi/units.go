package webapi

import (
	"net/http"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// registerUnits installs the /api/preferences/units route pair.
// Singleton config with the same GET + PUT shape as the other
// display-preference endpoints.
func (s *Server) registerUnits(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/preferences/units", s.getUnitsConfig)
	mux.HandleFunc("PUT /api/preferences/units", s.updateUnitsConfig)
}

// @Summary  Get units preference
// @Tags     preferences
// @ID       getUnitsConfig
// @Produce  json
// @Success  200 {object} dto.UnitsConfigResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /preferences/units [get]
func (s *Server) getUnitsConfig(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetUnitsConfig(r.Context())
	if err != nil {
		s.internalError(w, r, "get units config", err)
		return
	}
	writeJSON(w, http.StatusOK, dto.UnitsConfigResponse{System: c.System})
}

// @Summary  Update units preference
// @Tags     preferences
// @ID       updateUnitsConfig
// @Accept   json
// @Produce  json
// @Param    body body     dto.UnitsConfigRequest true "Units preference"
// @Success  200  {object} dto.UnitsConfigResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /preferences/units [put]
func (s *Server) updateUnitsConfig(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[dto.UnitsConfigRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}

	ctx := r.Context()
	if err := s.store.UpsertUnitsConfig(ctx, configstore.UnitsConfig{System: req.System}); err != nil {
		s.internalError(w, r, "upsert units config", err)
		return
	}

	c, err := s.store.GetUnitsConfig(ctx)
	if err != nil {
		s.internalError(w, r, "read units config after upsert", err)
		return
	}
	writeJSON(w, http.StatusOK, dto.UnitsConfigResponse{System: c.System})
}
