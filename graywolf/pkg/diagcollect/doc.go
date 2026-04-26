// Package diagcollect implements the body of the "graywolf flare"
// subcommand: collect a typed flareschema.Flare from the local
// install, scrub it, present it for review, and submit it.
//
// Contract:
//   - Each collector returns a typed flareschema.<Section> plus a
//     []flareschema.CollectorIssue. Section-level failures land in
//     issues; they never abort the whole flare.
//   - Redaction is a single pass after collection and before review.
//     It is mandatory for non-dry-run submissions; dry-run / --out
//     also run it (the scrub is what the user is asked to audit).
//   - The graywolf-flare-server is reached through a configurable URL.
//     Token-protected resubmission saves the portal token locally to
//     ~/.local/state/graywolf/flares/<flare-id>.json (mode 0600).
//
// Privacy invariants (also enforced by tests in redact/):
//   - APRS callsigns are NOT redacted (public identifiers; the whole
//     point is to diagnose APRS issues).
//   - Hostname hashes are SHA-256 truncated to 8 hex chars and stay
//     consistent across every field of a single submission so log
//     cross-references survive scrubbing.
//
// Design: .context/2026-04-25-graywolf-flare-system-design.md
//         § "Subsystem 2 — graywolf flare CLI".
package diagcollect
