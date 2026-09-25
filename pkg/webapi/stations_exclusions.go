package webapi

import (
	"fmt"
	"net/http"

	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// registerStationExclusions installs the /api/stations/exclusions CRUD
// routes. Exclusions are the callsigns web/src/lib/stationNewTransport.js
// never notifies for -- checked before the favorites path, so an
// exclusion wins even over a favorite -- for a station the
// digipeater/repeater/weather-station heuristics don't catch but the
// operator recognizes as infrastructure. See docs/wiki/notifications.md.
// No PUT: the operator edits an exclusion by deleting and re-adding it.
func (s *Server) registerStationExclusions(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/stations/exclusions", s.listExcludedStations)
	mux.HandleFunc("POST /api/stations/exclusions", s.createExcludedStation)
	mux.HandleFunc("DELETE /api/stations/exclusions/{id}", s.deleteExcludedStation)
}

// listExcludedStations returns every excluded station.
//
// @Summary  List excluded stations
// @Tags     stations
// @ID       listExcludedStations
// @Produce  json
// @Success  200 {array} dto.ExcludedStationResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /stations/exclusions [get]
func (s *Server) listExcludedStations(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListExcludedStations(r.Context())
	if err != nil {
		s.internalError(w, r, "list excluded stations", err)
		return
	}
	out := make([]dto.ExcludedStationResponse, len(rows))
	for i, row := range rows {
		out[i] = dto.ExcludedStationFromModel(row)
	}
	writeJSON(w, http.StatusOK, out)
}

// createExcludedStation adds a call sign to the exclusion list.
//
// @Summary  Add an excluded station
// @Tags     stations
// @ID       createExcludedStation
// @Accept   json
// @Produce  json
// @Param    body body     dto.ExcludedStationRequest true "Excluded station"
// @Success  201  {object} dto.ExcludedStationResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  409  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /stations/exclusions [post]
func (s *Server) createExcludedStation(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[dto.ExcludedStationRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	model := req.ToModel()
	if err := s.store.CreateExcludedStation(r.Context(), &model); err != nil {
		if isUniqueConstraintErr(err) {
			conflict(w, fmt.Sprintf("call sign %q is already excluded", model.Callsign))
			return
		}
		s.internalError(w, r, "create excluded station", err)
		return
	}
	writeJSON(w, http.StatusCreated, dto.ExcludedStationFromModel(model))
}

// deleteExcludedStation removes an exclusion.
//
// @Summary  Remove an excluded station
// @Tags     stations
// @ID       deleteExcludedStation
// @Param    id  path int true "Excluded station id"
// @Success  204 "No Content"
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /stations/exclusions/{id} [delete]
func (s *Server) deleteExcludedStation(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	existing, err := s.store.GetExcludedStation(r.Context(), id)
	if err != nil {
		s.internalError(w, r, "get excluded station", err)
		return
	}
	if existing == nil {
		notFound(w)
		return
	}
	if err := s.store.DeleteExcludedStation(r.Context(), id); err != nil {
		s.internalError(w, r, "delete excluded station", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
