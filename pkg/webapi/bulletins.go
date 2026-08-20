package webapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
	"github.com/chrissnell/graywolf/pkg/webtypes"
)

// registerBulletins installs the /api/bulletins route tree on mux.
func (s *Server) registerBulletins(mux *http.ServeMux) {
	s.registerBulletinsConfig(mux)
	mux.HandleFunc("GET /api/bulletins", s.listBulletinGroups)
	mux.HandleFunc("POST /api/bulletins", s.createBulletinGroup)
	mux.HandleFunc("GET /api/bulletins/{id}", s.getBulletinGroup)
	mux.HandleFunc("PUT /api/bulletins/{id}", s.updateBulletinGroup)
	mux.HandleFunc("DELETE /api/bulletins/{id}", s.deleteBulletinGroup)
	mux.HandleFunc("PUT /api/bulletins/{id}/items/{slot}", s.upsertBulletinItem)
	mux.HandleFunc("DELETE /api/bulletins/{id}/items/{slot}", s.clearBulletinItem)
	mux.HandleFunc("POST /api/bulletins/{id}/items/{slot}/send", s.sendBulletinItemNow)
}

// listBulletinGroups returns every bulletin group with its items.
//
// @Summary  List bulletin groups
// @Tags     bulletins
// @ID       listBulletinGroups
// @Produce  json
// @Success  200 {array}  dto.BulletinGroupResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /bulletins [get]
func (s *Server) listBulletinGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.store.ListBulletinGroups(r.Context())
	if err != nil {
		s.internalError(w, r, "list bulletin groups", err)
		return
	}
	resp := make([]dto.BulletinGroupResponse, len(groups))
	for i, g := range groups {
		resp[i] = dto.BulletinGroupFromModel(g)
	}
	writeJSON(w, http.StatusOK, resp)
}

// createBulletinGroup creates a new bulletin group and seeds its 10 slots.
//
// @Summary  Create bulletin group
// @Tags     bulletins
// @ID       createBulletinGroup
// @Accept   json
// @Produce  json
// @Param    body body     dto.BulletinGroupRequest true "Group settings"
// @Success  201  {object} dto.BulletinGroupResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /bulletins [post]
func (s *Server) createBulletinGroup(w http.ResponseWriter, r *http.Request) {
	handleCreate[dto.BulletinGroupRequest](s, w, r, "create bulletin group",
		func(ctx context.Context, req dto.BulletinGroupRequest) (configstore.BulletinGroup, error) {
			m := req.ToModel()
			if err := s.store.CreateBulletinGroup(ctx, &m); err != nil {
				if errors.Is(err, configstore.ErrBulletinNameReserved) {
					return configstore.BulletinGroup{}, validationError(err)
				}
				return configstore.BulletinGroup{}, err
			}
			s.signalBulletinReload()
			full, err := s.store.GetBulletinGroup(ctx, m.ID)
			if err != nil {
				return configstore.BulletinGroup{}, err
			}
			return *full, nil
		},
		dto.BulletinGroupFromModel)
}

// getBulletinGroup returns one bulletin group by ID.
//
// @Summary  Get bulletin group
// @Tags     bulletins
// @ID       getBulletinGroup
// @Produce  json
// @Param    id  path     int true "Group id"
// @Success  200 {object} dto.BulletinGroupResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /bulletins/{id} [get]
func (s *Server) getBulletinGroup(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	g, err := s.store.GetBulletinGroup(r.Context(), id)
	if err != nil {
		s.internalError(w, r, "get bulletin group", err)
		return
	}
	writeJSON(w, http.StatusOK, dto.BulletinGroupFromModel(*g))
}

