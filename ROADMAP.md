# Graywolf TODO — unfinished modem features

Items below were claimed in user-facing surfaces (README, channel-edit
dropdown) before being wired through the modem. Surfaces have been
walked back; implementation work below is what's needed to honestly
re-expose them.

## FX.25 forward error correction

Native TX/RX is wired for 1200-baud Bell 202 AFSK (1200/2200 Hz) only.
See [FX.25 implementation and interoperability](docs/fx25.md) for standard
formats, bounds, independent fixtures and hardware acceptance status.
Local operator RF/PTT checks passed for that scope, including ordinary AX.25
fallback for oversized packets. Other rates/modulations are deliberately
excluded. Ordinary 300-baud AFSK TX/RX code exists, but 300-baud FX.25 remains
future work and needs its own integration and acceptance tests.

## IL2P forward error correction

- Library: `graywolf-modem/src/il2p/` (`mod.rs`, `header.rs`,
  `payload.rs`, `scramble.rs`, `rs_il2p.rs`, `tests.rs`)
- Status: `encode`, `decode`, `Il2pReceiver`, header RS, payload RS,
  scrambler all implemented and unit-tested. The live wiring remains absent
  — `ModemConfig.il2p_encode` accepted but ignored.
- Wiring needed: requires separate protocol-specific integration. TX wraps via `il2p::encode`; RX
  runs `Il2pReceiver` in parallel with HDLC + FX.25.
- Note: IL2P removes the HDLC layer entirely (own framing, own
  scrambler, own RS over header and payload). RX dispatch must accept
  IL2P frames as a peer to HDLC, not as a wrapper around it.

## PSK transmit modulator

- RX: `PskDemodulator` receiver code is present, reachable via
  `modem_type=psk` in `graywolf-modem/src/modem/mod.rs`. This FX.25 work
  did not validate PSK RF reception.
- TX: no PSK modulator exists. Channels with `modem_type=psk` silently
  fall back to `afsk_mod::modulate` because `tx/mod.rs` ignores
  `modem_type` and always calls AFSK. Result: PSK channels receive
  V.26-A/B PSK but transmit AFSK on top of the same audio path —
  not a matching PSK waveform.
- Needed: `graywolf-modem/src/tx/psk_mod.rs` implementing V.26 A/B
  differential QPSK at the configured baud (default 2400), plus
  dispatch in `tx/mod.rs` keyed off `modem_type`.
- Until done: `psk` is kept in the dropdown (RX-only is still useful
  for monitor-only stations); the handbook explicitly marks TX absent.

## G3RUH 9600 baud (GFSK)

- RX: implemented at `graywolf-modem/src/modem_9600/mod.rs` —
  `Demod9600` with G3RUH descrambler (poly `x^17 + x^12 + 1`),
  baseband FSK + low-pass, DPLL clock recovery, ported from direwolf
  `demod_9600.c`. Reachable via `modem_type` strings `"9600"`,
  `"scramble"`, or `"baseband"` (`modem/mod.rs`). These receiver paths were not RF-validated here.
- TX: no 9600 modulator. `tx/` has only `afsk_mod`.
- UI: dropdown never sent any of the three working strings —
  `"gfsk"` fell through the match to the AFSK default. Dropdown
  option has now been removed.
- Needed:
  - `graywolf-modem/src/tx/g3ruh_mod.rs`: G3RUH scrambler (same
    polynomial as descrambler), NRZI, raised-cosine baseband shaping,
    direct FSK at the configured deviation.
  - Dispatch in `tx/mod.rs` keyed off `modem_type`.
  - Once TX exists, add `{ value: '9600', label: 'GFSK 9600 (G3RUH)' }`
    to `modemOptions` in `web/src/routes/Channels.svelte`. Map to
    backend string `"9600"` (already handled).

## Plain FSK (non-G3RUH)

- No demodulator and no modulator exist. UI value `fsk` was a label
  with nothing behind it; the dropdown entry has been removed.
- Decision needed: implement (e.g. unscrambled direct FSK for
  laboratory or non-AX.25 use cases) or leave permanently dropped.
  Default recommendation: drop unless a real use case appears.

## Documentation and remaining wiring

The README and HTML handbook distinguish implemented RX from TX and limit
FX.25 to 1200-baud Bell 202. They no longer advertise IL2P as live FEC or
PSK/G3RUH as complete native transceivers. The former `gfsk` and `fsk` UI
entries are removed; a receiver setting alone is not TX support. See the
handbook's channel capability matrix and the wiki code map.

Native backend selection still requires an input-device binding: literal
output-only device wiring is unfinished. With a configured native receiver,
TX does not require a successfully decoded incoming packet. Valid modem DCD
status reconciles stale busy state after reconfiguration while retaining
genuine carrier inhibition. These are distinct limits; do not bypass carrier
sensing as a workaround.
