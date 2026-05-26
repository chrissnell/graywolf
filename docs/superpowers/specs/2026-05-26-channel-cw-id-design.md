# Channel CW ID — Design

**Date:** 2026-05-26
**Status:** Approved for planning

## Summary

Two related changes:

1. **Remove the broken Test Tone feature** from Audio Interfaces. It plays a
   1 kHz tone straight to the cpal output device — it never keys PTT (so a real
   radio transmits nothing) and it fails on shared input/output dongles (AIOC)
   because opening the output stream while capture is active returns "device no
   longer available." It is a soundcard toy that does not exercise the radio.

2. **Add a per-channel "Send CW ID" button** that transmits the station
   callsign in Morse through the channel's real TX path: key PTT → play
   on/off-keyed CW audio → unkey. This is a genuine end-to-end TX self-test
   (PTT keying + audio routing + deviation, all verifiable by ear on a second
   radio) and doubles as legal station identification.

This effectively moves the "test transmit" concept from Audio Interfaces (where
PTT/TX has no meaning) up to Channels (where it does).

## Decisions (from brainstorming)

- **Scope:** Manual button only. No automatic/periodic CW ID timer.
- **CW parameters:** Hardcoded defaults — ~20 WPM, ~700 Hz sidetone, PARIS
  timing. No UI, no config schema.
- **Callsign source:** `Store.ResolveStationCallsign(ctx)` (the centralized
  station callsign). If empty or N0CALL, refuse with a clear error and never
  key the radio. N0CALL must never reach the air.
- **Button placement:** A per-channel action in `ChannelRow.svelte`, alongside
  Edit/Delete. Disabled/hidden for non-TX-capable channels.

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

### Part 2 — Channel CW ID

Reuses the existing TX worker, which already keys PTT → submits samples →
drains → unkeys per channel via `TxJob { samples: Vec<i16>, sample_rate }`
(`graywolf-modem/src/modem/tx_worker.rs`). CW is just a different way to
generate those samples.

**Approach A (chosen):** dedicated IPC message; the modem owns CW synthesis.

#### IPC

- New `TransmitCwId { request_id, channel, callsign }` request message.
- New `CwIdResult { request_id, success, error }` result message (mirrors
  `TestToneResult`).
- Added to the `.proto`, regenerated into `graywolf.pb.go` and `proto.rs`.

#### Rust modem

- New pure `morse` module:
  - `encode(callsign: &str) -> Vec<Symbol>` where `Symbol` ∈ {Dit, Dah,
    intra-char gap, inter-char gap, word gap}. Unknown characters are skipped.
  - `synthesize(symbols, sample_rate, wpm, tone_hz) -> Vec<i16>` using PARIS
    timing (dit = 1.2 / wpm seconds), a ~700 Hz sine gated on for key-down with
    short rise/fall ramps to avoid key clicks.
  - Both pure and unit-tested (timing lengths, symbol mapping, empty/invalid
    input).
- `handle_transmit_cw_id(req)`: build samples via the `morse` module, submit a
  `TxJob` to the TX worker for `req.channel`, reply with `CwIdResult`. PTT
  key/unkey is handled by the worker. Errors (no TX device/driver for the
  channel, worker failure) map to `success: false` with a message.
- New `Some(Payload::TransmitCwId(..))` match arm; `CwIdResult` added to the
  inbound-ignore list.

#### Go bridge

- `TransmitCwID(ctx, channel uint32, callsign string) error` in
  `pkg/modembridge/requests.go`, mirroring the request/dispatcher pattern of
  the existing typed requests; a `cwIdDispatcher` in `bridge.go` and dispatch
  in `session.go`; a `bridge_stop_test.go` case so it unblocks on stop.

#### Go webapi

- `POST /api/channels/{id}/cw-id`:
  1. Parse channel ID.
  2. `ResolveStationCallsign` → on empty/N0CALL, `422` with a clear message
     ("set your station callsign before sending CW ID").
  3. `requireTxCapableChannel` → on failure, `409`/`422` consistent with the
     beacon-send path.
  4. `bridge.TransmitCwID` → on failure, `503`/`500` as appropriate.
  5. `200 { "status": "sent" }`.
- New DTO `CwIdResponse { Status string }` in `pkg/webapi/dto`.
- Op-id entry; Swagger annotations; regenerated docs + `api.d.ts`.

#### Frontend

- In `web/src/routes/channels/ChannelRow.svelte`, add a "Send CW ID" `Button`
  (ghost variant) in the action row next to Edit/Delete. Show only for
  TX-capable channels (same condition that gates the RX/TX badge / PTT
  indicator — modem-backed channel with an output device). On click, `POST` the
  endpoint; success → toast "Sent CW ID on \"<channel>\""; failure → error
  toast with the server message. Disable the button while in flight.

### Wiki maintenance

- `docs/wiki/code-map.md`: record the new `/api/channels/{id}/cw-id` endpoint
  and the `morse` modem module; remove Test Tone references.
- `docs/wiki/invariants.md`: add "CW ID never keys the radio with an empty or
  N0CALL callsign."

## Error handling

- Empty/N0CALL callsign → refuse before any IPC; nothing is keyed.
- Non-TX channel → refuse before any IPC.
- Modem-side failure (no driver/sink, submit error) → `CwIdResult.success =
  false`; the TX worker's existing sequencing guarantees PTT is released on
  submit failure (no stuck-keyed radio).

## Testing

- **Rust:** unit tests for `morse::encode` (symbol mapping, unknown-char skip)
  and `morse::synthesize` (sample-count vs. WPM timing, non-empty output,
  ramping). A handler test that a `TransmitCwId` with no registered
  driver/sink yields `success: false`.
- **Go:** webapi tests for the three refusal paths (empty callsign, N0CALL,
  non-TX channel) and the success path (bridge stub returns nil). Bridge
  stop-test case for `TransmitCwID`.
- **Frontend:** button visibility gating by TX capability; success/error toast
  on stubbed responses.
- **Removal:** confirm the Test Tone route/handler/DTO/UI are gone and tests
  referencing them are removed, not skipped.

## Out of scope (YAGNI)

- Automatic/periodic station ID timer.
- Configurable WPM / tone frequency.
- Per-channel callsign override (uses the station callsign).
- Any CW receive/decode.
