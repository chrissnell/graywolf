package webapi

import (
	"context"
	"net/http"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// registerFixedPoints installs the /api/fixed-points route tree. Fixed
// points are operator-placed map landmarks shared across every device
// pointed at this server (graywolf#347). See beacons.go for the
// reference CRUD shape.
func (s *Server) registerFixedPoints(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/fixed-points", s.listFixedPoints)
	mux.HandleFunc("POST /api/fixed-points", s.createFixedPoint)
	mux.HandleFunc("GET /api/fixed-points/{id}", s.getFixedPoint)
	mux.HandleFunc("PUT /api/fixed-points/{id}", s.updateFixedPoint)
	mux.HandleFunc("DELETE /api/fixed-points/{id}", s.deleteFixedPoint)
}

// listFixedPoints returns every fixed point.
//
// @Summary  List fixed points
// @Tags     fixed-points
// @ID       listFixedPoints
// @Produce  json
// @Success  200 {array}  dto.FixedPointResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /fixed-points [get]
func (s *Server) listFixedPoints(w http.ResponseWriter, r *http.Request) {
	handleList[configstore.FixedPoint](s, w, r, "list fixed points",
		s.store.ListFixedPoints, dto.FixedPointFromModel)
}

// createFixedPoint creates a new fixed point.
//
// @Summary  Create fixed point
// @Tags     fixed-points
// @ID       createFixedPoint
// @Accept   json
// @Produce  json
// @Param    body body     dto.FixedPointRequest true "Fixed point definition"
// @Success  201  {object} dto.FixedPointResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /fixed-points [post]
func (s *Server) createFixedPoint(w http.ResponseWriter, r *http.Request) {
	handleCreate[dto.FixedPointRequest](s, w, r, "create fixed point",
		func(ctx context.Context, req dto.FixedPointRequest) (configstore.FixedPoint, error) {
			m := req.ToModel()
			if err := s.store.CreateFixedPoint(ctx, &m); err != nil {
				return configstore.FixedPoint{}, err
			}
			return m, nil
		},
		dto.FixedPointFromModel)
}

// getFixedPoint returns the fixed point with the given id.
//
// @Summary  Get fixed point
// @Tags     fixed-points
// @ID       getFixedPoint
// @Produce  json
// @Param    id  path     int true "Fixed point id"
// @Success  200 {object} dto.FixedPointResponse
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /fixed-points/{id} [get]
func (s *Server) getFixedPoint(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	handleGet[*configstore.FixedPoint](s, w, r, "get fixed point", id,
		s.store.GetFixedPoint,
		func(fp *configstore.FixedPoint) dto.FixedPointResponse {
			return dto.FixedPointFromModel(*fp)
		})
}

// updateFixedPoint replaces the fixed point with the given id.
//
// @Summary  Update fixed point
// @Tags     fixed-points
// @ID       updateFixedPoint
// @Accept   json
// @Produce  json
// @Param    id   path     int                   true "Fixed point id"
// @Param    body body     dto.FixedPointRequest true "Fixed point definition"
// @Success  200  {object} dto.FixedPointResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /fixed-points/{id} [put]
func (s *Server) updateFixedPoint(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	handleUpdate[dto.FixedPointRequest](s, w, r, "update fixed point", id,
		func(ctx context.Context, id uint32, req dto.FixedPointRequest) (configstore.FixedPoint, error) {
			m := req.ToUpdate(id)
			if err := s.store.UpdateFixedPoint(ctx, &m); err != nil {
				return configstore.FixedPoint{}, err
			}
			return m, nil
		},
		dto.FixedPointFromModel)
}

// deleteFixedPoint removes the fixed point with the given id.
//
// @Summary  Delete fixed point
// @Tags     fixed-points
// @ID       deleteFixedPoint
// @Param    id  path int true "Fixed point id"
// @Success  204 "No Content"
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /fixed-points/{id} [delete]
func (s *Server) deleteFixedPoint(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	handleDelete(s, w, r, "delete fixed point", id, s.store.DeleteFixedPoint)
}
