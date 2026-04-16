package webapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/pttdevice"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerPtt(mux *http.ServeMux) {
	mux.HandleFunc("/api/ptt", s.handlePttCollection)
	mux.HandleFunc("/api/ptt/", s.handlePttByChannel)
}

// GET/POST /api/ptt
func (s *Server) handlePttCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleList[configstore.PttConfig](s, w, r, "list ptt configs",
			s.store.ListPttConfigs, dto.PttFromModel)
	case http.MethodPost:
		handleCreate[dto.PttRequest](s, w, r, "upsert ptt config",
			func(ctx context.Context, req dto.PttRequest) (configstore.PttConfig, error) {
				m := req.ToModel()
				if err := s.store.UpsertPttConfig(ctx, &m); err != nil {
					return configstore.PttConfig{}, err
				}
				s.notifyBridgeForChannel(ctx, m.ChannelID)
				return m, nil
			},
			dto.PttFromModel)
	default:
		methodNotAllowed(w)
	}
}

// /api/ptt/{channel} — GET, PUT, DELETE
// /api/ptt/available — GET device enumeration
// /api/ptt/test-rigctld — POST probe a rigctld endpoint (see ptt_test_rigctld.go)
func (s *Server) handlePttByChannel(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/ptt/")
	if rest == "available" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, http.StatusOK, pttdevice.Enumerate())
		return
	}
	if rest == "test-rigctld" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		s.handleTestRigctld(w, r)
		return
	}
	id, err := parseID(rest)
	if err != nil {
		badRequest(w, "invalid channel id")
		return
	}
	switch r.Method {
	case http.MethodGet:
		handleGet[*configstore.PttConfig](s, w, r, id,
			s.store.GetPttConfigForChannel,
			func(p *configstore.PttConfig) dto.PttResponse {
				return dto.PttFromModel(*p)
			})
	case http.MethodPut:
		handleUpdate[dto.PttRequest](s, w, r, "upsert ptt config", id,
			func(ctx context.Context, channelID uint32, req dto.PttRequest) (configstore.PttConfig, error) {
				m := req.ToUpdate(channelID)
				if err := s.store.UpsertPttConfig(ctx, &m); err != nil {
					return configstore.PttConfig{}, err
				}
				s.notifyBridgeForChannel(ctx, channelID)
				return m, nil
			},
			dto.PttFromModel)
	case http.MethodDelete:
		handleDelete(s, w, r, "delete ptt config", id, func(ctx context.Context, channelID uint32) error {
			if err := s.store.DeletePttConfig(ctx, channelID); err != nil {
				return err
			}
			s.notifyBridgeForChannel(ctx, channelID)
			return nil
		})
	default:
		methodNotAllowed(w)
	}
}
