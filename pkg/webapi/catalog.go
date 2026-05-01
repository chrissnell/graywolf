package webapi

import (
	"net/http"

	"github.com/chrissnell/graywolf/pkg/mapscatalog"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerCatalog(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/maps/catalog", s.getCatalog)
}

// @Summary  Get the offline-maps download catalog
// @Tags     maps
// @ID       getMapsCatalog
// @Produce  json
// @Success  200 {object} dto.Catalog
// @Failure  503 {object} webtypes.ErrorResponse
// @Failure  502 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /maps/catalog [get]
func (s *Server) getCatalog(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		serviceUnavailable(w, "maps catalog not initialized")
		return
	}
	cat, err := s.catalog.Get(r.Context())
	if err != nil {
		s.internalError(w, r, "fetch catalog", err)
		return
	}
	writeJSON(w, http.StatusOK, toCatalogDTO(cat))
}

func toCatalogDTO(c mapscatalog.Catalog) dto.Catalog {
	out := dto.Catalog{
		SchemaVersion: c.SchemaVersion,
		GeneratedAt:   c.GeneratedAt,
		Countries:     make([]dto.CatalogCountry, len(c.Countries)),
		Provinces:     make([]dto.CatalogProvince, len(c.Provinces)),
		States:        make([]dto.CatalogState, len(c.States)),
	}
	for i, x := range c.Countries {
		out.Countries[i] = dto.CatalogCountry{ISO2: x.ISO2, Name: x.Name, SizeBytes: x.SizeBytes, SHA256: x.SHA256, BBox: x.BBox}
	}
	for i, x := range c.Provinces {
		out.Provinces[i] = dto.CatalogProvince{ISO2: x.ISO2, Slug: x.Slug, Name: x.Name, Code: x.Code, SizeBytes: x.SizeBytes, SHA256: x.SHA256, BBox: x.BBox}
	}
	for i, x := range c.States {
		out.States[i] = dto.CatalogState{Slug: x.Slug, Name: x.Name, Code: x.Code, SizeBytes: x.SizeBytes, SHA256: x.SHA256, BBox: x.BBox}
	}
	return out
}
