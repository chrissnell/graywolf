package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/chrissnell/graywolf/pkg/callsign"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/kiss"
	"github.com/chrissnell/graywolf/pkg/modembridge"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
	"github.com/chrissnell/graywolf/pkg/webtypes"
	"gorm.io/gorm"
)

// registerChannels installs the /api/channels route tree on mux using
// Go 1.22+ method-scoped patterns. Each route maps to exactly one
// handler. Subpath dispatch and `switch r.Method` are gone — the table
// here is the authoritative list.
//
// Operation IDs used in the swag annotation blocks below are frozen
// against the constants in pkg/webapi/docs/op_ids.go. The
// `make docs-lint` target enforces the correspondence.
func (s *Server) registerChannels(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/channels", s.listChannels)
	mux.HandleFunc("POST /api/channels", s.createChannel)
	mux.HandleFunc("GET /api/channels/{id}", s.getChannel)
	mux.HandleFunc("PUT /api/channels/{id}", s.updateChannel)
	mux.HandleFunc("DELETE /api/channels/{id}", s.deleteChannel)
	mux.HandleFunc("GET /api/channels/{id}/stats", s.getChannelStats)
	mux.HandleFunc("GET /api/channels/{id}/referrers", s.getChannelReferrers)
	mux.HandleFunc("POST /api/channels/{id}/ptt", s.manualPtt)
	mux.HandleFunc("POST /api/channels/{id}/test-tx", s.sendTestSignal)
}

// listChannels returns every configured channel.
//
// @Summary  List channels
// @Tags     channels
// @ID       listChannels
// @Produce  json
// @Success  200 {array}  dto.ChannelResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /channels [get]
func (s *Server) listChannels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	chs, err := s.store.ListChannels(ctx)
	if err != nil {
		s.internalError(w, r, "list channels", err)
		return
	}
	ifaces, err := s.store.ListKissInterfaces(ctx)
	if err != nil {
		s.internalError(w, r, "list channels", err)
		return
	}
	// PTT rows are looked up once and indexed by channel id so the
	// per-card render stays O(1). A missing row maps to nil Ptt — the
	// UI treats that as "never configured" (distinct from method=none).
	ptts, err := s.store.ListPttConfigs(ctx)
	if err != nil {
		s.internalError(w, r, "list channels", err)
		return
	}
	pttByChannel := make(map[uint32]configstore.PttConfig, len(ptts))
	for _, p := range ptts {
		pttByChannel[p.ChannelID] = p
	}
	statuses := s.kissStatus()
	modemLive := s.modemRunning()
	resp := make([]dto.ChannelResponse, len(chs))
	for i, c := range chs {
		resp[i] = dto.ChannelFromModel(c)
		b := computeChannelBacking(c, ifaces, statuses, modemLive)
		resp[i].Backing = &b
		if p, ok := pttByChannel[c.ID]; ok {
			ptt := dto.ChannelPttFromModel(p)
			resp[i].Ptt = &ptt
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// createChannel creates a new channel from the request body and
// returns the persisted record (with its assigned id) on success.
//
// @Summary  Create channel
// @Tags     channels
// @ID       createChannel
// @Accept   json
// @Produce  json
// @Param    body body     dto.ChannelRequest true "Channel definition"
// @Success  201  {object} dto.ChannelResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /channels [post]
func (s *Server) createChannel(w http.ResponseWriter, r *http.Request) {
	handleCreate[dto.ChannelRequest](s, w, r, "create channel",
		func(ctx context.Context, req dto.ChannelRequest) (configstore.Channel, error) {
			m := req.ToModel()
			return m, s.store.CreateChannel(ctx, &m)
		},
		dto.ChannelFromModel)
}

// getChannel returns the channel with the given id.
//
// @Summary  Get channel
// @Tags     channels
// @ID       getChannel
// @Produce  json
// @Param    id  path     int true "Channel id"
// @Success  200 {object} dto.ChannelResponse
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /channels/{id} [get]
func (s *Server) getChannel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	handleGet[*configstore.Channel](s, w, r, "get channel", id,
		s.store.GetChannel,
		func(c *configstore.Channel) dto.ChannelResponse {
			resp := dto.ChannelFromModel(*c)
			ifaces, err := s.store.ListKissInterfaces(r.Context())
			if err != nil {
				// Best effort: fall back to empty list so backing still
				// renders with just the modem side.
				ifaces = nil
			}
			b := computeChannelBacking(*c, ifaces, s.kissStatus(), s.modemRunning())
			resp.Backing = &b
			// Best-effort PTT lookup: ErrRecordNotFound is the common
			// case (no PttConfig row), and we map it to nil Ptt so the
			// UI can show "PTT not configured" rather than treating it
			// as a server error. Any OTHER store error gets logged —
			// without that signal, "my PTT-configured channel says
			// 'Not configured'" troubleshooting (the very flow this
			// indicator exists to support) goes silent.
			p, perr := s.store.GetPttConfigForChannel(r.Context(), c.ID)
			switch {
			case perr == nil:
				ptt := dto.ChannelPttFromModel(*p)
				resp.Ptt = &ptt
			case errors.Is(perr, gorm.ErrRecordNotFound):
				// Expected: no PTT row for this channel.
			default:
				s.logger.Warn("get channel: load ptt config", "channel", c.ID, "err", perr)
			}
			return resp
		})
}

// kissStatus returns a non-nil snapshot map of every managed KISS
// interface's lifecycle state. Falls back to an empty map when the
// manager is absent (test harnesses, early startup).
func (s *Server) kissStatus() map[uint32]kiss.InterfaceStatus {
	if s.kissManager == nil {
		return map[uint32]kiss.InterfaceStatus{}
	}
	return s.kissManager.Status()
}

// resolveChannelTxCapability computes the current TxCapability for a
// single channel id. Returns (cap, true, nil) when the channel exists,
// (zero, false, nil) when the channel id is unknown (so callers can
// fall through to the existing "channel N does not exist" error path),
// and (zero, false, err) on store failure.
//
// Used by the beacon / iGate / digipeater POST+PUT validators, which
// run AFTER dto.ValidateChannelRef and therefore already know the
// channel exists in the common case; the (found==false) branch guards
// against a racing delete between the two lookups.
func (s *Server) resolveChannelTxCapability(ctx context.Context, channelID uint32) (dto.TxCapability, bool, error) {
	ch, err := s.store.GetChannel(ctx, channelID)
	if err != nil {
		// gorm.ErrRecordNotFound → not-found path. Any other error is a
		// real store failure — surface it so the handler can emit 500.
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.TxCapability{}, false, nil
		}
		return dto.TxCapability{}, false, err
	}
	ifaces, err := s.store.ListKissInterfaces(ctx)
	if err != nil {
		return dto.TxCapability{}, false, err
	}
	b := computeChannelBacking(*ch, ifaces, s.kissStatus(), s.modemRunning())
	return b.Tx, true, nil
}

