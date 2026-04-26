package flareschema

import (
	"encoding/json"
	"fmt"
)

// Unmarshal decodes a flare payload, rejecting payloads whose
// schema_version is greater than this build's SchemaVersion. Older
// payloads pass through unchanged — migrating them is the receiver's
// job, not this package's.
//
// The two-pass decode is deliberate: peeking at the version header
// first lets us return ErrUnsupportedSchemaVersion before attempting to
// fit a future-shaped payload into our current Flare struct, which
// would otherwise produce confusing partial-decode results.
func Unmarshal(data []byte) (*Flare, error) {
	var hdr versionHeader
	if err := json.Unmarshal(data, &hdr); err != nil {
		return nil, fmt.Errorf("flareschema: decode version header: %w", err)
	}
	if hdr.SchemaVersion > SchemaVersion {
		return nil, ErrUnsupportedSchemaVersion{Got: hdr.SchemaVersion}
	}
	var f Flare
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("flareschema: decode payload: %w", err)
	}
	return &f, nil
}
