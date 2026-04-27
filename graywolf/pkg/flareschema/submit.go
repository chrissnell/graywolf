package flareschema

// SubmitResponse is the JSON document graywolf-flare-server returns
// from POST /api/v1/submit.
//
// FlareID is the server-assigned UUID; PortalToken is the unguessable
// per-flare URL token the server may use to authenticate operator-portal
// access (the CLI does not persist it locally); PortalURL is the
// fully-qualified browser link the CLI prints (and the server emails,
// when --email was supplied).
//
// SchemaVersion echoes the schema_version the server accepted —
// useful for diagnostics if the CLI ever finds itself talking to a
// server running a different build than the docs/flareschema/ commit
// the CLI was built against.
type SubmitResponse struct {
	FlareID       string `json:"flare_id"`
	PortalToken   string `json:"portal_token"`
	PortalURL     string `json:"portal_url"`
	SchemaVersion int    `json:"schema_version"`
}
