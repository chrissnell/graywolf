package webapi

import (
	"context"
	"net/http"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// registerDigipeater installs the /api/digipeater route tree on mux
// using Go 1.22+ method-scoped patterns. Each route maps to exactly
// one handler.
func (s *Server) registerDigipeater(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/digipeater", s.getDigipeaterConfig)
	mux.HandleFunc("PUT /api/digipeater", s.updateDigipeaterConfig)
	mux.HandleFunc("GET /api/digipeater/rules", s.listDigipeaterRules)
	mux.HandleFunc("POST /api/digipeater/rules", s.createDigipeaterRule)
	mux.HandleFunc("PUT /api/digipeater/rules/{id}", s.updateDigipeaterRule)
	mux.HandleFunc("DELETE /api/digipeater/rules/{id}", s.deleteDigipeaterRule)
}

// getDigipeaterConfig returns the singleton digipeater config. If no
// config row has been written yet the zero-value DTO is returned with
// 200 so the UI always gets a valid body to render defaults from.
//
// @Summary  Get digipeater config
// @Tags     digipeater
// @ID       getDigipeaterConfig
// @Produce  json
// @Success  200 {object} dto.DigipeaterConfigResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /digipeater [get]
func (s *Server) getDigipeaterConfig(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetDigipeaterConfig(r.Context())
	if err != nil {
		s.internalError(w, r, "get digipeater config", err)
		return
	}
	if c == nil {
		writeJSON(w, http.StatusOK, dto.DigipeaterConfigFromModel(configstore.DigipeaterConfig{}))
		return
	}
	writeJSON(w, http.StatusOK, dto.DigipeaterConfigFromModel(*c))
}

// updateDigipeaterConfig replaces the singleton digipeater config.
//
// @Summary  Update digipeater config
// @Tags     digipeater
// @ID       updateDigipeaterConfig
// @Accept   json
// @Produce  json
// @Param    body body     dto.DigipeaterConfigRequest true "Digipeater config"
// @Success  200  {object} dto.DigipeaterConfigResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /digipeater [put]
func (s *Server) updateDigipeaterConfig(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[dto.DigipeaterConfigRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	m := req.ToModel()
	if err := s.store.UpsertDigipeaterConfig(r.Context(), &m); err != nil {
		s.internalError(w, r, "upsert digipeater config", err)
		return
	}
	s.signalDigipeaterReload()
	writeJSON(w, http.StatusOK, dto.DigipeaterConfigFromModel(m))
}

// listDigipeaterRules returns every configured digipeater rule.
//
// @Summary  List digipeater rules
// @Tags     digipeater
// @ID       listDigipeaterRules
// @Produce  json
// @Success  200 {array}  dto.DigipeaterRuleResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /digipeater/rules [get]
func (s *Server) listDigipeaterRules(w http.ResponseWriter, r *http.Request) {
	handleList[configstore.DigipeaterRule](s, w, r, "list digipeater rules",
		s.store.ListDigipeaterRules, dto.DigipeaterRuleFromModel)
}

// createDigipeaterRule creates a new digipeater rule from the request
// body and returns the persisted record (with its assigned id).
//
// @Summary  Create digipeater rule
// @Tags     digipeater
// @ID       createDigipeaterRule
// @Accept   json
// @Produce  json
// @Param    body body     dto.DigipeaterRuleRequest true "Digipeater rule definition"
// @Success  201  {object} dto.DigipeaterRuleResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /digipeater/rules [post]
func (s *Server) createDigipeaterRule(w http.ResponseWriter, r *http.Request) {
	handleCreate[dto.DigipeaterRuleRequest](s, w, r, "create digipeater rule",
		func(ctx context.Context, req dto.DigipeaterRuleRequest) (configstore.DigipeaterRule, error) {
			m := req.ToModel()
			if err := s.store.CreateDigipeaterRule(ctx, &m); err != nil {
				return configstore.DigipeaterRule{}, err
			}
			s.signalDigipeaterReload()
			return m, nil
		},
		dto.DigipeaterRuleFromModel)
}

// updateDigipeaterRule replaces the digipeater rule with the given id
// using the request body and returns the persisted record.
//
// @Summary  Update digipeater rule
// @Tags     digipeater
// @ID       updateDigipeaterRule
// @Accept   json
// @Produce  json
// @Param    id   path     int                       true "Digipeater rule id"
// @Param    body body     dto.DigipeaterRuleRequest true "Digipeater rule definition"
// @Success  200  {object} dto.DigipeaterRuleResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /digipeater/rules/{id} [put]
func (s *Server) updateDigipeaterRule(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	handleUpdate[dto.DigipeaterRuleRequest](s, w, r, "update digipeater rule", id,
		func(ctx context.Context, id uint32, req dto.DigipeaterRuleRequest) (configstore.DigipeaterRule, error) {
			m := req.ToUpdate(id)
			if err := s.store.UpdateDigipeaterRule(ctx, &m); err != nil {
				return configstore.DigipeaterRule{}, err
			}
			s.signalDigipeaterReload()
			return m, nil
		},
		dto.DigipeaterRuleFromModel)
}

// deleteDigipeaterRule removes the digipeater rule with the given id.
//
// @Summary  Delete digipeater rule
// @Tags     digipeater
// @ID       deleteDigipeaterRule
// @Param    id  path int true "Digipeater rule id"
// @Success  204 "No Content"
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /digipeater/rules/{id} [delete]
func (s *Server) deleteDigipeaterRule(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	handleDelete(s, w, r, "delete digipeater rule", id, func(ctx context.Context, id uint32) error {
		if err := s.store.DeleteDigipeaterRule(ctx, id); err != nil {
			return err
		}
		s.signalDigipeaterReload()
		return nil
	})
}

// signalDigipeaterReload performs a non-blocking send on the
// digipeater reload channel; coalesces if a previous signal is still
// buffered.
func (s *Server) signalDigipeaterReload() {
	if s.digipeaterReload == nil {
		return
	}
	select {
	case s.digipeaterReload <- struct{}{}:
	default:
	}
}
