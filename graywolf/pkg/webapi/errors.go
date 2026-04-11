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
