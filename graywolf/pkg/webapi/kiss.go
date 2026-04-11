package webapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/kiss"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerKiss(mux *http.ServeMux) {
	mux.HandleFunc("/api/kiss", s.handleKissCollection)
	mux.HandleFunc("/api/kiss/", s.handleKissItem)
}

// GET/POST /api/kiss
func (s *Server) handleKissCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleList[configstore.KissInterface](s, w, r, "list kiss interfaces",
			s.store.ListKissInterfaces, dto.KissFromModel)
	case http.MethodPost:
		handleCreate[dto.KissRequest](s, w, r, "create kiss interface",
			func(ctx context.Context, req dto.KissRequest) (configstore.KissInterface, error) {
				m := req.ToModel()
				if err := s.store.CreateKissInterface(ctx, &m); err != nil {
					return configstore.KissInterface{}, err
				}
				s.notifyKissManager(m)
				return m, nil
			},
			dto.KissFromModel)
	default:
		methodNotAllowed(w)
	}
}

// GET/PUT/DELETE /api/kiss/{id}
func (s *Server) handleKissItem(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(strings.TrimPrefix(r.URL.Path, "/api/kiss/"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	switch r.Method {
	case http.MethodGet:
		handleGet[*configstore.KissInterface](s, w, r, id,
			s.store.GetKissInterface,
			func(k *configstore.KissInterface) dto.KissResponse {
				return dto.KissFromModel(*k)
			})
	case http.MethodPut:
		handleUpdate[dto.KissRequest](s, w, r, "update kiss interface", id,
			func(ctx context.Context, id uint32, req dto.KissRequest) (configstore.KissInterface, error) {
				m := req.ToUpdate(id)
				if err := s.store.UpdateKissInterface(ctx, &m); err != nil {
					return configstore.KissInterface{}, err
				}
				s.notifyKissManager(m)
				return m, nil
			},
			dto.KissFromModel)
	case http.MethodDelete:
		handleDelete(s, w, r, "delete kiss interface", id, func(ctx context.Context, id uint32) error {
			if err := s.store.DeleteKissInterface(ctx, id); err != nil {
				return err
			}
			if s.kissManager != nil {
				s.kissManager.Stop(id)
			}
			return nil
		})
	default:
		methodNotAllowed(w)
	}
}

// notifyKissManager starts or restarts the KISS server for the given
// interface. For non-TCP or disabled interfaces the server is stopped.
func (s *Server) notifyKissManager(ki configstore.KissInterface) {
	if s.kissManager == nil {
		return
	}
	if !ki.Enabled || ki.InterfaceType != "tcp" || ki.ListenAddr == "" {
		s.kissManager.Stop(ki.ID)
		return
	}
	ch := ki.Channel
	if ch == 0 {
		ch = 1
	}
	s.kissManager.Start(s.kissCtx, ki.ID, kiss.ServerConfig{
		Name:       ki.Name,
		ListenAddr: ki.ListenAddr,
		Logger:     s.logger,
		ChannelMap: map[uint8]uint32{0: ch},
		Broadcast:  ki.Broadcast,
	})
}
