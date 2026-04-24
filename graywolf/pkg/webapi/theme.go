package webapi

import (
	"net/http"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// registerTheme installs the /api/preferences/theme route pair.
// Singleton config with the same GET + PUT shape as the other
// display-preference endpoints. Theme id validation is regex-only
// (see configstore.IsValidTheme) — the shipped set is defined in
// graywolf/web/themes/themes.json so new themes are PR-only changes.
func (s *Server) registerTheme(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/preferences/theme", s.getThemeConfig)
	mux.HandleFunc("PUT /api/preferences/theme", s.updateThemeConfig)
}

// @Summary  Get theme preference
// @Tags     preferences
// @ID       getThemeConfig
// @Produce  json
// @Success  200 {object} dto.ThemeConfigResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /preferences/theme [get]
func (s *Server) getThemeConfig(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetThemeConfig(r.Context())
	if err != nil {
		s.internalError(w, r, "get theme config", err)
		return
	}
	writeJSON(w, http.StatusOK, dto.ThemeConfigResponse{ID: c.ThemeID})
}

// @Summary  Update theme preference
// @Tags     preferences
// @ID       updateThemeConfig
// @Accept   json
// @Produce  json
// @Param    body body     dto.ThemeConfigRequest true "Theme preference"
// @Success  200  {object} dto.ThemeConfigResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /preferences/theme [put]
func (s *Server) updateThemeConfig(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[dto.ThemeConfigRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}

	ctx := r.Context()
	if err := s.store.UpsertThemeConfig(ctx, configstore.ThemeConfig{ThemeID: req.ID}); err != nil {
		s.internalError(w, r, "upsert theme config", err)
		return
	}

	c, err := s.store.GetThemeConfig(ctx)
	if err != nil {
		s.internalError(w, r, "read theme config after upsert", err)
		return
	}
	writeJSON(w, http.StatusOK, dto.ThemeConfigResponse{ID: c.ThemeID})
}
