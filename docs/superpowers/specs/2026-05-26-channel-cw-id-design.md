# Channel TX Test Signals — Design

**Date:** 2026-05-26
**Status:** Approved for planning

## Summary

Two related changes:

1. **Remove the broken Test Tone feature** from Audio Interfaces. It plays a
   1 kHz tone straight to the cpal output device — it never keys PTT (so a real
   radio transmits nothing) and it fails on shared input/output dongles (AIOC)
   because opening the output stream while capture is active returns "device no
   longer available." It is a soundcard toy that does not exercise the radio.

2. **Add a per-channel "Test TX" dropdown menu** that transmits a chosen test
   signal through the channel's real TX path: key PTT → play audio → unkey.
   This is a genuine end-to-end TX self-test (PTT keying + audio routing +
   deviation, all verifiable by ear or meter on a second radio). The CW option
   doubles as legal station identification.

   The menu offers four options:
   - **Send callsign in CW** — the station callsign in Morse (~20 WPM, 700 Hz).
   - **Send 1200 Hz tone** — a steady 1200 Hz tone for 3 seconds.
   - **Send 2400 Hz tone** — a steady 2400 Hz tone for 3 seconds.
   - **Send 1200/2400 Hz alternating tone** — alternating between the two for
     3 seconds.

This effectively moves the "test transmit" concept from Audio Interfaces (where
PTT/TX has no meaning) up to Channels (where it does).

## Decisions (from brainstorming)

- **Scope:** Manual menu only. No automatic/periodic timers.
- **UI:** A "Test TX" button on each channel row opens a chonky-ui
  `DropdownMenu` with the four options. Shown only for TX-capable channels.
- **CW parameters:** Hardcoded — 20 WPM, 700 Hz sidetone, PARIS timing.
- **Tone parameters:** Hardcoded — 3 second duration; alternating tone switches
  every 200 ms (≈2.5 Hz warble). 1200 Hz and 2400 Hz are the two tone
  frequencies (1200 is the Bell 202 mark tone; the pair exercises AFSK-style
  deviation).
- **Callsign source / guard:** the CW option resolves
  `Store.ResolveStationCallsign(ctx)` (the centralized station callsign) and
  refuses (HTTP 422) with a clear error if it is empty or N0CALL — N0CALL must
  never reach the air. The three tone options do **not** require a callsign and
  work regardless of station-callsign state.
- **Where the "recipes" live:** the four UI options map to hardcoded parameter
  sets in one place in Go (the webapi handler). The Rust modem is a pure
  renderer keyed by a numeric `kind` + parameters — no hardcoded frequencies or
  durations buried in Rust.

## Architecture

### Part 1 — Remove Test Tone

Delete the entire chain:

- **Frontend:** `playTestTone()` + the Test Tone button and its state in
  `web/src/routes/AudioDevices.svelte`.
- **Go webapi:** the `POST /api/audio-devices/{id}/test-tone` route + the
  `playTestTone` handler in `pkg/webapi/audio_devices.go`; `TestToneResponse`
  in `pkg/webapi/dto/audio_device.go`; the op-id entry in
  `pkg/webapi/docs/op_ids.go`; related assertions in
  `pkg/webapi/audio_devices_test.go`.
- **Bridge:** `PlayTestTone` in `pkg/modembridge/requests.go`; the tone
  dispatcher + `TestToneResult` dispatch in `bridge.go` / `session.go`; the
  `PlayTestTone` case in `bridge_stop_test.go` and its adapter.
- **IPC proto:** the `PlayTestTone` and `TestToneResult` messages in the
  `.proto`, with regenerated `pkg/ipcproto/graywolf.pb.go` and
  `graywolf-modem/src/ipc/proto.rs`.
- **Rust modem:** `play_test_tone_blocking`, `handle_play_test_tone`, the
  `Some(Payload::PlayTestTone(..))` match arm, and the `TestToneResult` arm in
  the inbound-ignore list in `graywolf-modem/src/modem/mod.rs`.

Regenerate API types (`web/src/api/generated/api.d.ts`) and Swagger docs.

### Part 2 — Channel TX test signals

Reuses the existing TX worker, which already keys PTT → submits samples →
drains → unkeys per channel via `TxJob { samples: Vec<i16>, sample_rate }`
(`graywolf-modem/src/modem/tx_worker.rs`). Every test signal is just a
different way to *generate* those samples.

#### IPC

A single request message carries a numeric `kind` plus parameters; Go fills in
the hardcoded values per UI option. Using a plain `uint32 kind` (not a proto
enum) avoids prost enum-prefix-naming fragility.

```proto
// Go -> Rust: transmit a TX test signal on a channel. kind selects the
// generator: 0 = station callsign in CW, 1 = steady tone, 2 = alternating
// tone. Go fills the relevant parameters; the modem is a pure renderer.
message TransmitTestSignal {
  uint32 request_id = 1;   // echoed back in TestSignalResult
  uint32 channel = 2;      // selects output device + PTT driver
  uint32 kind = 3;         // 0=CW callsign, 1=steady tone, 2=alternating
  string callsign = 4;     // kind 0; already resolved + uppercased by Go
  uint32 cw_wpm = 5;       // kind 0
  uint32 freq_a_hz = 6;    // CW sidetone (kind 0) / tone (kind 1) / tone A (kind 2)
  uint32 freq_b_hz = 7;    // tone B (kind 2)
  uint32 duration_ms = 8;  // kinds 1, 2
  uint32 alt_period_ms = 9;// kind 2: ms per tone before switching
}

message TestSignalResult {
  uint32 request_id = 1;
  bool success = 2;
  string error = 3;        // empty on success
}
```