// modemRunning reports whether the Rust modem subprocess is currently
// running and exchanging heartbeats. False when the bridge is absent
// (tests) so channels that carry an input device are still reported
// with the correct summary (modem configured) but with health=down.
func (s *Server) modemRunning() bool {
	if s.bridge == nil {
		return false
	}
	return s.bridge.IsRunning()
}

// computeChannelBacking derives the backing object for a channel from
// its configuration plus the live kiss + modem state. Pure function —
// no I/O — so the computation is trivial to test and cheap to run on
// every /api/channels request.
//
// Summary is modem when the channel has a bound input audio device,
// kiss-tnc when it has ≥1 TNC-mode KISS interface attached, unbound
// otherwise. Dual-backend is explicitly forbidden by design decision
// D3 so the precedence order here matters only for that currently-
// impossible case: Phase 3 adds the write-time validator that rejects
// the combination; until then, a channel that somehow has both will
// render as modem-backed and the TNC interfaces will be listed for
// diagnostic visibility.
//
// Health is live when at least one live backend exists (modem running
// or ≥1 TNC interface in a live state), down when backends exist but
// none are live, and unbound when no backend is configured.
func computeChannelBacking(
	ch configstore.Channel,
	ifaces []configstore.KissInterface,
	statuses map[uint32]kiss.InterfaceStatus,
	modemRunning bool,
) dto.ChannelBacking {
	// Phase 2 switched InputDeviceID to *uint32 (nullable). A nil
	// value is the canonical "KISS-only channel" marker — no audio
	// modem was ever configured for it. Before Phase 2 this check
	// was `ch.InputDeviceID != 0`, which was a rough equivalent but
	// relied on the column's NOT NULL constraint + the validator
	// rejecting zero; the pointer check is the authoritative signal.
	hasModem := ch.InputDeviceID != nil
	modem := dto.ChannelModemBacking{Active: hasModem && modemRunning}
	// A channel with no audio input device has no modem at all; the
	// modem sub-object is dead state. Leaving Reason empty stops it
	// leaking through the channel-picker tooltip on KISS-only and
	// unbound channels (the summary already conveys the real state).
	if hasModem && !modemRunning {
		modem.Reason = "modem subprocess not running"
	}

	// Always return a non-nil slice so JSON renders [] rather than null.
	tncEntries := make([]dto.ChannelKissTncEntry, 0)
	tncLiveCount := 0
	for _, iface := range ifaces {
		if iface.Channel != ch.ID {
			continue
		}
		if iface.Mode != configstore.KissModeTnc {
			continue
		}
		st, running := statuses[iface.ID]
		entry := dto.ChannelKissTncEntry{
			InterfaceID:         iface.ID,
			InterfaceName:       iface.Name,
			AllowTxFromGovernor: iface.AllowTxFromGovernor,
			State:               st.State,
			LastError:           st.LastError,
			RetryAtUnixMs:       st.RetryAtUnixMs,
		}
		if !running {
			entry.State = kiss.StateStopped
		}
		if isKissLive(entry.State) {
			tncLiveCount++
		}
		tncEntries = append(tncEntries, entry)
	}

	backing := dto.ChannelBacking{
		Modem:   modem,
		KissTnc: tncEntries,
		Tx:      computeTxCapability(ch, tncEntries),
	}
	switch {
	case hasModem:
		backing.Summary = dto.ChannelBackingSummaryModem
		if modem.Active {
			backing.Health = dto.ChannelBackingHealthLive
		} else {
			backing.Health = dto.ChannelBackingHealthDown
		}
	case len(tncEntries) > 0:
		backing.Summary = dto.ChannelBackingSummaryKissTnc
		if tncLiveCount > 0 {
			backing.Health = dto.ChannelBackingHealthLive
		} else {
			backing.Health = dto.ChannelBackingHealthDown
		}
	default:
		backing.Summary = dto.ChannelBackingSummaryUnbound
		backing.Health = dto.ChannelBackingHealthUnbound
	}
	return backing
}

