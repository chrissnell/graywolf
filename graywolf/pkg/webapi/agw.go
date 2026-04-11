package webapi

import (
	"net/http"

	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerAgw(mux *http.ServeMux) {
	mux.HandleFunc("/api/agw", s.handleAgw)
}

// GET/PUT /api/agw — singleton.
func (s *Server) handleAgw(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		c, err := s.store.GetAgwConfig(r.Context())
		if err != nil || c == nil {
			notFound(w)
			return
		}
		writeJSON(w, http.StatusOK, dto.AgwFromModel(*c))
	case http.MethodPut:
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
		writeJSON(w, http.StatusOK, dto.AgwFromModel(m))
	default:
		methodNotAllowed(w)
	}
}
