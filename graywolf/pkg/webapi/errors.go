package webapi

import "net/http"

// badRequest writes a 400 with a generic JSON error body. Use for
// validation failures and malformed request bodies.
func badRequest(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
}

// notFound writes a 404 with a generic JSON error body.
func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

// methodNotAllowed writes a 405 with a generic JSON error body.
func methodNotAllowed(w http.ResponseWriter) {
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}

// internalError logs the real error with request context and writes a
// generic message to the client. Use for every 5xx response so we don't
// leak GORM/driver strings (e.g. "UNIQUE constraint failed: users.username")
// that enable account or schema enumeration.
func (s *Server) internalError(w http.ResponseWriter, r *http.Request, op string, err error) {
	s.logger.ErrorContext(r.Context(), "webapi internal error", "op", op, "err", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
}
