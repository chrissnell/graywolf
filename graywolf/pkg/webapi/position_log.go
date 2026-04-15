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
		enabled := c != nil && c.Enabled
		writeJSON(w, http.StatusOK, dto.PositionLogResponse{
			Enabled: enabled,
			DBPath:  s.historyDBPath,
		})
	case http.MethodPut:
		req, err := decodeJSON[dto.PositionLogRequest](r)
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		m := configstore.PositionLogConfig{
			Enabled: req.Enabled,
			DBPath:  s.historyDBPath,
		}
		if err := s.store.UpsertPositionLogConfig(r.Context(), &m); err != nil {
			s.internalError(w, r, "upsert position log config", err)
			return
		}
		s.signalPositionLogReload()
		writeJSON(w, http.StatusOK, dto.PositionLogResponse{
			Enabled: m.Enabled,
			DBPath:  s.historyDBPath,
		})
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
