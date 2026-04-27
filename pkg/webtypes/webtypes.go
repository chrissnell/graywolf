// Package webtypes holds types shared across web-facing packages.
//
// Keeping these shapes in a single, dependency-free package lets
// both pkg/webapi and pkg/webauth reference the same Go type so
// swag emits exactly one schema per wire shape in the OpenAPI
// spec (no per-package duplicates like webapi.ErrorResponse /
// webauth.ErrorResponse).
package webtypes

// ErrorResponse is the standard JSON error envelope used by every
// /api/* endpoint. Its wire shape is `{"error": "message"}`.
type ErrorResponse struct {
	Error string `json:"error"`
}
