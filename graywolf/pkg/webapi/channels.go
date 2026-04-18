package webapi

import (
	"context"
	"net/http"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
	"github.com/chrissnell/graywolf/pkg/webtypes"
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
	handleList[configstore.Channel](s, w, r, "list channels",
		s.store.ListChannels, dto.ChannelFromModel)
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
			return dto.ChannelFromModel(*c)
		})
}

// updateChannel replaces the channel with the given id using the
// request body and returns the persisted record.
//
// @Summary  Update channel
// @Tags     channels
// @ID       updateChannel
// @Accept   json
// @Produce  json
// @Param    id   path     int                true "Channel id"
// @Param    body body     dto.ChannelRequest true "Channel definition"
// @Success  200  {object} dto.ChannelResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /channels/{id} [put]
func (s *Server) updateChannel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	handleUpdate[dto.ChannelRequest](s, w, r, "update channel", id,
		func(ctx context.Context, id uint32, req dto.ChannelRequest) (configstore.Channel, error) {
			m := req.ToUpdate(id)
			if err := s.store.UpdateChannel(ctx, &m); err != nil {
				return configstore.Channel{}, err
			}
			s.notifyBridgeReload(ctx)
			return m, nil
		},
		dto.ChannelFromModel)
}

// deleteChannel removes the channel with the given id.
//
// @Summary  Delete channel
// @Tags     channels
// @ID       deleteChannel
// @Param    id  path int true "Channel id"
// @Success  204 "No Content"
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /channels/{id} [delete]
func (s *Server) deleteChannel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	handleDelete(s, w, r, "delete channel", id, func(ctx context.Context, id uint32) error {
		if err := s.store.DeleteChannel(ctx, id); err != nil {
			return err
		}
		s.notifyBridgeReload(ctx)
		return nil
	})
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
	if s.bridge == nil {
		writeJSON(w, http.StatusServiceUnavailable, webtypes.ErrorResponse{Error: "bridge not available"})
		return
	}
	stats, ok := s.bridge.GetChannelStats(id)
	if !ok {
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