Oneof field numbers: `TestSignalResult` takes RX id **9** (next free after the
Test Tone removal frees 6); `TransmitTestSignal` takes TX id **22** (next free
after 21).

#### Rust modem

A pure `txtest` module (no I/O), unit-tested:
- `encode(&str) -> Vec<Segment>` — Morse symbol/timing encoder (skips unknown
  characters).
- `cw_samples(callsign, sample_rate, wpm, tone_hz) -> Vec<i16>` — encode +
  synthesize the on/off-keyed sidetone (PARIS timing, raised-cosine edges).
- `tone_samples(sample_rate, freq_hz, duration_ms) -> Vec<i16>` — steady sine
  with edge ramps.
- `alternating_samples(sample_rate, freq_a, freq_b, duration_ms, period_ms) ->
  Vec<i16>` — phase-continuous frequency switching (no click at the switch),
  with outer edge ramps.

`handle_transmit_test_signal` resolves channel + audio config exactly like
`handle_transmit_frame`, picks the generator by `kind`, applies output gain,
submits a `TxJob` to the TX worker, and replies with `TestSignalResult`. PTT
key/unkey is handled by the worker.

#### Go webapi

`POST /api/channels/{id}/test-tx` with body `{ "signal": "<id>" }` where
`<id>` ∈ `cw | tone1200 | tone2400 | alt`. The handler:
1. Parses channel ID and maps the signal id to a hardcoded
   `modembridge.TestSignalParams` (kind + frequencies + duration + period +
   wpm); unknown id → 400.
2. `requireTxCapableChannel` → 409 on failure (the channel-config error
   precedes the cw-only callsign guard).
3. For `cw` only: `ResolveStationCallsign` → 422 on empty/N0CALL, then fills
   `params.Callsign`.
4. `bridge.TransmitTestSignal` → 503 on failure.
5. `200 { "status": "sent" }`.

The four recipes (the single source of the hardcoded values):

| signal      | kind | callsign | wpm | freq_a | freq_b | duration | alt_period |
|-------------|------|----------|-----|--------|--------|----------|------------|
| `cw`        | 0    | resolved | 20  | 700    | —      | —        | —          |
| `tone1200`  | 1    | —        | —   | 1200   | —      | 3000 ms  | —          |
| `tone2400`  | 1    | —        | —   | 2400   | —      | 3000 ms  | —          |
| `alt`       | 2    | —        | —   | 1200   | 2400   | 3000 ms  | 200 ms     |

#### Go bridge

`TransmitTestSignal(ctx, params TestSignalParams) error`, mirroring the
existing typed request/dispatcher pattern; a dispatcher in `bridge.go`, dispatch
in `session.go`, and a `bridge_stop_test.go` case so it unblocks on stop.

#### Frontend

In `web/src/routes/channels/ChannelRow.svelte`, replace the Edit/Delete-only
action row with a chonky-ui `DropdownMenu` ("Test TX ▾" trigger) holding four
`DropdownMenu.Item`s, shown only for TX-capable channels (same condition that
gates the RX/TX badge / PTT indicator: modem-backed channel with an output
device). Each item `POST`s `/channels/{id}/test-tx` with its signal id. One
in-flight flag disables the trigger while a signal is transmitting. Success →
toast; failure → error toast with the server message (e.g. the CW
callsign-missing 422).

### Wiki maintenance

- `docs/wiki/code-map.md`: record the new `/api/channels/{id}/test-tx` endpoint,
  the `txtest` modem module, and the `TransmitTestSignal`/`TestSignalResult`
  IPC messages. Remove Test Tone references.
- `docs/wiki/invariants.md`: add "the CW test signal never keys the radio with
  an empty or N0CALL callsign."

## Error handling

- CW with empty/N0CALL callsign → refuse before any IPC (422); nothing keyed.
- Unknown signal id → 400 before any IPC.
- Non-TX channel → 409 before any IPC.
- Modem-side failure (no driver/sink, submit error) → `TestSignalResult.success
  = false`; the TX worker's existing sequencing guarantees PTT is released on
  submit failure (no stuck-keyed radio).

## Testing

- **Rust:** unit tests for `encode` (symbol mapping, unknown-char skip, gaps),
  `cw_samples` (length vs WPM, non-empty), `tone_samples` (length vs duration,
  non-silent), `alternating_samples` (length, non-silent). Handler test that an
  unknown channel yields `success: false`.
- **Go:** webapi tests for refusal paths (CW empty callsign → 422, CW N0CALL →
  422, unknown signal → 400, non-TX channel → 409) and success paths (each of
  the four signals with a bridge stub returning nil → 200). Bridge stop-test
  case for `TransmitTestSignal`.
- **Frontend:** menu visibility gating by TX capability; each item posts the
  right signal id; success/error toast on stubbed responses.
- **Removal:** confirm the Test Tone route/handler/DTO/UI are gone and tests
  referencing them are removed, not skipped.

## Out of scope (YAGNI)

- Automatic/periodic station ID timer.
- Configurable WPM / tone frequency / duration in the UI or config schema.
- Per-channel callsign override (uses the station callsign).
- Any CW receive/decode.
- Tone frequencies beyond the three fixed options.
