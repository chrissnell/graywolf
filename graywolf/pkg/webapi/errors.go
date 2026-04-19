package webapi

import (
	"net/http"

	"github.com/chrissnell/graywolf/pkg/webtypes"
)

// The canonical JSON error envelope for every non-2xx response lives
// in pkg/webtypes (webtypes.ErrorResponse). Sharing the type across
// webapi and webauth keeps the OpenAPI spec pointing at a single
// schema for failure bodies. The wire shape is intentionally
// unchanged from the historical map-based writers — `{"error": "..."}`
// — so existing clients and tests are unaffected.

// badRequest writes a 400 with a generic JSON error body. Use for
// validation failures and malformed request bodies.
func badRequest(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusBadRequest, webtypes.ErrorResponse{Error: msg})
}

// notFound writes a 404 with a generic JSON error body.
func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, webtypes.ErrorResponse{Error: "not found"})
}

// conflict writes a 409 with a JSON error body. Use when a request is
// syntactically valid but rejected by an invariant (duplicate name,
// state-machine mismatch, etc).
func conflict(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusConflict, webtypes.ErrorResponse{Error: msg})
}

// serviceUnavailable writes a 503 with a JSON error body. Use when a
// dependency the handler needs isn't wired yet (e.g. messages service
// before Phase 5 hooks it up).
func serviceUnavailable(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusServiceUnavailable, webtypes.ErrorResponse{Error: msg})
}

// NOTE: a `methodNotAllowed` helper used to live here for hand-rolled
// dispatchers. Every handler is now registered with a Go 1.22
// method-scoped pattern (e.g. `mux.HandleFunc("GET /api/foo", …)`), so
// the mux emits 405 with an `Allow:` header automatically and no caller
// needs the helper. If a future hand-rolled dispatcher reintroduces the
// need, prefer upgrading the site to a method-scoped route first.

// internalError logs the real error with request context and writes a
// generic message to the client. Use for every 5xx response so we don't
// leak GORM/driver strings (e.g. "UNIQUE constraint failed: users.username")
// that enable account or schema enumeration.
func (s *Server) internalError(w http.ResponseWriter, r *http.Request, op string, err error) {
	s.logger.ErrorContext(r.Context(), "webapi internal error", "op", op, "err", err)
	writeJSON(w, http.StatusInternalServerError, webtypes.ErrorResponse{Error: "internal error"})
}
