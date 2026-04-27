// Package logbuffer persists graywolf's slog records to a bounded
// circular buffer in a standalone SQLite database (graywolf-logs.db).
//
// It is wired in cmd/graywolf/main.go as a slog.Handler that wraps the
// existing console TextHandler: every record continues to print to the
// console at the operator's selected level (INFO, or DEBUG with -debug),
// and is also persisted at DEBUG level so a later "graywolf flare"
// submission can attach recent history regardless of how the operator
// configured the console.
//
// The DB lives in a separate file from the main configstore to avoid
// write contention on the main app DB; on Raspberry Pi / SD-card-rooted
// systems the file is placed on tmpfs to avoid SD-card wear.
//
// Design: .context/2026-04-25-graywolf-flare-system-design.md
//         § "Subsystem 1 — Circular Log Persistence".
package logbuffer
