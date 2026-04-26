// Package flareschema is the canonical Go definition of the graywolf
// flare wire payload — the schema-versioned JSON document that the
// "graywolf flare" CLI submits to graywolf-flare-server.
//
// Contract:
//   - SchemaVersion is a monotonic integer; the server accepts and
//     migrates older versions for as long as the build supports them.
//   - Every collector section carries its own []CollectorIssue so a
//     failure in one domain never aborts the whole submission.
//   - Sub-struct field tags ("json:...,omitempty" where appropriate)
//     are part of the wire contract; renaming or retagging requires a
//     SchemaVersion bump and a corresponding entry in
//     docs/flareschema/.
//
// Cross-repo use:
//
// The graywolf-flare-server lives in its own git repo
// (~/dev/graywolf-flare-server). It depends on this package via go.mod;
// during local dev it uses a `replace` directive pointing at this
// worktree, and CI pins to a tagged graywolf version so the schema and
// the server can never silently disagree.
//
// Design: .context/2026-04-25-graywolf-flare-system-design.md
//         § "Subsystem 2 — graywolf flare CLI" → "Wire Schema".
package flareschema
