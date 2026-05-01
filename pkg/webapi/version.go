package webapi

import (
	"net/http"
	"runtime"
)

// VersionResponse is the JSON shape returned by GET /api/version.
type VersionResponse struct {
	Version string `json:"version"`
	// Platform is runtime.GOOS of the server process — "windows", "linux",
	// "darwin", etc. The UI uses it to surface platform-specific guidance
	// (e.g. the Windows app-volume warning on the Audio Devices page).
	Platform string `json:"platform"`
}

// RegisterVersion installs GET /api/version on mux. It is a public
// endpoint — wiring.go mounts it on the outer mux, not the
// RequireAuth-wrapped apiMux, because the web UI reads the version
// before the user is authenticated to decide which screens to show.
//
// Signature shape (srv, mux, deps...) is shared with every out-of-band
// RegisterXxx in this package — see RegisterIgate, RegisterPackets,
// RegisterPosition, RegisterStations. The version string itself is
// sourced from webapi.Config.Version, propagated from cmd-level build
// flags through pkg/app/wiring.go.
//
// Operation IDs in the swag annotation block below are frozen against
// constants in pkg/webapi/docs/op_ids.go; `make docs-lint` enforces the
// correspondence.
func RegisterVersion(srv *Server, mux *http.ServeMux) {
	if srv == nil || mux == nil {
		return
	}
	mux.HandleFunc("GET /api/version", getVersion(srv.version))
}

// getVersion returns the build-time version string captured by
// NewServer from Config.Version.
//
// @Summary  Get server version
// @Tags     version
// @ID       getVersion
// @Produce  json
// @Success  200 {object} webapi.VersionResponse
// @Router   /version [get]
func getVersion(version string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, VersionResponse{
			Version:  version,
			Platform: runtime.GOOS,
		})
	}
}