// computeTxCapability is the single source of truth for the
// "can this channel TX?" question consumed by the server-side referrer
// validator and by the frontend picker predicate. Pure function, derived
// from the same channel + kiss fields computeChannelBacking already has
// in hand.
//
// Decision order (single branch per plan D1 — the KISS short-circuit
// first so a KISS-only channel with InputDeviceID == nil reports
// Capable=true rather than "no input device configured"):
//
//	len(tncEntries) > 0        → Capable=true, Reason=""
//	ch.InputDeviceID == nil    → Capable=false, Reason="no input device configured"
//	ch.OutputDeviceID == 0     → Capable=false, Reason="no output device configured"
//	else                       → Capable=true, Reason=""
//
// Note: this treats any configured TNC-mode KISS interface as a usable
// TX path regardless of its live state. "Live state" is a runtime
// property (is the listener accepting? is the tcp-client connected?) and
// churns on a timescale shorter than the operator's config loop; we
// don't want editing a beacon to be blocked because a KISS server hasn't
// come up yet. The dispatcher's at-TX-time snapshot is the authoritative
// "is this deliverable right now" gate; TxCapability is the "is this
// configured correctly" gate.
func computeTxCapability(ch configstore.Channel, tncEntries []dto.ChannelKissTncEntry) dto.TxCapability {
	if len(tncEntries) > 0 {
		return dto.TxCapability{Capable: true}
	}
	if ch.InputDeviceID == nil {
		return dto.TxCapability{Capable: false, Reason: dto.TxReasonNoInputDevice}
	}
	if ch.OutputDeviceID == 0 {
		return dto.TxCapability{Capable: false, Reason: dto.TxReasonNoOutputDevice}
	}
	return dto.TxCapability{Capable: true}
}

// isKissLive reports whether a KISS interface State string represents
// a currently-live backend (one that can accept TX). Phase 1 treats
// "listening" as live (server-listen accepts new clients even with
// zero connected); Phase 4 adds "connected" for tcp-client.
func isKissLive(state string) bool {
	switch state {
	case kiss.StateListening, kiss.StateConnected:
		return true
	}
	return false
}

