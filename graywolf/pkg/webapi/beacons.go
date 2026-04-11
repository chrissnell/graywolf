package webapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerBeacons(mux *http.ServeMux) {
	mux.HandleFunc("/api/beacons", s.handleBeaconsCollection)
	mux.HandleFunc("/api/beacons/", s.handleBeaconsItem)
}

// GET/POST /api/beacons
func (s *Server) handleBeaconsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleList[configstore.Beacon](s, w, r, "list beacons",
			s.store.ListBeacons, dto.BeaconFromModel)
	case http.MethodPost:
		handleCreate[dto.BeaconRequest](s, w, r, "create beacon",
			func(ctx context.Context, req dto.BeaconRequest) (configstore.Beacon, error) {
				m := req.ToModel()
				if err := s.store.CreateBeacon(ctx, &m); err != nil {
					return configstore.Beacon{}, err
				}
				s.signalBeaconReload()
				return m, nil
			},
			dto.BeaconFromModel)
	default:
		methodNotAllowed(w)
	}
}

// /api/beacons/{id}        — GET, PUT, DELETE
// /api/beacons/{id}/send   — POST one-shot transmission
func (s *Server) handleBeaconsItem(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/beacons/")
	parts := strings.SplitN(path, "/", 2)

	id, err := parseID(parts[0])
	if err != nil {
		badRequest(w, "invalid id")
		return
	}

	if len(parts) == 2 && parts[1] == "send" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		s.handleBeaconSendNow(w, r, id)
		return
	}

	switch r.Method {
	case http.MethodGet:
		handleGet[*configstore.Beacon](s, w, r, id,
			s.store.GetBeacon,
			func(b *configstore.Beacon) dto.BeaconResponse {
				return dto.BeaconFromModel(*b)
			})
	case http.MethodPut:
		handleUpdate[dto.BeaconRequest](s, w, r, "update beacon", id,
			func(ctx context.Context, id uint32, req dto.BeaconRequest) (configstore.Beacon, error) {
				m := req.ToUpdate(id)
				if err := s.store.UpdateBeacon(ctx, &m); err != nil {
					return configstore.Beacon{}, err
				}
				s.signalBeaconReload()
				return m, nil
			},
			dto.BeaconFromModel)
	case http.MethodDelete:
		handleDelete(s, w, r, "delete beacon", id, func(ctx context.Context, id uint32) error {
			if err := s.store.DeleteBeacon(ctx, id); err != nil {
				return err
			}
			s.signalBeaconReload()
			return nil
		})
	default:
		methodNotAllowed(w)
	}
}

// POST /api/beacons/{id}/send — one-shot transmission trigger.
func (s *Server) handleBeaconSendNow(w http.ResponseWriter, r *http.Request, id uint32) {
	if s.beaconSendNow == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "beacon scheduler not available"})
		return
	}
	if _, err := s.store.GetBeacon(r.Context(), id); err != nil {
		notFound(w)
		return
	}
	if err := s.beaconSendNow(r.Context(), id); err != nil {
		s.internalError(w, r, "beacon send now", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}

// signalBeaconReload performs a non-blocking send on the beacon reload
// channel; coalesces if a previous signal is still buffered.
func (s *Server) signalBeaconReload() {
	if s.beaconReload == nil {
		return
	}
	select {
	case s.beaconReload <- struct{}{}:
	default:
	}
}
