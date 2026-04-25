package webapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/chrissnell/graywolf/pkg/mapsauth"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// registerMaps installs the /api/preferences/maps route pair plus the
// /register sub-endpoint. Source-only updates go through the PUT;
// registration is a separate POST so the auth.nw5w.com round-trip
// stays out of generic preference writes and so the issued token can
// be returned to the UI exactly once.
func (s *Server) registerMaps(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/preferences/maps", s.getMapsConfig)
	mux.HandleFunc("PUT /api/preferences/maps", s.updateMapsConfig)
	mux.HandleFunc("POST /api/preferences/maps/register", s.registerMapsToken)
}

// @Summary  Get maps preference
// @Tags     preferences
// @ID       getMapsConfig
// @Produce  json
// @Param    include_token query    string false "Set to 1 to include the device token in the response"
// @Success  200 {object} dto.MapsConfigResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /preferences/maps [get]
func (s *Server) getMapsConfig(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetMapsConfig(r.Context())
	if err != nil {
		s.internalError(w, r, "get maps config", err)
		return
	}
	resp := dto.MapsConfigResponse{
		Source:       c.Source,
		Callsign:     c.Callsign,
		Registered:   c.Token != "",
		RegisteredAt: c.RegisteredAt,
	}
	if r.URL.Query().Get("include_token") == "1" && c.Token != "" {
		resp.Token = c.Token
	}
	writeJSON(w, http.StatusOK, resp)
}

// @Summary  Update maps preference
// @Tags     preferences
// @ID       updateMapsConfig
// @Accept   json
// @Produce  json
// @Param    body body     dto.MapsConfigRequest true "Maps preference"
// @Success  200  {object} dto.MapsConfigResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /preferences/maps [put]
func (s *Server) updateMapsConfig(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[dto.MapsConfigRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	// Get-then-modify-then-Upsert: UpsertMapsConfig is full-replace, so
	// constructing a fresh MapsConfig{Source: req.Source} would clobber
	// the device callsign + token. Read the current row, mutate just
	// Source, and write it back.
	current, err := s.store.GetMapsConfig(r.Context())
	if err != nil {
		s.internalError(w, r, "get maps config", err)
		return
	}
	if req.Source == "graywolf" && current.Token == "" {
		badRequest(w, "register this device before selecting Graywolf maps")
		return
	}
	current.Source = req.Source
	if err := s.store.UpsertMapsConfig(r.Context(), current); err != nil {
		s.internalError(w, r, "upsert maps config", err)
		return
	}
	// Echo back the new state in the same shape as GET. include_token
	// query handling lives in getMapsConfig; deferring there keeps the
	// two responses consistent.
	s.getMapsConfig(w, r)
}

// @Summary  Register this device with auth.nw5w.com
// @Tags     preferences
// @ID       registerMapsToken
// @Accept   json
// @Produce  json
// @Param    body body     dto.RegisterRequest true "Registration request"
// @Success  200  {object} dto.RegisterResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  409  {object} webtypes.ErrorResponse
// @Failure  429  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /preferences/maps/register [post]
func (s *Server) registerMapsToken(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[dto.RegisterRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	callsign, err := dto.NormalizeCallsign(req.Callsign)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	out, err := s.mapsAuth.Register(r.Context(), callsign)
	if err != nil {
		// Surface upstream errors verbatim so the UI can show the
		// auth-server message (which points to the issue tracker for
		// real failures) instead of a generic "registration failed".
		var rerr *mapsauth.Error
		if errors.As(err, &rerr) {
			writeJSON(w, rerr.Status, map[string]string{
				"error":   rerr.Code,
				"message": rerr.Message,
			})
			return
		}
		s.internalError(w, r, "register with auth.nw5w.com", err)
		return
	}
	c, err := s.store.GetMapsConfig(r.Context())
	if err != nil {
		s.internalError(w, r, "get maps config", err)
		return
	}
	c.Source = "graywolf"
	c.Callsign = out.Callsign
	c.Token = out.Token
	c.RegisteredAt = time.Now().UTC()
	if err := s.store.UpsertMapsConfig(r.Context(), c); err != nil {
		s.internalError(w, r, "persist registration", err)
		return
	}
	// The only response that ever returns the token. The UI uses this
	// one-shot delivery to offer an export-token-to-file flow before
	// later GETs go back to suppressing it.
	writeJSON(w, http.StatusOK, dto.MapsConfigResponse{
		Source:       c.Source,
		Callsign:     c.Callsign,
		Registered:   true,
		RegisteredAt: c.RegisteredAt,
		Token:        c.Token,
	})
}