// updateChannel replaces the channel with the given id using the
// request body and returns the persisted record.
//
// Referrer guard: before committing the write, the handler computes the
// would-be post-mutation TxCapability and collects any existing
// referrers (beacons, iGate TX/RF channel, digipeater rules, KISS
// interfaces, RF filters, tx-timings) that point at the channel. If
// the channel was TX-capable before the edit but would no longer be
// after, the handler responds with 409 Conflict + the referrer list
// unless the request carries ?force=true. This mirrors the cascade-
// delete flow (see deleteChannel) so the UI can reuse its confirm
// dialog.
//
// @Summary  Update channel
// @Tags     channels
// @ID       updateChannel
// @Accept   json
// @Produce  json
// @Param    id    path     int                true "Channel id"
// @Param    force query    bool               false "Force the update even if it would break existing TX referrers"
// @Param    body  body     dto.ChannelRequest true "Channel definition"
// @Success  200  {object} dto.ChannelResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  409  {object} ChannelReferrersResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /channels/{id} [put]
func (s *Server) updateChannel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	req, err := decodeJSON[dto.ChannelRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	ctx := r.Context()
	force := r.URL.Query().Get("force") == "true"

	// Referrer guard: compute the TxCapability before and after the edit
	// and compare. Only the "was capable, would no longer be" transition
	// blocks — if the channel is already broken, the referrers are
	// already broken and this edit doesn't make things worse.
	if !force {
		existing, gerr := s.store.GetChannel(ctx, id)
		if gerr == nil && existing != nil {
			ifaces, ierr := s.store.ListKissInterfaces(ctx)
			if ierr != nil {
				s.internalError(w, r, "update channel: list kiss interfaces", ierr)
				return
			}
			statuses := s.kissStatus()
			modemLive := s.modemRunning()

			before := computeChannelBacking(*existing, ifaces, statuses, modemLive).Tx
			after := computeChannelBacking(req.ToUpdate(id), ifaces, statuses, modemLive).Tx

			if before.Capable && !after.Capable {
				refs, rerr := s.store.ChannelReferrers(ctx, id)
				if rerr != nil {
					s.internalError(w, r, "update channel: channel referrers", rerr)
					return
				}
				if len(refs.Items) > 0 {
					writeJSON(w, http.StatusConflict, ChannelReferrersResponse{
						Error:     "channel update would break existing TX referrers: " + after.Reason,
						Referrers: refs.Items,
					})
					return
				}
			}
		}
		// If GetChannel returned an error here we fall through to the
		// store.UpdateChannel call below, which will surface the
		// nonexistent-row error through the usual 500 path. We prefer
		// not to 404 here because the existing contract for this
		// endpoint doesn't 404 on missing ids (GORM .Save() inserts
		// when the PK is absent); staying consistent with that.
	}

	m := req.ToUpdate(id)
	if err := s.store.UpdateChannel(ctx, &m); err != nil {
		if v := isValidationErr(err); v != nil {
			badRequest(w, v.Error())
			return
		}
		s.internalError(w, r, "update channel", err)
		return
	}
	s.notifyBridgeReload(ctx)
	writeJSON(w, http.StatusOK, dto.ChannelFromModel(m))
}

// ChannelReferrersResponse is the body returned by
// GET /api/channels/{id}/referrers and by DELETE /api/channels/{id}
// when referrers exist and cascade is not requested (409 Conflict). The
// Error field is populated only on the 409 path so the wire shape stays
// stable between the two endpoints.
type ChannelReferrersResponse struct {
	Error     string                 `json:"error,omitempty"`
	Referrers []configstore.Referrer `json:"referrers"`
}

// getChannelReferrers returns the list of rows that reference the
// channel with the given id via a soft-FK column. Powers the first
// dialog in the UI's two-step delete flow (D12): the operator sees the
// impact list before committing to a cascade delete.
//
// @Summary  List referrers of a channel
// @Tags     channels
// @ID       getChannelReferrers
// @Produce  json
// @Param    id  path     int true "Channel id"
// @Success  200 {object} ChannelReferrersResponse
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /channels/{id}/referrers [get]
func (s *Server) getChannelReferrers(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	// Verify the channel exists first so a typo returns a clean 404
	// instead of an empty referrers list (which would ambiguously mean
	// "channel exists but has no refs" — the UI's second dialog needs
	// the channel row to render the typed-name gate).
	if _, err := s.store.GetChannel(r.Context(), id); err != nil {
		notFound(w)
		return
	}
	refs, err := s.store.ChannelReferrers(r.Context(), id)
	if err != nil {
		s.internalError(w, r, "channel referrers", err)
		return
	}
	writeJSON(w, http.StatusOK, ChannelReferrersResponse{Referrers: refs.Items})
}

