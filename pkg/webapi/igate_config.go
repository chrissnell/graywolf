package webapi

import (
	"context"
	"net/http"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// registerIgateConfig installs the /api/igate/config and
// /api/igate/filters route trees on mux using Go 1.22+ method-scoped
// patterns.
func (s *Server) registerIgateConfig(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/igate/config", s.getIgateConfig)
	mux.HandleFunc("PUT /api/igate/config", s.updateIgateConfig)
	mux.HandleFunc("GET /api/igate/filters", s.listIgateFilters)
	mux.HandleFunc("POST /api/igate/filters", s.createIgateFilter)
	mux.HandleFunc("PUT /api/igate/filters/{id}", s.updateIgateFilter)
	mux.HandleFunc("DELETE /api/igate/filters/{id}", s.deleteIgateFilter)
}

// getIgateConfig returns the singleton igate config. If no config row
// has been written yet the zero-value DTO is returned with 200 so the
// UI always gets a valid body to render defaults from.
//
// @Summary  Get igate config
// @Tags     igate
// @ID       getIgateConfig
// @Produce  json
// @Success  200 {object} dto.IGateConfigResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /igate/config [get]
func (s *Server) getIgateConfig(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetIGateConfig(r.Context())
	if err != nil {
		s.internalError(w, r, "get igate config", err)
		return
	}
	if c == nil {
		writeJSON(w, http.StatusOK, dto.IGateConfigFromModel(configstore.IGateConfig{}))
		return
	}
	writeJSON(w, http.StatusOK, dto.IGateConfigFromModel(*c))
}

// updateIgateConfig replaces the singleton igate config.
//
// @Summary  Update igate config
// @Tags     igate
// @ID       updateIgateConfig
// @Accept   json
// @Produce  json
// @Param    body body     dto.IGateConfigRequest true "Igate config"
// @Success  200  {object} dto.IGateConfigResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /igate/config [put]
func (s *Server) updateIgateConfig(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[dto.IGateConfigRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	ctx := r.Context()
	// Idempotent pass-through for req.Validate(): a legacy persisted
	// server_filter that contains `|` (saved by an older binary that
	// allowed it) must not block saves of unrelated fields. The DTO
	// validator only fires when the operator actually changed the
	// value, mirroring the FK-reference logic below. The frontend
	// applies the same rule on its serverFilterError preflight.
	existingServerFilter := loadIGateServerFilter(ctx, s.store)
	if req.ServerFilter != existingServerFilter {
		if err := req.Validate(); err != nil {
			badRequest(w, err.Error())
			return
		}
	}
	// Enable-guard (centralized station-callsign plan D7 rule 1):
	// reject any request that flips Enabled=true while the station
	// callsign is empty or N0CALL. Saves with Enabled=false, and
	// saves that leave Enabled unchanged at false, proceed. Runs
	// before the channel-ref validation so the user sees the
	// actionable error (set your callsign) rather than a channel FK
	// failure they hit along the way.
	if err := s.requireStationCallsignForEnable(ctx, req.Enabled); err != nil {
		badRequest(w, err.Error())
		return
	}
	// Cross-table: rf_channel and tx_channel are soft-FKs on
	// configstore.Channel.ID. A non-zero value that doesn't resolve
	// should land as a 400 before the upsert — the iGate singleton is
	// always writable (UpsertIGateConfig never rejects), so this is
	// the only gate on orphan refs.
	//
	// Idempotent pass-through: when the request value matches the
	// persisted value, skip the existence check. Without this, an FK
	// that became orphaned out-of-band (channel deleted via a path
	// that bypassed the cascade, schema migration, etc.) traps the
	// operator — the broken value is not exposed in the UI (e.g.
	// rf_channel has no field on the iGate page) so they cannot
	// supply a non-orphaned replacement, yet every save round-trips
	// the same broken value back through the validator and 400s.
	// Skipping when unchanged lets the operator save other fields
	// while the orphan FK is healed elsewhere (cascade on delete,
	// runtime resolveTxChannel fallback).
	existingRf, existingTx := loadIGateChannelRefs(ctx, s.store)
	if req.RfChannel != existingRf {
		if err := dto.ValidateChannelRef(ctx, s.store, "rf_channel", req.RfChannel); err != nil {
			badRequest(w, err.Error())
			return
		}
	}
	if req.TxChannel != existingTx {
		if err := dto.ValidateChannelRef(ctx, s.store, "tx_channel", req.TxChannel); err != nil {
			badRequest(w, err.Error())
			return
		}
	}
	// TX-capability gate (plan D2): the tx_channel must actually be able
	// to transmit. rf_channel is RX-only from the iGate's perspective,
	// so don't gate it on TX-capability. The check is skipped when the
	// iGate as a whole is disabled (Enabled=false) — per plan D3, a
	// disabled referrer on a broken channel is harmless and the goal is
	// to block silently-broken *active* config, not to trap operators
	// in a modal while they're turning things off. Also skipped on
	// idempotent saves (req.TxChannel == existingTx) so an orphaned or
	// no-longer-TX-capable persisted value can't trap the operator;
	// resolveTxChannel handles the runtime fallback in that case.
	if req.Enabled && req.TxChannel != existingTx {
		if err := s.requireTxCapableChannel(ctx, "tx_channel", req.TxChannel); err != nil {
			badRequest(w, err.Error())
			return
		}
	}
	m := req.ToModel()
	if err := s.store.UpsertIGateConfig(ctx, &m); err != nil {
		s.internalError(w, r, "upsert igate config", err)
		return
	}
	s.signalIgateReload()
	// iGate's TxChannel field is also the source of truth for messages
	// outbound RF channel (see app.wireMessages). Signal the messages
	// reload so a saved TxChannel propagates into the running Sender +
	// Router without a daemon restart.
	s.signalMessagesReload()
	writeJSON(w, http.StatusOK, dto.IGateConfigFromModel(m))
}

// loadIGateChannelRefs returns the persisted RfChannel and TxChannel
// values for the iGate singleton, or (0, 0) when no row exists or the
// fetch errors. Used by updateIgateConfig to support idempotent
// pass-through validation: a request that does not change either FK
// is allowed through even when the persisted value points at a
// non-existent (orphaned) channel, so the operator can still save
// other fields while the orphan is healed elsewhere.
func loadIGateChannelRefs(ctx context.Context, store *configstore.Store) (rf, tx uint32) {
	cfg, err := store.GetIGateConfig(ctx)
	if err != nil || cfg == nil {
		return 0, 0
	}
	return cfg.RfChannel, cfg.TxChannel
}

// loadIGateServerFilter returns the persisted ServerFilter for the
// iGate singleton, or "" when no row exists or the fetch errors.
// Used by updateIgateConfig to skip DTO Validate() (which rejects `|`
// in server_filter) when the request value is unchanged from the
// persisted value, so a legacy filter saved by an older binary that
// allowed pipes does not trap the operator on saves of unrelated
// fields.
func loadIGateServerFilter(ctx context.Context, store *configstore.Store) string {
	cfg, err := store.GetIGateConfig(ctx)
	if err != nil || cfg == nil {
		return ""
	}
	return cfg.ServerFilter
}

// signalIgateReload performs a non-blocking send on the igate reload
// channel; coalesces if a previous signal is still buffered.
func (s *Server) signalIgateReload() {
	if s.igateReload == nil {
		return
	}
	select {
	case s.igateReload <- struct{}{}:
	default:
	}
}

// listIgateFilters returns every configured igate RF filter.
//
// @Summary  List igate RF filters
// @Tags     igate
// @ID       listIgateFilters
// @Produce  json
// @Success  200 {array}  dto.IGateRfFilterResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /igate/filters [get]
func (s *Server) listIgateFilters(w http.ResponseWriter, r *http.Request) {
	handleList[configstore.IGateRfFilter](s, w, r, "list igate rf filters",
		s.store.ListIGateRfFilters, dto.IGateRfFilterFromModel)
}

// createIgateFilter creates a new igate RF filter from the request
// body and returns the persisted record (with its assigned id).
//
// @Summary  Create igate RF filter
// @Tags     igate
// @ID       createIgateFilter
// @Accept   json
// @Produce  json
// @Param    body body     dto.IGateRfFilterRequest true "Igate RF filter definition"
// @Success  201  {object} dto.IGateRfFilterResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /igate/filters [post]
func (s *Server) createIgateFilter(w http.ResponseWriter, r *http.Request) {
	handleCreate[dto.IGateRfFilterRequest](s, w, r, "create igate rf filter",
		func(ctx context.Context, req dto.IGateRfFilterRequest) (configstore.IGateRfFilter, error) {
			if err := dto.ValidateChannelRef(ctx, s.store, "channel", req.Channel); err != nil {
				return configstore.IGateRfFilter{}, validationError(err)
			}
			m := req.ToModel()
			if err := s.store.CreateIGateRfFilter(ctx, &m); err != nil {
				return configstore.IGateRfFilter{}, err
			}
			s.signalIgateReload()
			return m, nil
		},
		dto.IGateRfFilterFromModel)
}

// updateIgateFilter replaces the igate RF filter with the given id
// using the request body and returns the persisted record.
//
// @Summary  Update igate RF filter
// @Tags     igate
// @ID       updateIgateFilter
// @Accept   json
// @Produce  json
// @Param    id   path     int                      true "Igate RF filter id"
// @Param    body body     dto.IGateRfFilterRequest true "Igate RF filter definition"
// @Success  200  {object} dto.IGateRfFilterResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /igate/filters/{id} [put]
func (s *Server) updateIgateFilter(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	handleUpdate[dto.IGateRfFilterRequest](s, w, r, "update igate rf filter", id,
		func(ctx context.Context, id uint32, req dto.IGateRfFilterRequest) (configstore.IGateRfFilter, error) {
			if err := dto.ValidateChannelRef(ctx, s.store, "channel", req.Channel); err != nil {
				return configstore.IGateRfFilter{}, validationError(err)
			}
			m := req.ToUpdate(id)
			if err := s.store.UpdateIGateRfFilter(ctx, &m); err != nil {
				return configstore.IGateRfFilter{}, err
			}
			s.signalIgateReload()
			return m, nil
		},
		dto.IGateRfFilterFromModel)
}

// deleteIgateFilter removes the igate RF filter with the given id.
//
// @Summary  Delete igate RF filter
// @Tags     igate
// @ID       deleteIgateFilter
// @Param    id  path int true "Igate RF filter id"
// @Success  204 "No Content"
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /igate/filters/{id} [delete]
func (s *Server) deleteIgateFilter(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	handleDelete(s, w, r, "delete igate rf filter", id, func(ctx context.Context, id uint32) error {
		if err := s.store.DeleteIGateRfFilter(ctx, id); err != nil {
			return err
		}
		s.signalIgateReload()
		return nil
	})
}
