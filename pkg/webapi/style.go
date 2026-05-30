package webapi

import (
	"net/http"
	"strings"
)

// registerStyle mounts the style-asset proxy at /api/maps/style/{path...}.
//
// The path captures the relative asset (americana-roboto/style.json,
// roboto-glyphs/Roboto%20Regular/0-255.pbf, tiles.json, etc.) and the
// handler delegates to the mapsstyle.Cache. URL-encoded segments
// (e.g. %20) are decoded by Go's URL parser before reaching the handler;
// the cache treats them as literal bytes on disk and re-encodes on
// upstream fetch.
func (s *Server) registerStyle(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/maps/style/{path...}", s.getStyleAsset)
}

// @Summary  Serve a cached MapLibre style asset
// @Tags     maps
// @ID       getMapsStyleAsset
// @Produce  json
// @Produce  octet-stream
// @Param    path path string true "Relative asset path under the upstream worker (e.g. americana-roboto/style.json, americana/sprites/sprite.png, roboto-glyphs/Roboto%20Regular/0-255.pbf, tiles.json)"
// @Success  200 {string} string "Asset body; content-type matches the asset"
// @Failure  404 {object} webtypes.ErrorResponse
// @Failure  502 {object} webtypes.ErrorResponse
// @Failure  503 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /maps/style/{path} [get]
func (s *Server) getStyleAsset(w http.ResponseWriter, r *http.Request) {
	if s.style == nil {
		serviceUnavailable(w, "maps style cache not initialized")
		return
	}
	rel := r.PathValue("path")
	body, ct, err := s.style.Get(r.Context(), rel)
	if err != nil {
		if isPathRejection(err) {
			http.NotFound(w, r)
			return
		}
		// All other failures are "upstream unreachable and nothing on
		// disk." 502 (Bad Gateway) is the closest semantic match: an
		// upstream we depend on is unavailable.
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(body)
}

func isPathRejection(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "rejected path")
}
