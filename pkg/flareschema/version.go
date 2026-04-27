package flareschema

import "fmt"

// SchemaVersion is the wire schema version this build emits and accepts.
// Bumping requires:
//   - committing a new docs/flareschema/v<N>.json
//   - documenting the migration in the flare-server's release notes
//   - leaving the prior version's accept path in place until that build
//     is no longer in production
const SchemaVersion = 1

// ErrUnsupportedSchemaVersion is returned by Unmarshal when the payload's
// schema_version is greater than this build's SchemaVersion. Older
// versions are intentionally not rejected here — the server is responsible
// for migrating them up.
type ErrUnsupportedSchemaVersion struct {
	Got int
}

func (e ErrUnsupportedSchemaVersion) Error() string {
	return fmt.Sprintf("flareschema: unsupported schema_version %d (this build supports up to %d)", e.Got, SchemaVersion)
}

// versionHeader is the minimal shape Unmarshal peeks at before deciding
// whether to deserialize the rest of the payload.
type versionHeader struct {
	SchemaVersion int `json:"schema_version"`
}