// deleteChannel removes the channel with the given id. The default
// behavior refuses to delete a channel that is referenced by any row in
// beacons / digipeater_rules / kiss_interfaces / i_gate_configs /
// i_gate_rf_filters / tx_timings — the handler walks ChannelReferrers
// and returns 409 Conflict with the impact list (D12) so the UI can
// surface it to the operator.
//
// Passing ?cascade=true applies the per-table policy atomically (see
// configstore.DeleteChannelCascade): beacons / digi rules / filters /
// timings are deleted; kiss interfaces have their Channel nulled +
// NeedsReconfig set (the operator is expected to reassign + save); the
// iGate singleton has RfChannel / TxChannel nulled. A single
// notifyBridgeReload fires post-commit so in-memory state reconverges
// once, not N times.
//
// @Summary  Delete channel
// @Tags     channels
// @ID       deleteChannel
// @Param    id       path  int    true  "Channel id"
// @Param    cascade  query bool   false "Cascade per-table deletes / nulls; 409 without it when referrers exist"
// @Success  204 "No Content"
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Failure  409 {object} ChannelReferrersResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /channels/{id} [delete]
func (s *Server) deleteChannel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	ctx := r.Context()

	// Verify the channel exists first. A DELETE on a nonexistent row
	// should be a clear 404 rather than a silent 204 (idempotent DELETE
	// is a style choice; graywolf has always preferred explicit
	// not-found for the delete surface).
	if _, err := s.store.GetChannel(ctx, id); err != nil {
		notFound(w)
		return
	}

	cascade := r.URL.Query().Get("cascade") == "true"

	if !cascade {
		refs, err := s.store.ChannelReferrers(ctx, id)
		if err != nil {
			s.internalError(w, r, "channel referrers", err)
			return
		}
		if len(refs.Items) > 0 {
			writeJSON(w, http.StatusConflict, ChannelReferrersResponse{
				Error:     "channel has references",
				Referrers: refs.Items,
			})
			return
		}
		if err := s.store.DeleteChannel(ctx, id); err != nil {
			s.internalError(w, r, "delete channel", err)
			return
		}
		s.notifyBridgeReload(ctx)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Cascade path: single transactional sweep across every referring
	// table per D12, followed by one bridge+dispatcher reload notify.
	if _, err := s.store.DeleteChannelCascade(ctx, id); err != nil {
		s.internalError(w, r, "cascade delete channel", err)
		return
	}
	s.notifyBridgeReload(ctx)
	w.WriteHeader(http.StatusNoContent)
}

// getChannelStats returns live stats for the channel from the running
// modem bridge. Not CRUD — talks to the bridge rather than the
// configstore, so it stays a bespoke handler.
//
// @Summary  Get channel stats
// @Tags     channels
// @ID       getChannelStats
// @Produce  json
// @Param    id  path     int true "Channel id"
// @Success  200 {object} modembridge.ChannelStats
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Failure  503 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /channels/{id}/stats [get]
func (s *Server) getChannelStats(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid channel id")
		return
	}
	if s.bridge != nil {
		if stats, ok := s.bridge.GetChannelStats(id); ok {
			writeJSON(w, http.StatusOK, stats)
			return
		}
	}
	// KISS-TNC-backed channels have no Rust modem feeding the bridge
	// cache; surface the KISS manager's per-channel counters instead
	// so the dashboard and this endpoint stop reporting a stuck zero
	// (issue #132). RxBadFCS is omitted (always 0 for a hardware TNC).
	if s.kissManager != nil {
		if ks, ok := s.kissManager.ChannelStats(id); ok {
			writeJSON(w, http.StatusOK, &modembridge.ChannelStats{
				Channel:  id,
				RxFrames: ks.RxFrames,
				TxFrames: ks.TxFrames,
			})
			return
		}
	}
	if s.bridge == nil {
		writeJSON(w, http.StatusServiceUnavailable, webtypes.ErrorResponse{Error: "bridge not available"})
		return
	}
	notFound(w)
}

// CW / tone recipe constants — the single source of the hardcoded test-signal
// parameters. The four UI options map to these.
const (
	cwTestWpm       = 20
	cwTestToneHz    = 700
	toneTestDurMs   = 3000
	altTestPeriodMs = 200
	toneTestLowHz   = 1200
	toneTestHighHz  = 2400
)

