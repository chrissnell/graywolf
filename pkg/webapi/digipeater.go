package webapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/digipeater/blocklist"
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
	mux.HandleFunc("GET /api/digipeater/blocklist", s.listDigipeaterBlocklist)
	mux.HandleFunc("POST /api/digipeater/blocklist", s.createDigipeaterBlocklist)
	mux.HandleFunc("PUT /api/digipeater/blocklist/{id}", s.updateDigipeaterBlocklist)
	mux.HandleFunc("DELETE /api/digipeater/blocklist/{id}", s.deleteDigipeaterBlocklist)
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
	ctx := r.Context()
	// Enable-guard (centralized station-callsign plan D7 rule 1):
	// reject any request that flips Enabled=true while the station
	// callsign is empty or N0CALL. Saves with Enabled=false proceed.
	if err := s.requireStationCallsignForEnable(ctx, req.Enabled); err != nil {
		badRequest(w, err.Error())
		return
	}
	// Merge the request onto the existing stored config so
	// my_call=nil (field omitted) preserves the stored override
	// rather than silently clearing it. See dto.DigipeaterConfigRequest
	// contract: nil = leave unchanged, "" = inherit, non-empty =
	// override.
	existingPtr, err := s.store.GetDigipeaterConfig(ctx)
	if err != nil {
		s.internalError(w, r, "get digipeater config", err)
		return
	}
	var existing configstore.DigipeaterConfig
	if existingPtr != nil {
		existing = *existingPtr
	}
	m := req.ApplyToModel(existing)
	if err := s.store.UpsertDigipeaterConfig(ctx, &m); err != nil {
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
			if err := dto.ValidateChannelRef(ctx, s.store, "from_channel", req.FromChannel); err != nil {
				return configstore.DigipeaterRule{}, validationError(err)
			}
			if err := dto.ValidateChannelRef(ctx, s.store, "to_channel", req.ToChannel); err != nil {
				return configstore.DigipeaterRule{}, validationError(err)
			}
			// TX-capability gate (plan D2): both from_channel and
			// to_channel must be TX-capable — a digipeater rule without
			// a usable to_channel silently drops every matched frame,
			// and from_channel needs TX too because a same-channel
			// rule loops back out the same modem. Gate runs only when
			// the rule is enabled so operators can stage broken rules
			// under Enabled=false while they reshape their channel
			// config (plan D3 escape hatch).
			if req.Enabled {
				if err := s.requireTxCapableChannel(ctx, "from_channel", req.FromChannel); err != nil {
					return configstore.DigipeaterRule{}, validationError(err)
				}
				if err := s.requireTxCapableChannel(ctx, "to_channel", req.ToChannel); err != nil {
					return configstore.DigipeaterRule{}, validationError(err)
				}
			}
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
			if err := dto.ValidateChannelRef(ctx, s.store, "from_channel", req.FromChannel); err != nil {
				return configstore.DigipeaterRule{}, validationError(err)
			}
			if err := dto.ValidateChannelRef(ctx, s.store, "to_channel", req.ToChannel); err != nil {
				return configstore.DigipeaterRule{}, validationError(err)
			}
			// TX-capability gate (plan D2): both from_channel and
			// to_channel must be TX-capable — a digipeater rule without
			// a usable to_channel silently drops every matched frame,
			// and from_channel needs TX too because a same-channel
			// rule loops back out the same modem. Gate runs only when
			// the rule is enabled so operators can stage broken rules
			// under Enabled=false while they reshape their channel
			// config (plan D3 escape hatch).
			if req.Enabled {
				if err := s.requireTxCapableChannel(ctx, "from_channel", req.FromChannel); err != nil {
					return configstore.DigipeaterRule{}, validationError(err)
				}
				if err := s.requireTxCapableChannel(ctx, "to_channel", req.ToChannel); err != nil {
					return configstore.DigipeaterRule{}, validationError(err)
				}
			}
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

// reasonMaxLen caps user-supplied reason text. 256 chars is enough for
// any sensible operator note and keeps the column small.
const reasonMaxLen = 256

// listDigipeaterBlocklist returns every entry id-ascending.
//
// @Summary  List digipeater blocklist entries
// @Tags     digipeater
// @ID       listDigipeaterBlocklist
// @Produce  json
// @Success  200 {array}  dto.BlocklistEntryResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /digipeater/blocklist [get]
func (s *Server) listDigipeaterBlocklist(w http.ResponseWriter, r *http.Request) {
	handleList[configstore.DigipeaterBlocklist](s, w, r, "list digipeater blocklist",
		s.store.ListDigipeaterBlocklist, dto.BlocklistEntryFromModel)
}

// createDigipeaterBlocklist validates the pattern, persists the entry,
// and fires the digipeater reload signal so the live engine picks up
// the new list. Duplicate patterns return 409.
//
// @Summary  Create digipeater blocklist entry
// @Tags     digipeater
// @ID       createDigipeaterBlocklist
// @Accept   json
// @Produce  json
// @Param    body body     dto.BlocklistEntryRequest true "Blocklist entry"
// @Success  201  {object} dto.BlocklistEntryResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  409  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /digipeater/blocklist [post]
func (s *Server) createDigipeaterBlocklist(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[dto.BlocklistEntryRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	canonical, err := blocklist.ValidatePattern(req.Pattern)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	m := configstore.DigipeaterBlocklist{
		Pattern: canonical,
		Reason:  trimReason(req.Reason),
		Enabled: enabled,
	}
	if err := s.store.CreateDigipeaterBlocklistEntry(r.Context(), &m); err != nil {
		if isUniqueConstraintErr(err) {
			conflict(w, "pattern already exists")
			return
		}
		s.internalError(w, r, "create digipeater blocklist", err)
		return
	}
	s.signalDigipeaterReload()
	writeJSON(w, http.StatusCreated, dto.BlocklistEntryFromModel(m))
}

// updateDigipeaterBlocklist replaces the entry with the given id.
//
// @Summary  Update digipeater blocklist entry
// @Tags     digipeater
// @ID       updateDigipeaterBlocklist
// @Accept   json
// @Produce  json
// @Param    id   path     int                       true "Entry id"
// @Param    body body     dto.BlocklistEntryRequest true "Blocklist entry"
// @Success  200  {object} dto.BlocklistEntryResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  404  {object} webtypes.ErrorResponse
// @Failure  409  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /digipeater/blocklist/{id} [put]
func (s *Server) updateDigipeaterBlocklist(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	req, err := decodeJSON[dto.BlocklistEntryRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	canonical, err := blocklist.ValidatePattern(req.Pattern)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	m := configstore.DigipeaterBlocklist{
		ID:      id,
		Pattern: canonical,
		Reason:  trimReason(req.Reason),
		Enabled: enabled,
	}
	if err := s.store.UpdateDigipeaterBlocklistEntry(r.Context(), &m); err != nil {
		if isUniqueConstraintErr(err) {
			conflict(w, "pattern already exists")
			return
		}
		s.internalError(w, r, "update digipeater blocklist", err)
		return
	}
	s.signalDigipeaterReload()
	writeJSON(w, http.StatusOK, dto.BlocklistEntryFromModel(m))
}

// deleteDigipeaterBlocklist removes the entry with the given id.
//
// @Summary  Delete digipeater blocklist entry
// @Tags     digipeater
// @ID       deleteDigipeaterBlocklist
// @Param    id  path int true "Entry id"
// @Success  204 "No Content"
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /digipeater/blocklist/{id} [delete]
func (s *Server) deleteDigipeaterBlocklist(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	if err := s.store.DeleteDigipeaterBlocklistEntry(r.Context(), id); err != nil {
		s.internalError(w, r, "delete digipeater blocklist", err)
		return
	}
	s.signalDigipeaterReload()
	w.WriteHeader(http.StatusNoContent)
}

// trimReason normalizes operator-supplied reason text: trim whitespace
// and cap at reasonMaxLen bytes so a misconfigured client can't bloat
// the DB.
func trimReason(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > reasonMaxLen {
		s = s[:reasonMaxLen]
	}
	return s
}
