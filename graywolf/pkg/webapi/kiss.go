package webapi

import (
	"context"
	"net/http"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/kiss"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// registerKiss installs the /api/kiss route tree on mux using
// Go 1.22+ method-scoped patterns. See channels.go for the reference.
func (s *Server) registerKiss(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/kiss", s.listKiss)
	mux.HandleFunc("POST /api/kiss", s.createKiss)
	mux.HandleFunc("GET /api/kiss/{id}", s.getKiss)
	mux.HandleFunc("PUT /api/kiss/{id}", s.updateKiss)
	mux.HandleFunc("DELETE /api/kiss/{id}", s.deleteKiss)
	mux.HandleFunc("POST /api/kiss/{id}/reconnect", s.reconnectKiss)
}

// attachKissStatus folds live manager status onto every response DTO.
// Called from the listKiss / getKiss code paths so the Kiss page shows
// supervisor telemetry (state, last_error, retry_at_unix_ms, peer_addr,
// connected_since, reconnect_count, backoff_seconds) without a second
// round-trip. Status for server-listen interfaces reports
// StateListening; tcp-client interfaces report their supervisor state.
func (s *Server) attachKissStatus(out []dto.KissResponse) []dto.KissResponse {
	if s.kissManager == nil {
		return out
	}
	st := s.kissManager.Status()
	for i := range out {
		if is, ok := st[out[i].ID]; ok {
			out[i].State = is.State
			out[i].LastError = is.LastError
			out[i].RetryAtUnixMs = is.RetryAtUnixMs
			out[i].PeerAddr = is.PeerAddr
			out[i].ConnectedSince = is.ConnectedSince
			out[i].ReconnectCount = is.ReconnectCount
			out[i].BackoffSeconds = is.BackoffSeconds
		}
	}
	return out
}

// listKiss returns every configured KISS interface.
//
// @Summary  List KISS interfaces
// @Tags     kiss
// @ID       listKiss
// @Produce  json
// @Success  200 {array}  dto.KissResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /kiss [get]
func (s *Server) listKiss(w http.ResponseWriter, r *http.Request) {
	models, err := s.store.ListKissInterfaces(r.Context())
	if err != nil {
		s.internalError(w, r, "list kiss interfaces", err)
		return
	}
	out := make([]dto.KissResponse, len(models))
	for i, m := range models {
		out[i] = dto.KissFromModel(m)
	}
	out = s.attachKissStatus(out)
	writeJSON(w, http.StatusOK, out)
}

// createKiss creates a new KISS interface from the request body and
// returns the persisted record (with its assigned id) on success.
//
// @Summary  Create KISS interface
// @Tags     kiss
// @ID       createKiss
// @Accept   json
// @Produce  json
// @Param    body body     dto.KissRequest true "KISS interface definition"
// @Success  201  {object} dto.KissResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /kiss [post]
func (s *Server) createKiss(w http.ResponseWriter, r *http.Request) {
	handleCreate[dto.KissRequest](s, w, r, "create kiss interface",
		func(ctx context.Context, req dto.KissRequest) (configstore.KissInterface, error) {
			if err := dto.ValidateChannelRef(ctx, s.store, "channel", req.Channel); err != nil {
				return configstore.KissInterface{}, validationError(err)
			}
			m := req.ToModel()
			if err := s.store.CreateKissInterface(ctx, &m); err != nil {
				return configstore.KissInterface{}, err
			}
			s.notifyKissManager(m)
			return m, nil
		},
		dto.KissFromModel)
}

// getKiss returns the KISS interface with the given id.
//
// @Summary  Get KISS interface
// @Tags     kiss
// @ID       getKiss
// @Produce  json
// @Param    id  path     int true "KISS interface id"
// @Success  200 {object} dto.KissResponse
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /kiss/{id} [get]
func (s *Server) getKiss(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	handleGet[*configstore.KissInterface](s, w, r, "get kiss interface", id,
		s.store.GetKissInterface,
		func(k *configstore.KissInterface) dto.KissResponse {
			out := dto.KissFromModel(*k)
			spliced := s.attachKissStatus([]dto.KissResponse{out})
			return spliced[0]
		})
}

// updateKiss replaces the KISS interface with the given id using the
// request body and returns the persisted record.
//
// @Summary  Update KISS interface
// @Tags     kiss
// @ID       updateKiss
// @Accept   json
// @Produce  json
// @Param    id   path     int             true "KISS interface id"
// @Param    body body     dto.KissRequest true "KISS interface definition"
// @Success  200  {object} dto.KissResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /kiss/{id} [put]
func (s *Server) updateKiss(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	handleUpdate[dto.KissRequest](s, w, r, "update kiss interface", id,
		func(ctx context.Context, id uint32, req dto.KissRequest) (configstore.KissInterface, error) {
			if err := dto.ValidateChannelRef(ctx, s.store, "channel", req.Channel); err != nil {
				return configstore.KissInterface{}, validationError(err)
			}
			m := req.ToUpdate(id)
			// When the operator reassigns a valid channel, clear the
			// cascade-set NeedsReconfig flag so the "reconfigure me"
			// banner drops off the row (D12). Zero stays flagged until
			// a non-zero channel is chosen.
			if m.Channel != 0 {
				m.NeedsReconfig = false
			}
			if err := s.store.UpdateKissInterface(ctx, &m); err != nil {
				return configstore.KissInterface{}, err
			}
			s.notifyKissManager(m)
			return m, nil
		},
		dto.KissFromModel)
}

// deleteKiss removes the KISS interface with the given id.
//
// @Summary  Delete KISS interface
// @Tags     kiss
// @ID       deleteKiss
// @Param    id  path int true "KISS interface id"
// @Success  204 "No Content"
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /kiss/{id} [delete]
func (s *Server) deleteKiss(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	handleDelete(s, w, r, "delete kiss interface", id, func(ctx context.Context, id uint32) error {
		if err := s.store.DeleteKissInterface(ctx, id); err != nil {
			return err
		}
		if s.kissManager != nil {
			s.kissManager.Stop(id)
		}
		s.notifyTxBackendReload()
		return nil
	})
}

// notifyKissManager starts or restarts the KISS server for the given
// interface. For non-TCP or disabled interfaces the server is stopped.
// Tcp-client interfaces dispatch to StartClient (Phase 4). After any
// state change the TX backend reload signal is nudged so the Phase 3
// dispatcher's registry snapshot rebuilds to reflect the new tx queue
// membership.
func (s *Server) notifyKissManager(ki configstore.KissInterface) {
	defer s.notifyTxBackendReload()
	if s.kissManager == nil {
		return
	}
	if !ki.Enabled {
		s.kissManager.Stop(ki.ID)
		return
	}
	ch := ki.Channel
	if ch == 0 {
		ch = 1
	}
	mode := kiss.Mode(ki.Mode)
	if mode == "" {
		mode = kiss.ModeModem
	}
	switch ki.InterfaceType {
	case configstore.KissTypeTCPClient:
		if ki.RemoteHost == "" || ki.RemotePort == 0 {
			s.kissManager.Stop(ki.ID)
			return
		}
		reload := s.notifyTxBackendReload
		s.kissManager.StartClient(s.kissCtx, ki.ID, kiss.ClientConfig{
			Name:                ki.Name,
			RemoteHost:          ki.RemoteHost,
			RemotePort:          ki.RemotePort,
			ReconnectInitMs:     ki.ReconnectInitMs,
			ReconnectMaxMs:      ki.ReconnectMaxMs,
			Logger:              s.logger,
			ChannelMap:          map[uint8]uint32{0: ch},
			Mode:                mode,
			TncIngressRateHz:    ki.TncIngressRateHz,
			TncIngressBurst:     ki.TncIngressBurst,
			AllowTxFromGovernor: ki.AllowTxFromGovernor,
			OnReload:            reload,
		})
	case configstore.KissTypeTCP:
		if ki.ListenAddr == "" {
			s.kissManager.Stop(ki.ID)
			return
		}
		s.kissManager.Start(s.kissCtx, ki.ID, kiss.ServerConfig{
			Name:                ki.Name,
			ListenAddr:          ki.ListenAddr,
			Logger:              s.logger,
			ChannelMap:          map[uint8]uint32{0: ch},
			Broadcast:           ki.Broadcast,
			Mode:                mode,
			TncIngressRateHz:    ki.TncIngressRateHz,
			TncIngressBurst:     ki.TncIngressBurst,
			AllowTxFromGovernor: ki.AllowTxFromGovernor,
		})
	default:
		// Serial / bluetooth paths aren't wired through the manager
		// today; stop any lingering session.
		s.kissManager.Stop(ki.ID)
	}
}

// reconnectKiss cancels the backoff wait on a running tcp-client and
// dials immediately. Returns 200 on success, 404 when the interface
// isn't registered with the manager (either deleted or not tcp-client),
// or 409 when the interface exists but is not a tcp-client supervisor.
//
// @Summary  Reconnect a KISS tcp-client interface now
// @Tags     kiss
// @ID       reconnectKiss
// @Param    id  path int true "KISS interface id"
// @Success  200 "OK"
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Failure  409 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /kiss/{id}/reconnect [post]
func (s *Server) reconnectKiss(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	if s.kissManager == nil {
		http.Error(w, "kiss manager unavailable", http.StatusServiceUnavailable)
		return
	}
	// Verify the row is actually a tcp-client before asking the
	// manager — a server-listen row would hit the "not a tcp-client"
	// branch inside the manager and we'd still want to return 409.
	ki, err := s.store.GetKissInterface(r.Context(), id)
	if err != nil || ki == nil {
		notFound(w)
		return
	}
	if ki.InterfaceType != configstore.KissTypeTCPClient {
		http.Error(w, "interface is not a tcp-client", http.StatusConflict)
		return
	}
	if err := s.kissManager.Reconnect(id); err != nil {
		// Manager-level 409: row is configured but not currently
		// running (stopped via Enabled=false, or not yet started).
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusOK)
}
