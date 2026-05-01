package webapi

import (
	"errors"
	"net/http"
	"os"

	"github.com/chrissnell/graywolf/pkg/mapscache"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerDownloads(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/maps/downloads", s.listDownloads)
	mux.HandleFunc("GET /api/maps/downloads/{slug...}", s.getDownloadStatus)
	mux.HandleFunc("POST /api/maps/downloads/{slug...}", s.startDownload)
	mux.HandleFunc("DELETE /api/maps/downloads/{slug...}", s.deleteDownload)
}

// resolveSlug parses the URL path's slug, fetches the live catalog,
// and confirms the slug names a published archive. Returns
// (slug, ok). On rejection writes the appropriate HTTP error and
// returns ok=false.
func (s *Server) resolveSlug(w http.ResponseWriter, r *http.Request) (string, bool) {
	slug := r.PathValue("slug")
	if _, _, _, ok := parseSlug(slug); !ok {
		badRequest(w, "invalid slug")
		return "", false
	}
	if s.catalog == nil {
		serviceUnavailable(w, "maps catalog not initialized")
		return "", false
	}
	cat, err := s.catalog.Get(r.Context())
	if err != nil {
		s.internalError(w, r, "catalog lookup", err)
		return "", false
	}
	if !slugInCatalog(cat, slug) {
		badRequest(w, "unknown slug")
		return "", false
	}
	return slug, true
}

// ServeTilesPMTiles is mounted on the OUTER mux (under RequireAuth) in
// pkg/app/wiring.go because it lives at /tiles/..., outside /api/...
// http.ServeFile honors the Range header natively, which PMTiles
// relies on for efficient sub-range fetches of the index + tile blobs.
func (s *Server) ServeTilesPMTiles(w http.ResponseWriter, r *http.Request) {
	slug, ok := s.resolveSlug(w, r)
	if !ok {
		return
	}
	if s.mapsCache == nil {
		http.Error(w, "tile cache not initialized", http.StatusServiceUnavailable)
		return
	}
	path := s.mapsCache.PathFor(slug)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.pmtiles")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(w, r, path)
}

func toDTO(st mapscache.Status) dto.DownloadStatus {
	return dto.DownloadStatus{
		Slug:            st.Slug,
		State:           st.State,
		BytesTotal:      st.BytesTotal,
		BytesDownloaded: st.BytesDownloaded,
		DownloadedAt:    st.DownloadedAt,
		ErrorMessage:    st.ErrorMessage,
	}
}

// @Summary  List offline map downloads
// @Tags     maps
// @ID       listMapsDownloads
// @Produce  json
// @Success  200 {array}  dto.DownloadStatus
// @Failure  500 {object} webtypes.ErrorResponse
// @Failure  503 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /maps/downloads [get]
func (s *Server) listDownloads(w http.ResponseWriter, r *http.Request) {
	if s.mapsCache == nil {
		serviceUnavailable(w, "maps cache not initialized")
		return
	}
	rows, err := s.mapsCache.List(r.Context())
	if err != nil {
		s.internalError(w, r, "list downloads", err)
		return
	}
	out := make([]dto.DownloadStatus, len(rows))
	for i, st := range rows {
		out[i] = toDTO(st)
	}
	writeJSON(w, http.StatusOK, out)
}

// @Summary  Get one download's status
// @Tags     maps
// @ID       getMapsDownloadStatus
// @Produce  json
// @Param    slug path string true "namespaced slug (state/<slug>, country/<iso2>, province/<iso2>/<slug>)"
// @Success  200 {object} dto.DownloadStatus
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Failure  503 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /maps/downloads/{slug} [get]
func (s *Server) getDownloadStatus(w http.ResponseWriter, r *http.Request) {
	if s.mapsCache == nil {
		serviceUnavailable(w, "maps cache not initialized")
		return
	}
	slug, ok := s.resolveSlug(w, r)
	if !ok {
		return
	}
	st, err := s.mapsCache.Status(r.Context(), slug)
	if err != nil {
		s.internalError(w, r, "get download status", err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(st))
}

// @Summary  Start an offline download
// @Tags     maps
// @ID       startMapsDownload
// @Produce  json
// @Param    slug path string true "namespaced slug (state/<slug>, country/<iso2>, province/<iso2>/<slug>)"
// @Success  202 {object} dto.DownloadStatus
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  409 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Failure  503 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /maps/downloads/{slug} [post]
func (s *Server) startDownload(w http.ResponseWriter, r *http.Request) {
	if s.mapsCache == nil {
		serviceUnavailable(w, "maps cache not initialized")
		return
	}
	slug, ok := s.resolveSlug(w, r)
	if !ok {
		return
	}
	if err := s.mapsCache.Start(r.Context(), slug); err != nil {
		if errors.Is(err, mapscache.ErrAlreadyInflight) {
			writeJSON(w, http.StatusConflict, map[string]string{
				"error":   "already_inflight",
				"message": "a download for this slug is already in progress",
			})
			return
		}
		s.internalError(w, r, "start download", err)
		return
	}
	st, err := s.mapsCache.Status(r.Context(), slug)
	if err != nil {
		s.internalError(w, r, "start download status", err)
		return
	}
	writeJSON(w, http.StatusAccepted, toDTO(st))
}

// @Summary  Delete an offline download
// @Tags     maps
// @ID       deleteMapsDownload
// @Param    slug path string true "namespaced slug (state/<slug>, country/<iso2>, province/<iso2>/<slug>)"
// @Success  204
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Failure  503 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /maps/downloads/{slug} [delete]
func (s *Server) deleteDownload(w http.ResponseWriter, r *http.Request) {
	if s.mapsCache == nil {
		serviceUnavailable(w, "maps cache not initialized")
		return
	}
	slug, ok := s.resolveSlug(w, r)
	if !ok {
		return
	}
	if err := s.mapsCache.Delete(r.Context(), slug); err != nil {
		s.internalError(w, r, "delete download", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
