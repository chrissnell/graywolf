// Package submit posts a scrubbed flareschema.Flare to
// graywolf-flare-server. The Client interface lets tests inject a
// fake transport without an httptest.Server; the HTTPClient is the
// production implementation.
//
// Errors:
//
//	ErrSchemaRejected   server replied 400 (caller should print body)
//	ErrRateLimited      server replied 429 (caller should print retry-after)
//	ErrServerError      server replied 5xx (caller should save pending)
//	ErrPayloadTooLarge  body exceeds 5 MB (caller should warn)
//
// All error types preserve the server's response body where
// available so the operator can debug schema-version mismatches and
// the like.
package submit
