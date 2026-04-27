package webapi

import (
	"errors"
	"net/http"
	"os"
	"regexp"

	"github.com/chrissnell/graywolf/pkg/mapscache"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// validDownloadSlugs mirrors graywolf/web/src/lib/maps/state-list.js —
// 50 US states + DC, lowercase + hyphenated to match the maps.nw5w.com
// R2 layout. The closed list is the authoritative validator; the
// regex below is a cheap pre-check that catches obviously-bogus inputs
// (unicode, oversized strings) before we hit log lines.
var validDownloadSlugs = map[string]bool{
	"alabama":              true,
	"alaska":               true,
	"arizona":              true,
	"arkansas":             true,
	"california":           true,
	"colorado":             true,
	"connecticut":          true,
	"delaware":             true,
	"district-of-columbia": true,
	"florida":              true,
	"georgia":              true,
	"hawaii":               true,
	"idaho":                true,
	"illinois":             true,
	"indiana":              true,
	"iowa":                 true,
	"kansas":               true,
	"kentucky":             true,
	"louisiana":            true,
	"maine":                true,
	"maryland":             true,
	"massachusetts":        true,
	"michigan":             true,
	"minnesota":            true,
	"mississippi":          true,
	"missouri":             true,
	"montana":              true,
	"nebraska":             true,
	"nevada":               true,
	"new-hampshire":        true,
	"new-jersey":           true,
	"new-mexico":           true,
	"new-york":             true,
	"north-carolina":       true,
	"north-dakota":         true,
	"ohio":                 true,
	"oklahoma":             true,
	"oregon":               true,
	"pennsylvania":         true,
	"rhode-island":         true,
	"south-carolina":       true,
	"south-dakota":         true,
	"tennessee":            true,
	"texas":                true,
	"utah":                 true,
	"vermont":              true,
	"virginia":             true,
	"washington":           true,
	"west-virginia":        true,
	"wisconsin":            true,
	"wyoming":              true,
}

func isValidSlug(slug string) bool {
	return validDownloadSlugs[slug]
}

// slugPattern is enforced before isValidSlug as a cheap pre-check.
// Belt-and-suspenders: the closed list catches everything, but
// keeping the pattern protects against weird unicode getting into
// log lines.
var slugPattern = regexp.MustCompile(`^[a-z][a-z\-]{1,40}$`)

func (s *Server) registerDownloads(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/maps/downloads", s.listDownloads)
	mux.HandleFunc("GET /api/maps/downloads/{slug}", s.getDownloadStatus)
	mux.HandleFunc("POST /api/maps/downloads/{slug}", s.startDownload)
	mux.HandleFunc("DELETE /api/maps/downloads/{slug}", s.deleteDownload)
}

// ServeTilesPMTiles is mounted on the OUTER mux (under RequireAuth) in
// pkg/app/wiring.go because it lives at /tiles/..., outside /api/...
// http.ServeFile honors the Range header natively, which PMTiles
// relies on for efficient sub-range fetches of the index + tile blobs.
func (s *Server) ServeTilesPMTiles(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !slugPattern.MatchString(slug) || !isValidSlug(slug) {
		http.NotFound(w, r)
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
// @Param    slug path string true "state slug"
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
	slug := r.PathValue("slug")
	if !slugPattern.MatchString(slug) || !isValidSlug(slug) {
		badRequest(w, "unknown state slug")
		return
	}
	st, err := s.mapsCache.Status(r.Context(), slug)
	if err != nil {
		s.internalError(w, r, "get download status", err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(st))
}

// @Summary  Start an offline download for a state
// @Tags     maps
// @ID       startMapsDownload
// @Produce  json
// @Param    slug path string true "state slug"
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
	slug := r.PathValue("slug")
	if !slugPattern.MatchString(slug) || !isValidSlug(slug) {
		badRequest(w, "unknown state slug")
		return
	}
	if err := s.mapsCache.Start(r.Context(), slug); err != nil {
		if errors.Is(err, mapscache.ErrAlreadyInflight) {
			writeJSON(w, http.StatusConflict, map[string]string{
				"error":   "already_inflight",
				"message": "a download for this state is already in progress",
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

// @Summary  Delete an offline download for a state
// @Tags     maps
// @ID       deleteMapsDownload
// @Param    slug path string true "state slug"
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
	slug := r.PathValue("slug")
	if !slugPattern.MatchString(slug) || !isValidSlug(slug) {
		badRequest(w, "unknown state slug")
		return
	}
	if err := s.mapsCache.Delete(r.Context(), slug); err != nil {
		s.internalError(w, r, "delete download", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
