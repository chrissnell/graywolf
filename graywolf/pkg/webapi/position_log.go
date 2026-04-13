package webapi

import (
	"net/http"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerPositionLog(mux *http.ServeMux) {
	mux.HandleFunc("/api/position-log", s.handlePositionLog)
}

// GET/PUT /api/position-log — singleton.
func (s *Server) handlePositionLog(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		c, err := s.store.GetPositionLogConfig(r.Context())
		if err != nil {
			s.internalError(w, r, "get position log config", err)
			return
		}
		if c == nil {
			// Return defaults when no row exists yet.
			c = &configstore.PositionLogConfig{
				DBPath: "./graywolf-history.db",
			}
		}
		writeJSON(w, http.StatusOK, dto.PositionLogFromModel(*c))
	case http.MethodPut:
		req, err := decodeJSON[dto.PositionLogRequest](r)
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		if err := req.Validate(); err != nil {
			badRequest(w, err.Error())
			return
		}
		m := req.ToModel()
		if err := s.store.UpsertPositionLogConfig(r.Context(), &m); err != nil {
			s.internalError(w, r, "upsert position log config", err)
			return
		}
		s.signalPositionLogReload()
		writeJSON(w, http.StatusOK, dto.PositionLogFromModel(m))
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) signalPositionLogReload() {
	if s.positionLogReload == nil {
		return
	}
	select {
	case s.positionLogReload <- struct{}{}:
	default:
	}
}
