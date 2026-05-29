package webapi

import (
	"encoding/json"
	"net/http"

	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerLocalBounds(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/maps/local-bounds", s.getLocalBounds)
}

// @Summary  Get the bbox of every completed offline map
// @Tags     maps
// @ID       getMapsLocalBounds
// @Produce  json
// @Success  200 {object} dto.LocalBounds
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /maps/local-bounds [get]
//
// getLocalBounds is the render-path bounds source. It deliberately
// does NOT consult the remote catalog: a completed download's bbox is
// snapshotted into maps_downloads at download time and (for legacy
// rows) backfilled from the on-disk pmtiles header at startup. This
// is what lets the offline map render on a host that has never
// reached maps.nw5w.com in this boot session.
func (s *Server) getLocalBounds(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListMapsDownloads(r.Context())
	if err != nil {
		s.internalError(w, r, "list downloads", err)
		return
	}
	out := dto.LocalBounds{}
	for _, row := range rows {
		if row.Status != "complete" || row.BBox == nil {
			continue
		}
		var bbox [4]float64
		if err := json.Unmarshal([]byte(*row.BBox), &bbox); err != nil {
			// Malformed bbox shouldn't kill the response; skip the
			// row and log. The render path falls back to the network
			// for this slug until a re-download or restart heals it.
			s.logger.Warn("local-bounds: skipping malformed bbox",
				"slug", row.Slug, "raw", *row.BBox, "err", err)
			continue
		}
		out[row.Slug] = bbox
	}
	writeJSON(w, http.StatusOK, out)
}
