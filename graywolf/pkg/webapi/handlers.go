package webapi

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// decodeJSON reads a JSON request body into T and rejects any unknown
// fields. Handlers route every request decode through this helper so
// the API contract fails loudly when a client sends a misspelled or
// deprecated field instead of silently dropping it.
func decodeJSON[T any](r *http.Request) (T, error) {
	var out T
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, fmt.Errorf("decode: %w", err)
	}
	return out, nil
}
