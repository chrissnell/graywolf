// Package redact applies privacy scrubbing to a flareschema.Flare
// after collection and before review. The scrub is mandatory — even
// dry-run runs it, because the dry-run output is what the user is
// asked to audit.
//
// Scope:
//   - Value-level regex rules: emails, Bearer tokens, hex/base64 ≥24,
//     IPv4, IPv6, MAC addresses, home-directory paths, hostname.
//   - Hostname hashing is consistent within one submission: the host's
//     real name is computed once, hashed (SHA-256 truncated to 8 hex),
//     and every literal occurrence is replaced with the same hash so
//     log lines that reference the host still cross-reference each
//     other after scrubbing.
//   - Ad-hoc redaction added through Engine.AddRegex is layered on
//     top of the built-in rules without disturbing them.
//
// Out of scope:
//   - APRS callsigns are NOT redacted. Public ham-radio identifiers;
//     the whole point of a flare is to diagnose callsign-bound
//     issues, and redacting them would defeat the operator UI's
//     correlation between flare config and observed packets.
//     TestEngine_PreservesAPRSCallsigns locks this in.
//   - Key-based config dropping (aprs.passcode dropped entirely;
//     *password*/*secret*/*token*/*_key/*_secret values redacted) is
//     applied by the configdump collector at source — not here.
//
// Design: .context/2026-04-25-graywolf-flare-system-design.md
//
//	§ "Subsystem 2 — graywolf flare CLI" → "Privacy & Scrubbing".
package redact
