package webapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerDigipeater(mux *http.ServeMux) {
	mux.HandleFunc("/api/digipeater", s.handleDigipeaterConfig)
	mux.HandleFunc("/api/digipeater/rules", s.handleDigipeaterRules)
	mux.HandleFunc("/api/digipeater/rules/", s.handleDigipeaterRuleItem)
}

// GET/PUT /api/digipeater — singleton config.
func (s *Server) handleDigipeaterConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		c, err := s.store.GetDigipeaterConfig(r.Context())
		if err != nil || c == nil {
			notFound(w)
			return
		}
		writeJSON(w, http.StatusOK, dto.DigipeaterConfigFromModel(*c))
	case http.MethodPut:
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
	default:
		methodNotAllowed(w)
	}
}

// GET/POST /api/digipeater/rules
func (s *Server) handleDigipeaterRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleList[configstore.DigipeaterRule](s, w, r, "list digipeater rules",
			s.store.ListDigipeaterRules, dto.DigipeaterRuleFromModel)
	case http.MethodPost:
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
	default:
		methodNotAllowed(w)
	}
}

// PUT/DELETE /api/digipeater/rules/{id}
func (s *Server) handleDigipeaterRuleItem(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(strings.TrimPrefix(r.URL.Path, "/api/digipeater/rules/"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	switch r.Method {
	case http.MethodPut:
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
	case http.MethodDelete:
		handleDelete(s, w, r, "delete digipeater rule", id, func(ctx context.Context, id uint32) error {
			if err := s.store.DeleteDigipeaterRule(ctx, id); err != nil {
				return err
			}
			s.signalDigipeaterReload()
			return nil
		})
	default:
		methodNotAllowed(w)
	}
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
