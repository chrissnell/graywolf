package webapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/chrissnell/graywolf/pkg/beacon"
	"github.com/chrissnell/graywolf/pkg/callsign"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/cot"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
	"github.com/chrissnell/graywolf/pkg/webtypes"
)

// registerCot installs the /api/cot-settings and /api/cot-targets route
// trees. A Cursor-on-Target (CoT) is a named/iconed/commented APRS
// object dropped from the live map's "Add CoT" dialog: it transmits
// immediately, then retransmits a finite number of times on a decaying
// schedule (pkg/cot). Settings are a singleton every target snapshots
// at creation time; see dto/cot.go.
func (s *Server) registerCot(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/cot-settings", s.getCotSettings)
	mux.HandleFunc("PUT /api/cot-settings", s.updateCotSettings)
	mux.HandleFunc("GET /api/cot-targets", s.listCotTargets)
	mux.HandleFunc("POST /api/cot-targets", s.createCotTarget)
	mux.HandleFunc("POST /api/cot-targets/{id}/send", s.sendCotTargetNow)
	// Registered before the {id} pattern below; Go 1.22's ServeMux
	// resolves the literal "inactive" segment in preference to the
	// wildcard for that exact path, so DELETE /api/cot-targets/inactive
	// never falls through to deleteCotTarget with id="inactive".
	mux.HandleFunc("DELETE /api/cot-targets/inactive", s.deleteInactiveCotTargets)
	mux.HandleFunc("DELETE /api/cot-targets/{id}", s.deleteCotTarget)
}

// getCotSettings returns the active CoT settings.
//
// @Summary     Get Cursor-on-Target settings
// @Description Returns the global parameters every new CoT target
// @Description snapshots at creation time. Returns defaults when no
// @Description configuration has been saved yet.
// @Tags        cot
// @ID          getCotSettings
// @Produce     json
// @Success     200 {object} dto.CotSettingsResponse
// @Failure     500 {object} webtypes.ErrorResponse
// @Security    CookieAuth
// @Router      /cot-settings [get]
func (s *Server) getCotSettings(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetCotSettings(r.Context())
	if err != nil {
		s.internalError(w, r, "get cot settings", err)
		return
	}
	if c == nil {
		writeJSON(w, http.StatusOK, dto.CotSettingsDefaults())
		return
	}
	writeJSON(w, http.StatusOK, dto.CotSettingsFromModel(*c))
}

