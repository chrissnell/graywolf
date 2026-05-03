// Package remoteactions implements the OUTBOUND half of the Actions
// feature: operator-curated macros and TOTP credentials used to fire
// `@@<otp>#<action> [k=v ...]` invocations at remote stations from
// inside Messages.
//
// This package is a sibling of pkg/actions/ (the inbound Actions
// runner), not a fork. The two share nothing at runtime: outbound
// macros never enter the inbound classifier, and inbound invocations
// never read remote credentials. They share only the wire grammar,
// which is owned by pkg/actions/parser.go and re-used here for
// validation parity.
//
// Schema: see migration 16 in pkg/configstore/migrate_remote_actions.go.
// Models in this package's models.go are the in-memory shape; they are
// intentionally NOT registered with AutoMigrate.
//
// Composition root: Service (service.go), constructed from
// pkg/app/wiring.go after the messages.Service is wired. Failure to
// construct is non-fatal — the REST handlers return 503 and the UI
// drawer reads as empty.
package remoteactions
