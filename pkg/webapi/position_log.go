package webapi

import (
	"net/http"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// registerPositionLog installs the /api/position-log route on mux
// using Go 1.22+ method-scoped patterns.
func (s *Server) registerPositionLog(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/position-log", s.getPositionLog)
	mux.HandleFunc("PUT /api/position-log", s.updatePositionLog)
}

// getPositionLog returns the singleton position-log config. The
// database path is controlled by the -history-db flag, not the API,
// but is echoed back for client convenience.
//
// @Summary  Get position log config
// @Tags     position-log
// @ID       getPositionLog
// @Produce  json
// @Success  200 {object} dto.PositionLogResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /position-log [get]
func (s *Server) getPositionLog(w http.ResponseWriter, r *http.Request) {
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
}

// updatePositionLog replaces the singleton position-log config.
//
// @Summary  Update position log config
// @Tags     position-log
// @ID       updatePositionLog
// @Accept   json
// @Produce  json
// @Param    body body     dto.PositionLogRequest true "Position log config"
// @Success  200  {object} dto.PositionLogResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /position-log [put]
func (s *Server) updatePositionLog(w http.ResponseWriter, r *http.Request) {
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
