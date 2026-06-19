package webapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/chrissnell/graywolf/pkg/bulletins"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
	"gorm.io/gorm"
)

// BulletinService is the narrow surface the webapi handlers consume
// from pkg/bulletins.Service.
type BulletinService interface {
	Send(ctx context.Context, req bulletins.SendRequest) (*configstore.Bulletin, error)
	List(ctx context.Context, f bulletins.Filter) ([]configstore.Bulletin, error)
	Delete(ctx context.Context, id uint64) error
	MarkRead(ctx context.Context, id uint64) error
	MarkAllRead(ctx context.Context) error
}

// SetBulletinService installs the bulletin service. Until called,
// bulletin handlers return 503.
func (s *Server) SetBulletinService(svc BulletinService) { s.bulletinService = svc }

func (s *Server) requireBulletinSvc(w http.ResponseWriter) (BulletinService, bool) {
	if s.bulletinService == nil {
		serviceUnavailable(w, "bulletin service not configured")
		return nil, false
	}
	return s.bulletinService, true
}

func (s *Server) registerBulletins(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/bulletins", s.listBulletins)
	mux.HandleFunc("POST /api/bulletins", s.sendBulletin)
	mux.HandleFunc("DELETE /api/bulletins/{id}", s.deleteBulletin)
	mux.HandleFunc("POST /api/bulletins/{id}/read", s.markBulletinRead)
	mux.HandleFunc("POST /api/bulletins/read-all", s.markAllBulletinsRead)
}

// GET /api/bulletins?direction=in|out
func (s *Server) listBulletins(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.requireBulletinSvc(w)
	if !ok {
		return
	}
	f := bulletins.Filter{
		Direction:  r.URL.Query().Get("direction"),
		UnreadOnly: r.URL.Query().Get("unread_only") == "true",
	}
	rows, err := svc.List(r.Context(), f)
	if err != nil {
		s.internalError(w, r, "list bulletins", err)
		return
	}
	resp := make([]dto.BulletinResponse, len(rows))
	for i, b := range rows {
		resp[i] = dto.BulletinFromModel(b)
	}
	writeJSON(w, http.StatusOK, resp)
}

// POST /api/bulletins
func (s *Server) sendBulletin(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.requireBulletinSvc(w)
	if !ok {
		return
	}
	req, err := decodeJSON[dto.SendBulletinRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	b, err := svc.Send(r.Context(), bulletins.SendRequest{
		Slot: strings.ToUpper(strings.TrimSpace(req.Slot)),
		Text: strings.TrimSpace(req.Text),
	})
	if err != nil {
		s.internalError(w, r, "send bulletin", err)
		return
	}
	writeJSON(w, http.StatusCreated, dto.BulletinFromModel(*b))
}

// DELETE /api/bulletins/{id}
func (s *Server) deleteBulletin(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.requireBulletinSvc(w)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	if err := svc.Delete(r.Context(), id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w)
			return
		}
		s.internalError(w, r, "delete bulletin", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/bulletins/{id}/read
func (s *Server) markBulletinRead(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.requireBulletinSvc(w)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	if err := svc.MarkRead(r.Context(), id); err != nil {
		s.internalError(w, r, "mark bulletin read", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/bulletins/read-all
func (s *Server) markAllBulletinsRead(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.requireBulletinSvc(w)
	if !ok {
		return
	}
	if err := svc.MarkAllRead(r.Context()); err != nil {
		s.internalError(w, r, "mark all bulletins read", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
