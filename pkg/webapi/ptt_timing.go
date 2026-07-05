package webapi

import (
	"net/http"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// pttTimingDefaults is returned when no ptt_timings row exists yet so the
// UI always renders the protocol defaults rather than a misleading 0/0.
// A fresh install's migration seeds this row, so this is a belt-and-suspenders
// fallback.
var pttTimingDefaults = configstore.PttTiming{TxDelayMs: 300, TxTailMs: 100}

// registerPttTiming installs the /api/ptt-timing route on mux. PTT keying
// timing (TX delay / TX tail) is a global station setting — one singleton,
// no channel path param — because it is a property of the radio's PTT,
// independent of the modem mode.
func (s *Server) registerPttTiming(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/ptt-timing", s.getPttTiming)
	mux.HandleFunc("PUT /api/ptt-timing", s.updatePttTiming)
}

// getPttTiming returns the global PTT keying timing. If no row exists yet
// the protocol defaults are returned with 200 so the UI always has a body.
//
// @Summary  Get global PTT timing
// @Tags     ptt-timing
// @ID       getPttTiming
// @Produce  json
// @Success  200 {object} dto.PttTimingResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /ptt-timing [get]
func (s *Server) getPttTiming(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.GetPttTiming(r.Context())
	if err != nil {
		s.internalError(w, r, "get ptt timing", err)
		return
	}
	if t == nil {
		writeJSON(w, http.StatusOK, dto.PttTimingFromModel(pttTimingDefaults))
		return
	}
	writeJSON(w, http.StatusOK, dto.PttTimingFromModel(*t))
}

// updatePttTiming replaces the global PTT keying timing and triggers a
// bridge reload so the new values are pushed to every channel's PTT.
//
// @Summary  Update global PTT timing
// @Tags     ptt-timing
// @ID       updatePttTiming
// @Accept   json
// @Produce  json
// @Param    body body     dto.PttTimingRequest true "PTT timing"
// @Success  200  {object} dto.PttTimingResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /ptt-timing [put]
func (s *Server) updatePttTiming(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[dto.PttTimingRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	ctx := r.Context()
	existingPtr, err := s.store.GetPttTiming(ctx)
	if err != nil {
		s.internalError(w, r, "get ptt timing", err)
		return
	}
	var existing configstore.PttTiming
	if existingPtr != nil {
		existing = *existingPtr
	}
	m := req.ApplyToModel(existing)
	if err := s.store.UpsertPttTiming(ctx, &m); err != nil {
		s.internalError(w, r, "upsert ptt timing", err)
		return
	}
	// Global timing feeds every channel's ConfigurePtt; a full reload
	// re-emits them with the new values.
	s.notifyBridgeReload(ctx)
	writeJSON(w, http.StatusOK, dto.PttTimingFromModel(m))
}
