package webapi

import (
	"net/http"
	"sync"

	"github.com/chrissnell/graywolf/pkg/releasenotes"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
	"github.com/chrissnell/graywolf/pkg/webauth"
	"github.com/chrissnell/graywolf/pkg/webtypes"
)

// responseEnvelopeSchemaVersion is the top-level schema tag for the
// /api/release-notes response envelope. Bumped when the envelope shape
// changes in a way existing clients can't understand.
const responseEnvelopeSchemaVersion = 1

// releaseNotesLoader returns every note and the caller's "unseen"
// subset. Wired as a package-level indirection so tests can inject a
// forced parse-failure. Production callers use the releasenotes
// package directly.
type releaseNotesLoader struct {
	all    func() ([]releasenotes.Note, error)
	unseen func(lastSeen string) ([]releasenotes.Note, error)
}

// defaultLoader is mutable so tests can inject a forced parse-failure
// or a fixed note slice. Reads and writes go through loaderMu so a
// future t.Parallel() doesn't race against production handler traffic
// in the same test binary.
var (
	loaderMu      sync.RWMutex
	defaultLoader = releaseNotesLoader{
		all:    releasenotes.All,
		unseen: releasenotes.Unseen,
	}
)

func currentLoader() releaseNotesLoader {
	loaderMu.RLock()
	defer loaderMu.RUnlock()
	return defaultLoader
}

func setLoader(l releaseNotesLoader) releaseNotesLoader {
	loaderMu.Lock()
	defer loaderMu.Unlock()
	prev := defaultLoader
	defaultLoader = l
	return prev
}

// RegisterReleaseNotes installs the three release-notes endpoints on
// apiMux. All three require authentication (apiMux is behind
// RequireAuth via wiring.go).
//
//   - GET  /api/release-notes         — every note
//   - GET  /api/release-notes/unseen  — caller's unseen subset
//   - POST /api/release-notes/ack     — server-authoritative ack
//
// version is the running build version (webapi.Config.Version); auth
// is the auth store used both for the ack write and for reading the
// caller's current LastSeenReleaseVersion.
//
// Signature shape (srv, mux, deps...) matches the other out-of-band
// registrars (RegisterVersion, RegisterPackets, …).
//
// Operation IDs are frozen in pkg/webapi/docs/op_ids.go; docs-lint
// enforces the correspondence.
func RegisterReleaseNotes(srv *Server, mux *http.ServeMux, version string, auth *webauth.AuthStore) {
	if srv == nil || mux == nil || auth == nil {
		return
	}
	mux.HandleFunc("GET /api/release-notes", listReleaseNotes(srv, version))
	mux.HandleFunc("GET /api/release-notes/unseen", listUnseenReleaseNotes(srv, version))
	mux.HandleFunc("POST /api/release-notes/ack", ackReleaseNotes(srv, version, auth))
}

// listReleaseNotes returns every release note. Used by the About
// page's "What's new" section.
//
// @Summary  List all release notes
// @Tags     release-notes
// @ID       listReleaseNotes
// @Produce  json
// @Success  200 {object} dto.ReleaseNotesResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /release-notes [get]
func listReleaseNotes(srv *Server, version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		notes, err := currentLoader().all()
		if err != nil {
			srv.internalError(w, r, "listReleaseNotes", err)
			return
		}
		writeJSON(w, http.StatusOK, buildResponse(version, notes))
	}
}

// listUnseenReleaseNotes returns the authenticated user's unseen
// subset (strictly newer than their LastSeenReleaseVersion). Used by
// the login-time popup.
//
// @Summary  List unseen release notes for the caller
// @Tags     release-notes
// @ID       listUnseenReleaseNotes
// @Produce  json
// @Success  200 {object} dto.ReleaseNotesResponse
// @Failure  401 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /release-notes/unseen [get]
func listUnseenReleaseNotes(srv *Server, version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := webauth.AuthenticatedUser(r)
		if user == nil {
			// RequireAuth should have already rejected this request;
			// the nil check is defense-in-depth so a misconfigured
			// mux doesn't return private data on an unauthenticated
			// request.
			writeJSON(w, http.StatusUnauthorized, webtypes.ErrorResponse{Error: "authentication required"})
			return
		}
		notes, err := currentLoader().unseen(user.LastSeenReleaseVersion)
		if err != nil {
			srv.internalError(w, r, "listUnseenReleaseNotes", err)
			return
		}
		resp := buildResponse(version, notes)
		resp.LastSeen = user.LastSeenReleaseVersion
		writeJSON(w, http.StatusOK, resp)
	}
}

// ackReleaseNotes marks every note up to and including the running
// build version as seen for the authenticated user. The handler does
// not consume a request body — ack is server-authoritative (see plan
// D9), removing a minor client-supplied-version spoofing vector.
// Idempotent: re-acking is a no-op.
//
// @Summary  Acknowledge every release note through the current build
// @Tags     release-notes
// @ID       ackReleaseNotes
// @Produce  json
// @Success  204
// @Failure  401 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /release-notes/ack [post]
func ackReleaseNotes(srv *Server, version string, auth *webauth.AuthStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := webauth.AuthenticatedUser(r)
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, webtypes.ErrorResponse{Error: "authentication required"})
			return
		}
		// Explicitly NOT parsing the body. Any content the client
		// sends is ignored; ack stays server-authoritative.
		//
		// Ack to max(buildVersion, highest note version in the binary).
		// Writing the running build alone leaves a forward-dated note
		// (authored at vN+1 but shipping in vN) perpetually unseen:
		// Compare(vN+1, vN) > 0, so on every page load Unseen re-emits
		// the note and the popup reappears even after the user clicks
		// Got It. Taking the max means ack covers every note currently
		// embedded in this binary, while staying server-authoritative
		// (no client input). Future releases that add a newer note
		// (vN+2) correctly re-surface it — LastSeenReleaseVersion of
		// vN+1 is still < vN+2.
		ackVersion := version
		if notes, lerr := currentLoader().all(); lerr == nil {
			for _, n := range notes {
				if releasenotes.Compare(n.Version, ackVersion) > 0 {
					ackVersion = n.Version
				}
			}
		}
		// If the loader failed we fall back to the running build
		// version — worst case is today's behaviour (a forward-dated
		// note resurfaces), not broken.
		if err := auth.SetLastSeenReleaseVersion(r.Context(), user.ID, ackVersion); err != nil {
			srv.internalError(w, r, "ackReleaseNotes", err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// buildResponse converts a []releasenotes.Note into the
// ReleaseNotesResponse envelope. Sorting is already applied inside the
// releasenotes package (CTA-first, version-desc); this function only
// performs the shape translation.
func buildResponse(version string, notes []releasenotes.Note) dto.ReleaseNotesResponse {
	out := dto.ReleaseNotesResponse{
		SchemaVersion: responseEnvelopeSchemaVersion,
		Current:       version,
		Notes:         make([]dto.ReleaseNoteDTO, 0, len(notes)),
	}
	for _, n := range notes {
		out.Notes = append(out.Notes, dto.ReleaseNoteDTO{
			SchemaVersion: n.SchemaVersion,
			Version:       n.Version,
			Date:          n.Date,
			Style:         n.Style,
			Title:         n.Title,
			Body:          n.Body,
		})
	}
	return out
}