// updateCotSettings replaces the singleton CoT settings.
//
// @Summary     Update Cursor-on-Target settings
// @Description Replaces the global CoT parameters. Does not affect any
// @Description CoT target already created -- each target snapshots
// @Description these settings for itself at creation time.
// @Tags        cot
// @ID          updateCotSettings
// @Accept      json
// @Produce     json
// @Param       body body     dto.CotSettingsRequest true "CoT settings"
// @Success     200  {object} dto.CotSettingsResponse
// @Failure     400  {object} webtypes.ErrorResponse
// @Failure     409  {object} webtypes.ErrorResponse
// @Failure     500  {object} webtypes.ErrorResponse
// @Security    CookieAuth
// @Router      /cot-settings [put]
func (s *Server) updateCotSettings(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[dto.CotSettingsRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	ctx := r.Context()
	if req.SendPath != beacon.SendPathISOnly {
		if err := dto.ValidateChannelRef(ctx, s.store, "channel", req.Channel); err != nil {
			badRequest(w, err.Error())
			return
		}
		if err := s.requireTxCapableChannel(ctx, "channel", req.Channel); err != nil {
			writeJSON(w, http.StatusConflict, webtypes.ErrorResponse{Error: err.Error()})
			return
		}
	}
	m := req.ToModel()
	if err := s.store.UpsertCotSettings(ctx, &m); err != nil {
		s.internalError(w, r, "upsert cot settings", err)
		return
	}
	writeJSON(w, http.StatusOK, dto.CotSettingsFromModel(m))
}

// listCotTargets returns every CoT target (active and inactive; the
// frontend splits tabs on the response's `active` field).
//
// @Summary  List Cursor-on-Target objects
// @Tags     cot
// @ID       listCotTargets
// @Produce  json
// @Success  200 {array}  dto.CotTargetResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /cot-targets [get]
func (s *Server) listCotTargets(w http.ResponseWriter, r *http.Request) {
	handleList[configstore.CotTarget](s, w, r, "list cot targets",
		s.store.ListCotTargets, dto.CotTargetFromModel)
}

// createCotTarget creates a new CoT target, snapshots the current CoT
// settings onto it, and fires its immediate first transmit.
//
// @Summary     Create a Cursor-on-Target object
// @Description Snapshots the current CoT settings onto the new target
// @Description and transmits it immediately under the station
// @Description callsign. The immediate send is best-effort: on failure
// @Description the target is still created and the scheduler retries
// @Description it on its next poll.
// @Tags        cot
// @ID          createCotTarget
// @Accept      json
// @Produce     json
// @Param       body body     dto.CotTargetRequest true "CoT target definition"
// @Success     201  {object} dto.CotTargetResponse
// @Failure     400  {object} webtypes.ErrorResponse
// @Failure     409  {object} webtypes.ErrorResponse
// @Failure     422  {object} webtypes.ErrorResponse
// @Failure     500  {object} webtypes.ErrorResponse
// @Security    CookieAuth
// @Router      /cot-targets [post]
func (s *Server) createCotTarget(w http.ResponseWriter, r *http.Request) {
	handleCreate[dto.CotTargetRequest](s, w, r, "create cot target",
		func(ctx context.Context, req dto.CotTargetRequest) (configstore.CotTarget, error) {
			// CoT objects always transmit under the inherited station
			// callsign (no per-target override, per spec) -- refuse
			// up front rather than creating a target that can never
			// send, mirroring the CW test-signal callsign guard.
			if _, err := s.store.ResolveStationCallsign(ctx); err != nil {
				switch {
				case errors.Is(err, callsign.ErrCallsignEmpty):
					return configstore.CotTarget{}, validationError(errors.New("set your station callsign before adding a Cursor-on-Target"))
				case errors.Is(err, callsign.ErrCallsignN0Call):
					return configstore.CotTarget{}, validationError(errors.New("station callsign is still N0CALL; set a real callsign before adding a Cursor-on-Target"))
				default:
					return configstore.CotTarget{}, err
				}
			}

			settings, err := s.store.GetCotSettings(ctx)
			if err != nil {
				return configstore.CotTarget{}, err
			}
			sm := cot.DefaultSettings()
			if settings != nil {
				sm = *settings
			}

			if sm.SendPath != beacon.SendPathISOnly {
				if err := s.requireTxCapableChannel(ctx, "channel", sm.Channel); err != nil {
					return configstore.CotTarget{}, validationError(err)
				}
			}

			m := req.ToModel(sm)
			if err := s.store.CreateCotTarget(ctx, &m); err != nil {
				return configstore.CotTarget{}, err
			}

			// Immediate first send (TX1), best-effort: a failure here
			// is logged and the row is left due, so the scheduler's
			// next 5s poll retries it automatically instead of the
			// target silently never transmitting.
			if s.cotSendScheduled != nil {
				if sendErr := s.cotSendScheduled(ctx, m.ID); sendErr != nil {
					s.logger.Warn("cot immediate send failed; will retry on next poll", "id", m.ID, "err", sendErr)
				} else if updated, getErr := s.store.GetCotTarget(ctx, m.ID); getErr == nil && updated != nil {
					m = *updated
				}
			}
			return m, nil
		},
		dto.CotTargetFromModel)
}

// sendCotTargetNow triggers an immediate one-shot "Beacon Now"
// transmission of the CoT target, without affecting its TxCount.
//
// @Summary  Send a Cursor-on-Target object now
// @Tags     cot
// @ID       sendCotTarget
// @Produce  json
// @Param    id  path     int true "CoT target id"
// @Success  200 {object} dto.CotSendResponse
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Failure  409 {object} webtypes.ErrorResponse
// @Failure  422 {object} webtypes.ErrorResponse
// @Failure  503 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /cot-targets/{id}/send [post]
func (s *Server) sendCotTargetNow(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	if s.cotSendNow == nil {
		writeJSON(w, http.StatusServiceUnavailable, webtypes.ErrorResponse{Error: "cot scheduler not available"})
		return
	}
	if _, err := s.store.GetCotTarget(r.Context(), id); err != nil {
		notFound(w)
		return
	}
	if err := s.cotSendNow(r.Context(), id); err != nil {
		var se *cot.SendError
		if errors.As(err, &se) {
			switch se.Kind {
			case cot.SendErrorCallsign, cot.SendErrorEncode:
				writeJSON(w, http.StatusUnprocessableEntity, webtypes.ErrorResponse{Error: se.Error()})
				return
			case cot.SendErrorChannelMode:
				writeJSON(w, http.StatusConflict, webtypes.ErrorResponse{Error: se.Error()})
				return
			case cot.SendErrorSubmit:
				writeJSON(w, http.StatusServiceUnavailable, webtypes.ErrorResponse{Error: se.Error()})
				return
			}
		}
		s.internalError(w, r, "cot send now", err)
		return
	}
	writeJSON(w, http.StatusOK, dto.CotSendResponse{Status: "sent"})
}

// deleteCotTarget removes a single CoT target. Sends a best-effort APRS
// object-kill report first (see sendCotKillBestEffort) so compliant
// stations drop the object from their own display; the delete proceeds
// regardless of whether the kill transmit succeeds.
//
// @Summary  Delete a Cursor-on-Target object
// @Tags     cot
// @ID       deleteCotTarget
// @Param    id  path int true "CoT target id"
// @Success  204 "No Content"
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /cot-targets/{id} [delete]
func (s *Server) deleteCotTarget(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	handleDelete(s, w, r, "delete cot target", id, func(ctx context.Context, id uint32) error {
		s.sendCotKillBestEffort(ctx, id)
		return s.store.DeleteCotTarget(ctx, id)
	})
}

// deleteInactiveCotTargets bulk-deletes every exhausted CoT target, for
// the Inactive tab's "Delete all" button. Sends a best-effort kill
// report for each row before the bulk delete.
//
// @Summary  Delete every inactive (exhausted) Cursor-on-Target object
// @Tags     cot
// @ID       deleteInactiveCotTargets
// @Produce  json
// @Success  200 {object} dto.CotDeleteInactiveResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /cot-targets/inactive [delete]
func (s *Server) deleteInactiveCotTargets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	all, err := s.store.ListCotTargets(ctx)
	if err != nil {
		s.internalError(w, r, "list cot targets for bulk delete", err)
		return
	}
	for _, t := range all {
		if t.TxCount >= t.NumTransmits {
			s.sendCotKillBestEffort(ctx, t.ID)
		}
	}
	n, err := s.store.DeleteInactiveCotTargets(ctx)
	if err != nil {
		s.internalError(w, r, "delete inactive cot targets", err)
		return
	}
	writeJSON(w, http.StatusOK, dto.CotDeleteInactiveResponse{Deleted: n})
}

// sendCotKillBestEffort transmits a one-shot APRS object-kill report for
// a CoT target about to be deleted. Best-effort: a failure only logs a
// warning, since an operator's delete request should not be blocked by
// a transmit problem (unreachable channel, unset callsign, etc).
func (s *Server) sendCotKillBestEffort(ctx context.Context, id uint32) {
	if s.cotSendKill == nil {
		return
	}
	if err := s.cotSendKill(ctx, id); err != nil {
		s.logger.Warn("cot kill send failed", "id", id, "err", err)
	}
}