// updateBulletinGroup replaces group-level settings.
//
// @Summary  Update bulletin group
// @Tags     bulletins
// @ID       updateBulletinGroup
// @Accept   json
// @Produce  json
// @Param    id   path     int                      true "Group id"
// @Param    body body     dto.BulletinGroupRequest true "Group settings"
// @Success  200  {object} dto.BulletinGroupResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  403  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /bulletins/{id} [put]
func (s *Server) updateBulletinGroup(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	req, err := decodeJSON[dto.BulletinGroupRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	m := req.ToModel()
	m.ID = id
	if err := s.store.UpdateBulletinGroup(r.Context(), &m); err != nil {
		s.internalError(w, r, "update bulletin group", err)
		return
	}
	s.signalBulletinReload()
	full, err := s.store.GetBulletinGroup(r.Context(), id)
	if err != nil {
		s.internalError(w, r, "reload bulletin group", err)
		return
	}
	writeJSON(w, http.StatusOK, dto.BulletinGroupFromModel(*full))
}

// deleteBulletinGroup removes a group and its items. The Global group
// (id=1) cannot be deleted and returns 403.
//
// @Summary  Delete bulletin group
// @Tags     bulletins
// @ID       deleteBulletinGroup
// @Param    id path int true "Group id"
// @Success  204
// @Failure  403 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /bulletins/{id} [delete]
func (s *Server) deleteBulletinGroup(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	if err := s.store.DeleteBulletinGroup(r.Context(), id); err != nil {
		if errors.Is(err, configstore.ErrBulletinGlobalProtected) {
			writeJSON(w, http.StatusForbidden, webtypes.ErrorResponse{Error: err.Error()})
			return
		}
		s.internalError(w, r, "delete bulletin group", err)
		return
	}
	s.signalBulletinReload()
	w.WriteHeader(http.StatusNoContent)
}

// upsertBulletinItem sets the text and active flag for one slot.
//
// @Summary  Upsert bulletin item
// @Tags     bulletins
// @ID       upsertBulletinItem
// @Accept   json
// @Produce  json
// @Param    id   path int                     true "Group id"
// @Param    slot path int                     true "Slot number (0-9)"
// @Param    body body dto.BulletinItemRequest true "Item"
// @Success  204
// @Failure  400 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /bulletins/{id}/items/{slot} [put]
func (s *Server) upsertBulletinItem(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	slot, err := parseSlot(r.PathValue("slot"))
	if err != nil {
		badRequest(w, "slot must be 0–9")
		return
	}
	req, err := decodeJSON[dto.BulletinItemRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := s.store.UpsertBulletinItem(r.Context(), id, slot, req.Text, req.Active); err != nil {
		s.internalError(w, r, "upsert bulletin item", err)
		return
	}
	s.signalBulletinReload()
	w.WriteHeader(http.StatusNoContent)
}

// clearBulletinItem blanks a slot (text="", active=false, send_count=0).
//
// @Summary  Clear bulletin item
// @Tags     bulletins
// @ID       clearBulletinItem
// @Param    id   path int true "Group id"
// @Param    slot path int true "Slot number (0-9)"
// @Success  204
// @Security CookieAuth
// @Router   /bulletins/{id}/items/{slot} [delete]
func (s *Server) clearBulletinItem(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	slot, err := parseSlot(r.PathValue("slot"))
	if err != nil {
		badRequest(w, "slot must be 0–9")
		return
	}
	if err := s.store.ClearBulletinItem(r.Context(), id, slot); err != nil {
		s.internalError(w, r, "clear bulletin item", err)
		return
	}
	s.signalBulletinReload()
	w.WriteHeader(http.StatusNoContent)
}

// sendBulletinItemNow immediately transmits a single bulletin item.
//
// @Summary  Send bulletin item now
// @Tags     bulletins
// @ID       sendBulletinItemNow
// @Param    id   path int true "Group id"
// @Param    slot path int true "Slot number (0-9)"
// @Success  204
// @Failure  503 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /bulletins/{id}/items/{slot}/send [post]
func (s *Server) sendBulletinItemNow(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	slot, err := parseSlot(r.PathValue("slot"))
	if err != nil {
		badRequest(w, "slot must be 0–9")
		return
	}
	if s.bulletinSendNow == nil {
		serviceUnavailable(w, "bulletin scheduler not ready")
		return
	}
	if err := s.bulletinSendNow(r.Context(), id, slot); err != nil {
		s.internalError(w, r, "send bulletin now", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// signalBulletinReload performs a non-blocking send on bulletinReload so the
// scheduler picks up the new config without blocking the HTTP handler.
func (s *Server) signalBulletinReload() {
	if s.bulletinReload == nil {
		return
	}
	select {
	case s.bulletinReload <- struct{}{}:
	default:
	}
}

// parseSlot parses a slot path value (0–9) from a URL path parameter.
func parseSlot(s string) (int, error) {
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 || v > 9 {
		return 0, errors.New("slot must be 0–9")
	}
	return v, nil
}
