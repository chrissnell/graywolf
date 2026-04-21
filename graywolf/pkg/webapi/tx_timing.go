package webapi

import (
	"context"
	"net/http"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// registerTxTiming installs the /api/tx-timing route tree on mux using
// Go 1.22+ method-scoped patterns. The `{channel}` path param is a
// channel id (per-channel singleton timing config); the resource has
// no DELETE — the store exposes no equivalent method and the config
// is upserted rather than removed.
func (s *Server) registerTxTiming(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tx-timing", s.listTxTiming)
	mux.HandleFunc("POST /api/tx-timing", s.createTxTiming)
	mux.HandleFunc("GET /api/tx-timing/{id}", s.getTxTiming)
	mux.HandleFunc("PUT /api/tx-timing/{id}", s.updateTxTiming)
}

// listTxTiming returns every configured tx-timing record.
//
// @Summary  List tx-timing records
// @Tags     tx-timing
// @ID       listTxTiming
// @Produce  json
// @Success  200 {array}  dto.TxTimingResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /tx-timing [get]
func (s *Server) listTxTiming(w http.ResponseWriter, r *http.Request) {
	handleList[configstore.TxTiming](s, w, r, "list tx timings",
		s.store.ListTxTimings, dto.TxTimingFromModel)
}

// createTxTiming upserts a tx-timing record for the channel referenced
// in the body and returns the persisted record.
//
// @Summary  Upsert tx-timing record
// @Tags     tx-timing
// @ID       createTxTiming
// @Accept   json
// @Produce  json
// @Param    body body     dto.TxTimingRequest true "Tx-timing definition"
// @Success  201  {object} dto.TxTimingResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /tx-timing [post]
func (s *Server) createTxTiming(w http.ResponseWriter, r *http.Request) {
	handleCreate[dto.TxTimingRequest](s, w, r, "upsert tx timing",
		func(ctx context.Context, req dto.TxTimingRequest) (configstore.TxTiming, error) {
			if err := dto.ValidateChannelRef(ctx, s.store, "channel", req.Channel); err != nil {
				return configstore.TxTiming{}, validationError(err)
			}
			m := req.ToModel()
			if err := s.store.UpsertTxTiming(ctx, &m); err != nil {
				return configstore.TxTiming{}, err
			}
			s.notifyBridgeForChannel(ctx, m.Channel)
			return m, nil
		},
		dto.TxTimingFromModel)
}

// getTxTiming returns the tx-timing record for the given channel id.
//
// @Summary  Get tx-timing record
// @Tags     tx-timing
// @ID       getTxTiming
// @Produce  json
// @Param    id  path     int true "Channel id"
// @Success  200 {object} dto.TxTimingResponse
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /tx-timing/{id} [get]
func (s *Server) getTxTiming(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid channel id")
		return
	}
	handleGet[*configstore.TxTiming](s, w, r, "get tx timing", id,
		s.store.GetTxTiming,
		func(t *configstore.TxTiming) dto.TxTimingResponse {
			return dto.TxTimingFromModel(*t)
		})
}

// updateTxTiming upserts the tx-timing record for the given channel id
// using the request body and returns the persisted record.
//
// @Summary  Update tx-timing record
// @Tags     tx-timing
// @ID       updateTxTiming
// @Accept   json
// @Produce  json
// @Param    id   path     int                 true "Channel id"
// @Param    body body     dto.TxTimingRequest true "Tx-timing definition"
// @Success  200  {object} dto.TxTimingResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /tx-timing/{id} [put]
func (s *Server) updateTxTiming(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid channel id")
		return
	}
	handleUpdate[dto.TxTimingRequest](s, w, r, "upsert tx timing", id,
		func(ctx context.Context, channel uint32, req dto.TxTimingRequest) (configstore.TxTiming, error) {
			// Channel id comes from the URL path, not the body — validate
			// the URL-bound value so updating per-channel timing for a
			// nonexistent channel lands as a 400.
			if err := dto.ValidateChannelRef(ctx, s.store, "channel", channel); err != nil {
				return configstore.TxTiming{}, validationError(err)
			}
			m := req.ToUpdate(channel)
			if err := s.store.UpsertTxTiming(ctx, &m); err != nil {
				return configstore.TxTiming{}, err
			}
			s.notifyBridgeForChannel(ctx, channel)
			return m, nil
		},
		dto.TxTimingFromModel)
}
