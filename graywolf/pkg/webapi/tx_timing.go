package webapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerTxTiming(mux *http.ServeMux) {
	mux.HandleFunc("/api/tx-timing", s.handleTxTimingCollection)
	mux.HandleFunc("/api/tx-timing/", s.handleTxTimingByChannel)
}

// GET/POST /api/tx-timing
func (s *Server) handleTxTimingCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleList[configstore.TxTiming](s, w, r, "list tx timings",
			s.store.ListTxTimings, dto.TxTimingFromModel)
	case http.MethodPost:
		handleCreate[dto.TxTimingRequest](s, w, r, "upsert tx timing",
			func(ctx context.Context, req dto.TxTimingRequest) (configstore.TxTiming, error) {
				m := req.ToModel()
				if err := s.store.UpsertTxTiming(ctx, &m); err != nil {
					return configstore.TxTiming{}, err
				}
				s.notifyBridgeForChannel(ctx, m.Channel)
				return m, nil
			},
			dto.TxTimingFromModel)
	default:
		methodNotAllowed(w)
	}
}

// GET/PUT /api/tx-timing/{channel}
func (s *Server) handleTxTimingByChannel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(strings.TrimPrefix(r.URL.Path, "/api/tx-timing/"))
	if err != nil {
		badRequest(w, "invalid channel id")
		return
	}
	switch r.Method {
	case http.MethodGet:
		handleGet[*configstore.TxTiming](s, w, r, id,
			s.store.GetTxTiming,
			func(t *configstore.TxTiming) dto.TxTimingResponse {
				return dto.TxTimingFromModel(*t)
			})
	case http.MethodPut:
		handleUpdate[dto.TxTimingRequest](s, w, r, "upsert tx timing", id,
			func(ctx context.Context, channel uint32, req dto.TxTimingRequest) (configstore.TxTiming, error) {
				m := req.ToUpdate(channel)
				if err := s.store.UpsertTxTiming(ctx, &m); err != nil {
					return configstore.TxTiming{}, err
				}
				s.notifyBridgeForChannel(ctx, channel)
				return m, nil
			},
			dto.TxTimingFromModel)
	default:
		methodNotAllowed(w)
	}
}
