# Graywolf TODO — unfinished modem features

Items below were claimed in user-facing surfaces (README, channel-edit
dropdown) before being wired through the modem. Surfaces have been
walked back; implementation work below is what's needed to honestly
re-expose them.

## FX.25 forward error correction

Native TX/RX is wired for 1200-baud Bell 202 AFSK (1200/2200 Hz) only.
See [FX.25 implementation and interoperability](docs/fx25.md) for standard
formats, bounds, independent fixtures and hardware acceptance status.
Other rates/modulations are deliberately excluded.

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

- RX: `PskDemodulator` works, reachable via `modem_type=psk` at
  `graywolf-modem/src/modem/mod.rs:1031`.
- TX: no PSK modulator exists. Channels with `modem_type=psk` silently
  fall back to `afsk_mod::modulate` because `tx/mod.rs` ignores
  `modem_type` and always calls AFSK. Result: PSK channels receive
  V.26-A/B PSK but transmit AFSK on top of the same audio path —
  guaranteed unreadable on the air.
- Needed: `graywolf-modem/src/tx/psk_mod.rs` implementing V.26 A/B
  differential QPSK at the configured baud (default 2400), plus
  dispatch in `tx/mod.rs` keyed off `modem_type`.
- Until done: `psk` is kept in the dropdown (RX-only is still useful
  for monitor-only stations) but should grow a "(RX only)" annotation.

## G3RUH 9600 baud (GFSK)

- RX: implemented at `graywolf-modem/src/modem_9600/mod.rs` —
  `Demod9600` with G3RUH descrambler (poly `x^17 + x^12 + 1`),
  baseband FSK + low-pass, DPLL clock recovery, ported from direwolf
  `demod_9600.c`. Reachable via `modem_type` strings `"9600"`,
  `"scramble"`, or `"baseband"` (`modem/mod.rs:1047`).
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

## Surfaces walked back

- `README.md:60`: FX.25 / IL2P FEC line — to be removed.
- `web/src/routes/Channels.svelte:93-98`: `gfsk`, `fsk` removed from
  `modemOptions`.
- `web/src/lib/api.js`: mock channels updated from `gfsk*` to `afsk`
  so dev mode no longer surfaces a non-existent modem in the UI.
- `docs/wiki/glossary.md:24-25` and `docs/wiki/code-map.md:21-22`:
  IL2P remains a codec library; FX.25 is wired only for 1200-baud Bell 202.