// sendTestSignal transmits a TX test signal (CW callsign or a tone) on a
// channel. The "cw" signal refuses to key the radio when the station callsign
// is empty or N0CALL; tone signals need no callsign. All signals require a
// TX-capable channel.
//
// @Summary  Send a TX test signal (CW callsign or tone) on a channel
// @Tags     channels
// @ID       sendTestSignal
// @Accept   json
// @Produce  json
// @Param    id path int true "Channel ID"
// @Param    body body dto.TestSignalRequest true "Signal to send"
// @Success  200 {object} dto.TestSignalResponse
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  409 {object} webtypes.ErrorResponse
// @Failure  422 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Failure  503 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /channels/{id}/test-tx [post]
func (s *Server) sendTestSignal(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid channel id")
		return
	}
	if s.bridge == nil {
		writeJSON(w, http.StatusServiceUnavailable, webtypes.ErrorResponse{Error: "bridge not available"})
		return
	}

	var req dto.TestSignalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}

	// Validate the signal (400) before checking channel capability (409) so a
	// malformed request is rejected on its own terms; then the more
	// fundamental TX-capability error precedes the cw-only callsign guard (422).
	params, ok := buildTestSignalParams(id, req.Signal)
	if !ok {
		badRequest(w, "unknown signal: "+req.Signal)
		return
	}

	if err := s.requireTxCapableChannel(r.Context(), "channel", id); err != nil {
		writeJSON(w, http.StatusConflict, webtypes.ErrorResponse{Error: err.Error()})
		return
	}

	// The cw signal keys the radio with the station callsign; refuse if unset.
	if req.Signal == "cw" {
		call, err := s.store.ResolveStationCallsign(r.Context())
		if err != nil {
			switch {
			case errors.Is(err, callsign.ErrCallsignEmpty):
				writeJSON(w, http.StatusUnprocessableEntity, webtypes.ErrorResponse{Error: "set your station callsign before sending CW ID"})
			case errors.Is(err, callsign.ErrCallsignN0Call):
				writeJSON(w, http.StatusUnprocessableEntity, webtypes.ErrorResponse{Error: "station callsign is still N0CALL; set a real callsign before sending CW ID"})
			default:
				s.internalError(w, r, "resolve station callsign", err)
			}
			return
		}
		params.Callsign = call
	}

	if err := s.bridge.TransmitTestSignal(r.Context(), params); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, webtypes.ErrorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, dto.TestSignalResponse{Status: "sent"})
}

// buildTestSignalParams maps a UI signal id to modem TX test-signal parameters.
// The "cw" case leaves Callsign empty for the caller to fill from resolved
// station config. ok is false for an unrecognized signal id.
func buildTestSignalParams(channelID uint32, signal string) (modembridge.TestSignalParams, bool) {
	params := modembridge.TestSignalParams{Channel: channelID}
	switch signal {
	case "cw":
		params.Kind = 0 // CW callsign
		params.CwWpm = cwTestWpm
		params.FreqAHz = cwTestToneHz
	case "tone1200":
		params.Kind = 1 // steady tone
		params.FreqAHz = toneTestLowHz
		params.DurationMs = toneTestDurMs
	case "tone2400":
		params.Kind = 1 // steady tone
		params.FreqAHz = toneTestHighHz
		params.DurationMs = toneTestDurMs
	case "alt":
		params.Kind = 2 // alternating tone
		params.FreqAHz = toneTestLowHz
		params.FreqBHz = toneTestHighHz
		params.DurationMs = toneTestDurMs
		params.AltPeriodMs = altTestPeriodMs
	default:
		return modembridge.TestSignalParams{}, false
	}
	return params, true
}

// manualPtt keys or unkeys the radio on the given channel for SPA testing.
// A 10-second watchdog on the bridge will auto-unkey if no heartbeat arrives.
// The SPA is expected to POST {"keyed":true} every 2s while holding PTT and
// POST {"keyed":false} on release.
//
// @Summary  Manual PTT key/unkey
// @Tags     channels
// @ID       manualPtt
// @Accept   json
// @Param    id   path     int               true "Channel id"
// @Param    body body     object{keyed=bool} true "PTT state"
// @Success  204 "No Content"
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  503 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /channels/{id}/ptt [post]
func (s *Server) manualPtt(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid channel id")
		return
	}
	if s.bridge == nil {
		writeJSON(w, http.StatusServiceUnavailable, webtypes.ErrorResponse{Error: "bridge not available"})
		return
	}
	var req struct {
		Keyed bool `json:"keyed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	if err := s.bridge.ManualPttWithWatchdog(id, req.Keyed); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, webtypes.ErrorResponse{Error: err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
