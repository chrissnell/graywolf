package webapi

import (
	"net/http"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/updatescheck"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// registerUpdates installs the /api/updates route tree on mux using
// Go 1.22+ method-scoped patterns. Shape mirrors registerStationConfig:
// a singleton config (GET + PUT) plus a read-only derived /status
// endpoint that projects the in-memory checker snapshot.
func (s *Server) registerUpdates(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/updates/config", s.getUpdatesConfig)
	mux.HandleFunc("PUT /api/updates/config", s.updateUpdatesConfig)
	mux.HandleFunc("GET /api/updates/status", s.getUpdatesStatus)
}

// getUpdatesConfig returns the current toggle state for the daily
// GitHub update check. On a fresh install (no row yet) the store
// returns UpdatesConfig{Enabled: true} per the zero-value-on-missing
// contract, so the feature is on by default without a seed step.
//
// @Summary  Get updates check configuration
// @Tags     updates
// @ID       getUpdatesConfig
// @Produce  json
// @Success  200 {object} dto.UpdatesConfigResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /updates/config [get]
func (s *Server) getUpdatesConfig(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetUpdatesConfig(r.Context())
	if err != nil {
		s.internalError(w, r, "get updates config", err)
		return
	}
	writeJSON(w, http.StatusOK, dto.UpdatesConfigResponse{Enabled: c.Enabled})
}

// updateUpdatesConfig replaces the singleton updates-check
// configuration and signals the checker so a toggle flip takes effect
// without waiting up to 24 hours for the next scheduled tick. The
// response body echoes the persisted value.
//
// @Summary  Update updates check configuration
// @Tags     updates
// @ID       updateUpdatesConfig
// @Accept   json
// @Produce  json
// @Param    body body     dto.UpdatesConfigRequest true "Updates configuration"
// @Success  200  {object} dto.UpdatesConfigResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /updates/config [put]
func (s *Server) updateUpdatesConfig(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[dto.UpdatesConfigRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}

	ctx := r.Context()
	if err := s.store.UpsertUpdatesConfig(ctx, configstore.UpdatesConfig{Enabled: req.Enabled}); err != nil {
		s.internalError(w, r, "upsert updates config", err)
		return
	}

	// Nudge the checker so the new toggle state is honored immediately
	// (D4). Non-blocking; coalesces via the size-1 buffer.
	s.signalUpdatesReload()

	// Read back for the response so the body always reflects stored
	// state, matching the other singleton PUT handlers.
	c, err := s.store.GetUpdatesConfig(ctx)
	if err != nil {
		s.internalError(w, r, "read updates config after upsert", err)
		return
	}
	writeJSON(w, http.StatusOK, dto.UpdatesConfigResponse{Enabled: c.Enabled})
}

// getUpdatesStatus projects the checker's in-memory snapshot into the
// /api/updates/status DTO. The checker is installed by pkg/app wiring
// via SetUpdatesChecker; if it hasn't been installed yet (e.g. a test
// that constructs a Server directly without running wiring) the handler
// returns a synthesized "pending" response rather than panicking.
//
// @Summary  Get latest-release status
// @Tags     updates
// @ID       getUpdatesStatus
// @Produce  json
// @Success  200 {object} dto.UpdatesStatusResponse
// @Security CookieAuth
// @Router   /updates/status [get]
func (s *Server) getUpdatesStatus(w http.ResponseWriter, _ *http.Request) {
	if s.updatesChecker == nil {
		// No checker wired yet. Report "pending" with the running
		// version as Current so the UI can still render "Graywolf
		// v{current}" under the About tab's "This install" heading.
		writeJSON(w, http.StatusOK, dto.UpdatesStatusResponse{
			Status:  updatescheck.StatusPending,
			Current: s.version,
		})
		return
	}
	snap := s.updatesChecker.Snapshot()
	resp := dto.UpdatesStatusResponse{
		Status:  snap.Status,
		Current: snap.Current,
		Latest:  snap.Latest,
		URL:     snap.URL,
	}
	if !snap.CheckedAt.IsZero() {
		resp.CheckedAt = snap.CheckedAt.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, resp)
}
