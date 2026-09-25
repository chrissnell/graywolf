package webapi

import (
	"fmt"
	"net/http"

	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// registerStationFavorites installs the /api/stations/favorites CRUD
// routes. Favorites are the callsigns web/src/lib/stationNewTransport.js
// notifies for regardless of the Live Map's RF Only / Direct RX Only
// filter or digipeater status -- see docs/wiki/notifications.md. No PUT:
// the operator edits a favorite by deleting and re-adding it.
func (s *Server) registerStationFavorites(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/stations/favorites", s.listFavoriteStations)
	mux.HandleFunc("POST /api/stations/favorites", s.createFavoriteStation)
	mux.HandleFunc("DELETE /api/stations/favorites/{id}", s.deleteFavoriteStation)
}

// listFavoriteStations returns every favorite station.
//
// @Summary  List favorite stations
// @Tags     stations
// @ID       listFavoriteStations
// @Produce  json
// @Success  200 {array} dto.FavoriteStationResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /stations/favorites [get]
func (s *Server) listFavoriteStations(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListFavoriteStations(r.Context())
	if err != nil {
		s.internalError(w, r, "list favorite stations", err)
		return
	}
	out := make([]dto.FavoriteStationResponse, len(rows))
	for i, row := range rows {
		out[i] = dto.FavoriteStationFromModel(row)
	}
	writeJSON(w, http.StatusOK, out)
}

// createFavoriteStation adds a call sign to the favorites list.
//
// @Summary  Add a favorite station
// @Tags     stations
// @ID       createFavoriteStation
// @Accept   json
// @Produce  json
// @Param    body body     dto.FavoriteStationRequest true "Favorite station"
// @Success  201  {object} dto.FavoriteStationResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  409  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /stations/favorites [post]
func (s *Server) createFavoriteStation(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[dto.FavoriteStationRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	model := req.ToModel()
	if err := s.store.CreateFavoriteStation(r.Context(), &model); err != nil {
		if isUniqueConstraintErr(err) {
			conflict(w, fmt.Sprintf("call sign %q is already a favorite", model.Callsign))
			return
		}
		s.internalError(w, r, "create favorite station", err)
		return
	}
	writeJSON(w, http.StatusCreated, dto.FavoriteStationFromModel(model))
}

// deleteFavoriteStation removes a favorite.
//
// @Summary  Remove a favorite station
// @Tags     stations
// @ID       deleteFavoriteStation
// @Param    id  path int true "Favorite station id"
// @Success  204 "No Content"
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /stations/favorites/{id} [delete]
func (s *Server) deleteFavoriteStation(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	existing, err := s.store.GetFavoriteStation(r.Context(), id)
	if err != nil {
		s.internalError(w, r, "get favorite station", err)
		return
	}
	if existing == nil {
		notFound(w)
		return
	}
	if err := s.store.DeleteFavoriteStation(r.Context(), id); err != nil {
		s.internalError(w, r, "delete favorite station", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
