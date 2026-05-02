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

## Behavioral source citations

State-transition handlers cite the kernel function and source-line
range they derive from directly in the function's doc comment (e.g.
`ax25_std_state3_machine` at `net/ax25/ax25_std_in.c:141-259`). The
mapping from graywolf state name to kernel function:

- `StateDisconnected` → `ax25_rcv` decision, `ax25_in.c:317-427`
- `StateAwaitingConnection` → `ax25_std_state1_machine`,
  `ax25_std_in.c:39-96`; T1 expiry at `ax25_std_timer.c:120-141`
- `StateConnected` → `ax25_std_state3_machine`,
  `ax25_std_in.c:141-259`; kick at `ax25_out.c:286-324`
- `StateTimerRecovery` → `ax25_std_state4_machine`,
  `ax25_std_in.c:266-414`; T1 expiry at `ax25_std_timer.c:155-165`
- `StateAwaitingRelease` → `ax25_std_state2_machine`,
  `ax25_std_in.c:103-134`; T1 expiry at `ax25_std_timer.c:143-148`

Default constants (T1/T2/T3/N2/paclen/window/backoff) cite
`include/net/ax25.h:148-160`. RTT calc and clamps cite
`ax25_subr.c:220-258` and `include/net/ax25.h:20-21`.

## FRMR policy

graywolf/pkg/ax25conn does NOT emit FRMR. We match the Linux kernel
deviation from spec §4.3.3.9: the kernel never sends FRMR (verified
by grep — no `ax25_send_frmr` exists). On receipt of FRMR, or any
frame `ax25_decode()` flags as illegal, we tear the link to state 1
and re-SABM via `establishDataLink()`. This both matches the kernel
and is simpler than maintaining an FRMR info-byte encoder that no
Linux peer would ever accept anyway.

If a future Phase wants spec-correct FRMR transmission, the v2.2
info-field layout is documented in AX.25 v2.2 §4.3.3.9. Note:
kernel peers will tear the link down on FRMR receipt regardless of
info-field validity, so spec-correctness gains nothing on the wire.

## Test coverage debt — kernel-trace JSON replay (Phase 4 task 4.1b)

Phase 1 ships with hand-coded Go integration scenarios in
`integration_test.go` covering handshake/I-exchange, T1→TIMER_RECOVERY
recovery, REJ requeue, and peer-DISC. These scenarios verify the
implementation is internally consistent but do NOT verify byte-for-byte
agreement with a real Linux peer on the wire. JSON-driven kernel-trace
replay fixtures (originally Phase 1 task 1.15) are deferred to Phase 4
task 4.1b. Until those land, first-time interop with a kernel BBS is
the de-facto compliance test.

## On-air fixture provenance — Phase 2 smoke test (pending)

Phase 2 ships the WebSocket bridge (`pkg/ax25termws`), the
`/api/ax25/terminal` endpoint, and the Svelte terminal route. None of
that surface has been validated against a real kernel-AX.25 peer in
this branch — the smoke test is left as an operator gate before the
Phase-2/3 merge to main.

Recommended procedure (matches plan task 2.18):

1. On a Linux box, bring up an AX.25 stack on a loopback radio. The
   minimal recipe is `axports` plus `kissattach` over a `mkfifo` /
   `socat` pair so two callsigns share one virtual KISS modem.
2. Configure a graywolf channel with mode `aprs+packet` (or `packet`).
3. Build & run the branch (`make build && bin/graywolf -config ...`).
   Open `/terminal`, fill the PreConnectForm, click Connect.
4. Verify on the wire:
   - SABM goes out from graywolf.
   - UA comes back from the Linux side.
   - Terminal status flips to CONNECTED.
   - Bytes typed at the Linux shell appear in the xterm canvas.
   - Bytes typed in the canvas reach the Linux shell.
5. Click Disconnect; verify DISC -> UA -> DISCONNECTED.
6. When validation completes, replace this section with the actual
   capture details (peer software + version, callsigns, anything that
   needed a fix in the bridge or state machine).

## SREJ

graywolf does not emit SREJ even when modulus is 128. AX.25 v2.2
treats SREJ as optional, and the Linux kernel never emits or
accepts it. Out-of-order recovery on mod-128 uses REJ + go-back-N,
exactly as on mod-8. See cheat sheet §5 for verification grep.

## Specification

- AX.25 v2.2 (Jul 1998): https://www.ax25.net/AX25.2.2-Jul%2098-2.pdf
- AX.25 v2.0 (Oct 1984): https://bitsavers.informatik.uni-stuttgart.de/communications/arrl/AX.25_Link-Layer_Protocol_Ver_2.0_198410.pdf
- K3NA, 1988 CNC paper: https://web.tapr.org/meetings/CNC_1988/CNC1988-AX.25DataLinkStateMachine-K3NA.pdf
