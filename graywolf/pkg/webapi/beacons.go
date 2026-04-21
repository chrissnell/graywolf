package webapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
	"github.com/chrissnell/graywolf/pkg/webtypes"
)

// registerBeacons installs the /api/beacons route tree on mux using
// Go 1.22+ method-scoped patterns. See channels.go for the reference.
func (s *Server) registerBeacons(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/beacons", s.listBeacons)
	mux.HandleFunc("POST /api/beacons", s.createBeacon)
	mux.HandleFunc("GET /api/beacons/{id}", s.getBeacon)
	mux.HandleFunc("PUT /api/beacons/{id}", s.updateBeacon)
	mux.HandleFunc("DELETE /api/beacons/{id}", s.deleteBeacon)
	mux.HandleFunc("POST /api/beacons/{id}/send", s.sendBeacon)
}

// listBeacons returns every configured beacon.
//
// @Summary  List beacons
// @Tags     beacons
// @ID       listBeacons
// @Produce  json
// @Success  200 {array}  dto.BeaconResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /beacons [get]
func (s *Server) listBeacons(w http.ResponseWriter, r *http.Request) {
	handleList[configstore.Beacon](s, w, r, "list beacons",
		s.store.ListBeacons, dto.BeaconFromModel)
}

// createBeacon creates a new beacon from the request body and returns
// the persisted record (with its assigned id) on success.
//
// @Summary  Create beacon
// @Tags     beacons
// @ID       createBeacon
// @Accept   json
// @Produce  json
// @Param    body body     dto.BeaconRequest true "Beacon definition"
// @Success  201  {object} dto.BeaconResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /beacons [post]
func (s *Server) createBeacon(w http.ResponseWriter, r *http.Request) {
	handleCreate[dto.BeaconRequest](s, w, r, "create beacon",
		func(ctx context.Context, req dto.BeaconRequest) (configstore.Beacon, error) {
			if err := dto.ValidateChannelRef(ctx, s.store, "channel", req.Channel); err != nil {
				return configstore.Beacon{}, validationError(err)
			}
			if err := s.requireTxCapableChannel(ctx, "channel", req.Channel); err != nil {
				return configstore.Beacon{}, validationError(err)
			}
			m := req.ToModel()
			if err := s.store.CreateBeacon(ctx, &m); err != nil {
				return configstore.Beacon{}, err
			}
			s.signalBeaconReload()
			return m, nil
		},
		dto.BeaconFromModel)
}

// getBeacon returns the beacon with the given id.
//
// @Summary  Get beacon
// @Tags     beacons
// @ID       getBeacon
// @Produce  json
// @Param    id  path     int true "Beacon id"
// @Success  200 {object} dto.BeaconResponse
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /beacons/{id} [get]
func (s *Server) getBeacon(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	handleGet[*configstore.Beacon](s, w, r, "get beacon", id,
		s.store.GetBeacon,
		func(b *configstore.Beacon) dto.BeaconResponse {
			return dto.BeaconFromModel(*b)
		})
}

// updateBeacon replaces the beacon with the given id using the request
// body and returns the persisted record.
//
// @Summary  Update beacon
// @Tags     beacons
// @ID       updateBeacon
// @Accept   json
// @Produce  json
// @Param    id   path     int               true "Beacon id"
// @Param    body body     dto.BeaconRequest true "Beacon definition"
// @Success  200  {object} dto.BeaconResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /beacons/{id} [put]
func (s *Server) updateBeacon(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	handleUpdate[dto.BeaconRequest](s, w, r, "update beacon", id,
		func(ctx context.Context, id uint32, req dto.BeaconRequest) (configstore.Beacon, error) {
			if err := dto.ValidateChannelRef(ctx, s.store, "channel", req.Channel); err != nil {
				return configstore.Beacon{}, validationError(err)
			}
			if err := s.requireTxCapableChannel(ctx, "channel", req.Channel); err != nil {
				return configstore.Beacon{}, validationError(err)
			}
			// Merge the request onto the existing row so a nil
			// Callsign (field omitted) preserves the stored
			// override value. Missing row → fall back to bare
			// ToUpdate which populates the struct from the
			// request, treating nil as "" (inherit).
			existing, err := s.store.GetBeacon(ctx, id)
			if err != nil {
				return configstore.Beacon{}, err
			}
			var m configstore.Beacon
			if existing != nil {
				m = req.ApplyToUpdate(id, *existing)
			} else {
				m = req.ToUpdate(id)
			}
			if err := s.store.UpdateBeacon(ctx, &m); err != nil {
				return configstore.Beacon{}, err
			}
			s.signalBeaconReload()
			return m, nil
		},
		dto.BeaconFromModel)
}

// deleteBeacon removes the beacon with the given id.
//
// @Summary  Delete beacon
// @Tags     beacons
// @ID       deleteBeacon
// @Param    id  path int true "Beacon id"
// @Success  204 "No Content"
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /beacons/{id} [delete]
func (s *Server) deleteBeacon(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	handleDelete(s, w, r, "delete beacon", id, func(ctx context.Context, id uint32) error {
		if err := s.store.DeleteBeacon(ctx, id); err != nil {
			return err
		}
		s.signalBeaconReload()
		return nil
	})
}

// sendBeacon triggers a one-shot transmission of the beacon with the
// given id. Not CRUD — talks to the beacon scheduler rather than the
// configstore, so it stays a bespoke handler.
//
// @Summary  Send beacon now
// @Tags     beacons
// @ID       sendBeacon
// @Produce  json
// @Param    id  path     int true "Beacon id"
// @Success  200 {object} dto.BeaconSendResponse
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Failure  503 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /beacons/{id}/send [post]
func (s *Server) sendBeacon(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	if s.beaconSendNow == nil {
		writeJSON(w, http.StatusServiceUnavailable, webtypes.ErrorResponse{Error: "beacon scheduler not available"})
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
	writeJSON(w, http.StatusOK, dto.BeaconSendResponse{Status: "sent"})
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

// requireTxCapableChannel rejects referrer writes (beacons / iGate
// TX channel / digipeater from+to channels) whose channel exists but
// is not currently TX-capable. Runs AFTER dto.ValidateChannelRef so
// the "channel N does not exist" error still wins for typos; this
// handler-level check surfaces the different failure mode ("channel
// exists but cannot TX") with a distinct, actionable message.
//
// A channelID of 0 is treated as "none" — same convention as
// ValidateChannelRef. Soft-FK columns that use 0 as the unset
// sentinel (IGateConfig.TxChannel etc.) pass through unchanged.
//
// The returned error is intended to be wrapped in validationError()
// by the caller so handleCreate / handleUpdate surface it as a 400.
func (s *Server) requireTxCapableChannel(ctx context.Context, fieldName string, channelID uint32) error {
	if channelID == 0 {
		return nil
	}
	tx, found, err := s.resolveChannelTxCapability(ctx, channelID)
	if err != nil {
		return fmt.Errorf("%s: look up channel %d: %w", fieldName, channelID, err)
	}
	if !found {
		// Caller's ValidateChannelRef should have caught this; a race
		// between the two lookups is the only way to land here. Emit
		// the same error text ValidateChannelRef would so the UI's
		// handling is consistent.
		return fmt.Errorf("%s: channel %d does not exist", fieldName, channelID)
	}
	if !tx.Capable {
		return fmt.Errorf("%s: channel %d is not TX-capable: %s", fieldName, channelID, tx.Reason)
	}
	return nil
}
