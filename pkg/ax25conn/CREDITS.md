# pkg/ax25conn — Upstream attribution

This package is licensed GPL-2.0 (matching graywolf overall). Its
behavior is derived from the AX.25 v2.0/v2.2 specification plus
two reference implementations consulted under their compatible GPL-2.0
licenses:

## Reference codebases

- **Linux kernel `net/ax25/`** (GPL-2.0). Authoritative behavioral
  reference for edge cases the spec leaves ambiguous. Pinned at tag
  `v6.12` (commit `06090c9b622a7e1f797e775db4c035e0d779b76e`). Files
  consulted: `ax25_std_in.c`, `ax25_std_subr.c`, `ax25_std_timer.c`,
  `ax25_in.c`, `ax25_out.c`, `ax25_subr.c`, `ax25_addr.c`,
  `ax25_timer.c`, `af_ax25.c`. Source:
  https://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git
- **ax25-tools / linuxax25** (GPL-2.0). Calling-client behavior
  baseline. Cross-checked from `github.com/ve7fet/linuxax25` master
  (`ax25apps/call/call.c`). The userspace tools defer to the kernel
  state machine; no separate userspace re-implementation exists.

We re-implement in idiomatic Go from documented behavior plus the
spec, not by translating C verbatim. When a Go function adopts a
non-obvious algorithm, edge-case handler, or tuning value from
either codebase, the function's doc comment names the source file,
function, and (when stable) line range. Default tuning constants
(T1/T2/T3/N2/k/paclen/backoff) live in defaults.go with a citation
block.

## Behavioral cheat sheet

The graywolf-internal cheat sheet at
`.context/2026-05-01-ax25-lapb-behavioral-reference.md` transcribes
every state transition from the kernel sources at v6.12 with line
citations. Phase 1 transition tasks reference its sections (`§1`
state tables, `§2` timers, `§3` collision, `§4` FRMR, `§5`
REJ/SREJ, `§6` busy/RNR, `§7` digi paths, `§8` SABME negotiation,
`§9` defaults, `§10` pitfalls). Treat it as the primary source;
re-derive directly from the kernel only when the cheat sheet is
silent on the question at hand.

## Specification

- AX.25 v2.2 (Jul 1998): https://www.ax25.net/AX25.2.2-Jul%2098-2.pdf
- AX.25 v2.0 (Oct 1984): https://bitsavers.informatik.uni-stuttgart.de/communications/arrl/AX.25_Link-Layer_Protocol_Ver_2.0_198410.pdf
- K3NA, 1988 CNC paper: https://web.tapr.org/meetings/CNC_1988/CNC1988-AX.25DataLinkStateMachine-K3NA.pdf
