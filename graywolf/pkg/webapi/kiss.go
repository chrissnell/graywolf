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
	handleList[configstore.KissInterface](s, w, r, "list kiss interfaces",
		s.store.ListKissInterfaces, dto.KissFromModel)
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
			return dto.KissFromModel(*k)
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
			m := req.ToUpdate(id)
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
		return nil
	})
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
	mode := kiss.Mode(ki.Mode)
	if mode == "" {
		mode = kiss.ModeModem
	}
	s.kissManager.Start(s.kissCtx, ki.ID, kiss.ServerConfig{
		Name:             ki.Name,
		ListenAddr:       ki.ListenAddr,
		Logger:           s.logger,
		ChannelMap:       map[uint8]uint32{0: ch},
		Broadcast:        ki.Broadcast,
		Mode:             mode,
		TncIngressRateHz: ki.TncIngressRateHz,
		TncIngressBurst:  ki.TncIngressBurst,
	})
}
