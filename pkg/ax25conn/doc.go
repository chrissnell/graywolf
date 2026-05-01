// Package ax25conn implements an AX.25 v2.0 / v2.2 LAPB data-link
// state machine for outbound (client) connections. One graywolf
// session corresponds to one (local-addr, peer-addr, channel)
// triple. The package owns its own goroutine per session, consumes
// non-UI frames from pkg/app/ingress, and emits encoded frames
// through pkg/txgovernor.
//
// Outbound-only: this package responds to unsolicited inbound SABMs
// with DM. It is not a BBS host. See
// .context/2026-05-01-ax25-terminal-brainstorm.md for the design
// rationale.
//
// Reference implementations: ax25-tools (axcall.c, ax25d.c) and the
// Linux kernel net/ax25/ stack. See CREDITS.md for the attribution
// policy and per-function source citations.
package ax25conn
