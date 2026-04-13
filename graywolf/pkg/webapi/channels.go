package webapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// registerChannels installs the /api/channels route tree on mux. Owned
// by this file so the RegisterRoutes entry point stays a short dispatch
// list and the handler bodies live next to the resource they touch.
func (s *Server) registerChannels(mux *http.ServeMux) {
	mux.HandleFunc("/api/channels", s.handleChannelsCollection)
	mux.HandleFunc("/api/channels/", s.handleChannelsItem)
}

// GET/POST /api/channels
func (s *Server) handleChannelsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleList[configstore.Channel](s, w, r, "list channels",
			s.store.ListChannels, dto.ChannelFromModel)
	case http.MethodPost:
		handleCreate[dto.ChannelRequest](s, w, r, "create channel",
			func(ctx context.Context, req dto.ChannelRequest) (configstore.Channel, error) {
				m := req.ToModel()
				return m, s.store.CreateChannel(ctx, &m)
			},
			dto.ChannelFromModel)
	default:
		methodNotAllowed(w)
	}
}

// GET /api/channels/{id}
// PUT /api/channels/{id}
// DELETE /api/channels/{id}
// GET /api/channels/{id}/stats
func (s *Server) handleChannelsItem(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/channels/")
	parts := strings.SplitN(path, "/", 2)

	// /api/channels/{id}/stats
	if len(parts) == 2 && parts[1] == "stats" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		s.handleChannelStats(w, r, parts[0])
		return
	}

	id, err := parseID(parts[0])
	if err != nil {
		badRequest(w, "invalid id")
		return
	}

	switch r.Method {
	case http.MethodGet:
		handleGet[*configstore.Channel](s, w, r, id,
			s.store.GetChannel,
			func(c *configstore.Channel) dto.ChannelResponse {
				return dto.ChannelFromModel(*c)
			})
	case http.MethodPut:
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
	case http.MethodDelete:
		// Look up device IDs before deleting so we can notify the bridge.
		handleDelete(s, w, r, "delete channel", id, func(ctx context.Context, id uint32) error {
			if err := s.store.DeleteChannel(ctx, id); err != nil {
				return err
			}
			s.notifyBridgeReload(ctx)
			return nil
		})
	default:
		methodNotAllowed(w)
	}
}

// handleChannelStats is not CRUD; it talks to the live modem bridge
// rather than the store, so it stays hand-rolled.
func (s *Server) handleChannelStats(w http.ResponseWriter, _ *http.Request, idStr string) {
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		badRequest(w, "invalid channel id")
		return
	}
	if s.bridge == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "bridge not available"})
		return
	}
	stats, ok := s.bridge.GetChannelStats(uint32(id))
	if !ok {
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
