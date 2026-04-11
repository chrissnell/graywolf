package webapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerIgateConfig(mux *http.ServeMux) {
	mux.HandleFunc("/api/igate/config", s.handleIgateConfig)
	mux.HandleFunc("/api/igate/filters", s.handleIgateFilters)
	mux.HandleFunc("/api/igate/filters/", s.handleIgateFilterItem)
}

// GET/PUT /api/igate/config — singleton.
func (s *Server) handleIgateConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		c, err := s.store.GetIGateConfig(r.Context())
		if err != nil || c == nil {
			notFound(w)
			return
		}
		writeJSON(w, http.StatusOK, dto.IGateConfigFromModel(*c))
	case http.MethodPut:
		req, err := decodeJSON[dto.IGateConfigRequest](r)
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		if err := req.Validate(); err != nil {
			badRequest(w, err.Error())
			return
		}
		m := req.ToModel()
		if err := s.store.UpsertIGateConfig(r.Context(), &m); err != nil {
			s.internalError(w, r, "upsert igate config", err)
			return
		}
		writeJSON(w, http.StatusOK, dto.IGateConfigFromModel(m))
	default:
		methodNotAllowed(w)
	}
}

// GET/POST /api/igate/filters
func (s *Server) handleIgateFilters(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleList[configstore.IGateRfFilter](s, w, r, "list igate rf filters",
			s.store.ListIGateRfFilters, dto.IGateRfFilterFromModel)
	case http.MethodPost:
		handleCreate[dto.IGateRfFilterRequest](s, w, r, "create igate rf filter",
			func(ctx context.Context, req dto.IGateRfFilterRequest) (configstore.IGateRfFilter, error) {
				m := req.ToModel()
				return m, s.store.CreateIGateRfFilter(ctx, &m)
			},
			dto.IGateRfFilterFromModel)
	default:
		methodNotAllowed(w)
	}
}

// PUT/DELETE /api/igate/filters/{id}
func (s *Server) handleIgateFilterItem(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(strings.TrimPrefix(r.URL.Path, "/api/igate/filters/"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	switch r.Method {
	case http.MethodPut:
		handleUpdate[dto.IGateRfFilterRequest](s, w, r, "update igate rf filter", id,
			func(ctx context.Context, id uint32, req dto.IGateRfFilterRequest) (configstore.IGateRfFilter, error) {
				m := req.ToUpdate(id)
				return m, s.store.UpdateIGateRfFilter(ctx, &m)
			},
			dto.IGateRfFilterFromModel)
	case http.MethodDelete:
		handleDelete(s, w, r, "delete igate rf filter", id, s.store.DeleteIGateRfFilter)
	default:
		methodNotAllowed(w)
	}
}
