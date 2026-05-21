# Android Bluetooth KISS TNC Support — Design

Date: 2026-05-19
Status: Approved (brainstorming)
Scope: Android app (Kotlin platform service + proto extension); Go-side KISS
serial transport adapter; Svelte Kiss page Bluetooth picker. Desktop Linux
and macOS unchanged.

## Goal

Let an Android-tablet operator add a Bluetooth-paired KISS TNC (Mobilinkd
TNC3/TNC4, Kenwood TH-D74/D75, etc.) as a KISS interface in Graywolf:
the Kotlin platform service holds the RFCOMM socket, the bytes stream over
the existing platform UDS into the existing `pkg/kiss` serial data path
via dependency injection, and the operator sees a Bluetooth interface
type in `Kiss.svelte` with a bonded-device picker. Desktop continues to
use the existing `rfcomm bind` → `/dev/rfcomm0` workflow (Linux) or
`/dev/cu.*` path (macOS) under the existing **Serial** interface type
with no changes.

## Decisions (locked during brainstorming)

| Decision | Choice | Why |
|---|---|---|
| Platform scope | Android only | Desktop Linux/macOS already work via `rfcomm bind` + the existing serial-device-path field. The real gap is Android, which has no equivalent OS surface. |
| BT radio scope | Classic SPP/RFCOMM | Covers Mobilinkd TNC3/4 and Kenwood TH-D74/D75 — all current KISS-over-BT TNCs. BLE/GATT is materially different and not used by any TNC operators are buying today. |
| Pairing UX | Bonded devices only | Graywolf lists devices already paired in Android Settings; the OS pairing flow is well-supported and we don't reimplement it. |
| Bytes transport | Byte-relay over the existing platform UDS | Kotlin owns the `BluetoothSocket`; new proto messages carry framed bytes; Go-side adapter implements `io.ReadWriteCloser` and is injected as `SerialConfig.OpenFunc`. Reuses every line of the existing KISS serial data path (`SerialSupervisor`, backoff, hot-reload, metrics). |
| Wire enum | Reuse existing `KissTypeBluetooth` | The `bluetooth` value already exists in `pkg/configstore/models.go` as a placeholder; DTO validation already accepts it. No schema change. |
| UI label | "Bluetooth Serial" | More honest about the path — RFCOMM is a serial-over-Bluetooth byte stream. Label is UI-only; wire/DB stays `bluetooth`. |
| Device storage | MAC address in `KissInterface.Device` | Reuses the existing string column. Display name is re-resolved live from `BluetoothAdapter.bondedDevices` at render time. |
| Auto-generated name | `kiss-bt-<sanitized-device-name>` | Mirrors `kiss-serial-<device>`; operator can override. |
| Bonded-list refresh | Explicit Refresh button + `ACTION_BOND_STATE_CHANGED` receiver | Cheap, complementary; auto-refresh covers the common "I just paired one" case without a manual click. |
| Type-select scope on Android | `Bluetooth Serial` + `Network` only | Serial-on-`/dev/ttyACM*` is blocked by the Android sandbox (would need a separate platformsvc relay; not in v1). TCP-server is niche on a tablet. |

## Non-goals

- No BLE/GATT-based TNC support.
- No in-app Bluetooth discovery or PIN-pairing UI — the OS Settings flow handles bonding.
- No KISS-over-Bluetooth client/server (a phone exposing Graywolf as a KISS BT server) — only the dial-out-to-bonded-TNC direction.
- No USB-serial KISS TNC support on Android in v1 (the proto messages designed below are deliberately transport-agnostic so this can be added later as a one-line UI change + Kotlin opening a `UsbSerialPort` instead of a `BluetoothSocket`).
- No desktop Linux/macOS UI changes; the existing Serial path under `Kiss.svelte` covers BT TNCs via `rfcomm bind` today.

## Architecture

### Overview

```
                                            (Android process)
                                  +----------------------------+
+--------------+   platform UDS   |   GraywolfService (Kotlin) |
|  Go child    |<================>|   platformsvc.PlatformServer
|  pkg/kiss    |   SerialOpen/   |                            |
|  serial path |   SerialData    |   BtSerialAdapter          |
|              |   SerialClose   |     - BluetoothSocket      |
|  injected    |   SerialError   |     - worker thread        |
|  OpenFunc -->|                  |     - bondedDevices query  |
|  ReadWriteCloser                |                            |
+--------------+                  +----------------------------+
                                              |
                                              | RFCOMM
                                              v
                                      [paired KISS TNC]
```

The Go side's `pkg/kiss/serial.go` already exposes `SerialConfig.OpenFunc`
— an injectable function returning `(io.ReadWriteCloser, error)`. The
entire KISS RX/TX data path, `SerialSupervisor`, backoff, hot-reload, and
metrics consume that interface; they have no idea whether the underlying
bytes come from a UART, a TCP socket, or an Android-relayed RFCOMM
stream. The new Android transport implements an adapter that connects
to the platform UDS, sends a `SerialOpen{kind:bluetooth, address:MAC}`
request, and exposes `Read`/`Write`/`Close` methods backed by
`SerialData` frames in both directions.

### Proto additions (`proto/platform.proto`)

Extend the existing `PlatformMessage` oneof with five new variants. The
naming is deliberately transport-agnostic (`SerialOpen`, not
`BluetoothSerialOpen`) so a future USB-serial KISS path reuses the same
messages.

```protobuf
enum SerialKind {
  SERIAL_KIND_UNSPECIFIED = 0;
  SERIAL_KIND_BLUETOOTH   = 1;
  // USB-serial reserved for future use (1209:7388 AIOC, 10c4:ea60 Digirig, etc.)
  // SERIAL_KIND_USB         = 2;
}

message SerialOpen {
  uint32     handle  = 1;  // client-chosen, lifecycle-scoped
  SerialKind kind    = 2;
  string     address = 3;  // bluetooth: MAC; usb: device path
  // baud, parity, etc. omitted — RFCOMM is a stream, not a UART
}

message SerialData {
  uint32 handle = 1;
  bytes  data   = 2;  // raw bytes, no KISS framing here — that's Go's job
}

message SerialClose {
  uint32 handle = 1;
  string reason = 2;  // operator-visible if non-empty
}

message SerialError {
  uint32 handle = 1;
  string code   = 2;  // bond_lost | permission_denied | rfcomm_closed | io_error
  string detail = 3;
}

message SerialOpenAck {
  uint32 handle = 1;
  bool   ok     = 2;
  string error  = 3;  // non-empty when ok=false
}

// Bonded-device enumeration (no auto-refresh; called explicitly + on broadcast)
message BondedBtDevicesRequest  {}
message BondedBtDevicesResponse {
  message Device {
    string mac  = 1;
    string name = 2;  // BluetoothDevice.getName() at query time
    // BluetoothClass.MAJOR_AUDIO etc. omitted — operators pick by name
  }
  repeated Device devices = 1;
}
```

Schema version bump on `Hello.schema_version` per the existing platform
proto contract; mismatch logs warn + restarts (status quo).

### Kotlin side (`android/app/src/main/kotlin/com/nw5w/graywolf/`)

New file: `platformsvc/BtSerialAdapter.kt`. Responsibilities:

1. **Bonded-device enumeration** in response to `BondedBtDevicesRequest`.
   Reads `BluetoothAdapter.bondedDevices`. Filters to
   `BluetoothClass.SERVICE_AUDIO`-or-unset device classes — the
   Mobilinkd/Kenwood family advertises generic profiles. Per
   memory `feedback_android_usb_open_worker_thread`, the BluetoothAdapter
   query runs on a worker thread; main-thread access can block UI for
   several seconds the first time the Bluetooth stack is touched.

2. **RFCOMM connect** on `SerialOpen`. Uses
   `BluetoothDevice.createRfcommSocketToServiceRecord(SPP_UUID)` where
   `SPP_UUID = 00001101-0000-1000-8000-00805F9B34FB`. Per the same
   memory, the `connect()` call **must** run on a worker thread —
   blocking and slow. Reply `SerialOpenAck{ok=true, handle=N}` on
   success, `SerialOpenAck{ok=false, error=...}` on failure.

3. **Pump pair** after connect: one thread for the socket's
   `InputStream` (reads chunks → emits `SerialData` proto), one for the
   `OutputStream` (consumes `SerialData` proto received over UDS →
   writes to socket). Closed on `SerialClose` from either side, on
   socket EOF, or on bond-lost broadcast.

4. **Lifecycle bookkeeping**: per-handle map of `(BluetoothSocket,
   readThread, writeThread, closer)`. `Close()` joins threads with a
   short timeout, swallows already-closed exceptions.

5. **`ACTION_BOND_STATE_CHANGED` broadcast receiver** registered by
   `GraywolfService`. On any state change, pushes an unsolicited
   `BondedBtDevicesResponse` over the platform UDS so the SPA's bonded
   list refreshes without polling.

6. **Permission handling**: `BLUETOOTH_CONNECT` runtime permission
   (API 31+). Requested at first attempt to enumerate or connect.
   `SerialError{code:"permission_denied"}` if denied; the Kiss page
   surfaces a recoverable banner with a "Grant Bluetooth permission" CTA.

Manifest additions (in addition to existing entries):

```xml
<uses-permission android:name="android.permission.BLUETOOTH_CONNECT" />
<!-- BLUETOOTH_SCAN intentionally omitted: bonded-only, no discovery -->
```

### Go side

#### New file: `pkg/platformsvc/btserial.go` (build-tag `android`)

```go
// BtSerialOpen returns an io.ReadWriteCloser backed by an Android
// platform-service RFCOMM relay to the bonded device at mac.
// Suitable for direct injection as kiss.SerialConfig.OpenFunc.
func (c *Client) BtSerialOpen(ctx context.Context, mac string) (io.ReadWriteCloser, error)

// BondedBtDevices enumerates devices already paired in Android Settings.
func (c *Client) BondedBtDevices(ctx context.Context) ([]BondedBtDevice, error)

type BondedBtDevice struct {
    MAC  string
    Name string
}
```

The returned `io.ReadWriteCloser` multiplexes `SerialData` frames keyed
by a per-instance `handle`. `Read` blocks on the next inbound frame for
this handle (or returns `io.EOF` on `SerialClose`/`SerialError`).
`Write` chunks the input and emits one or more `SerialData` frames.
`Close` sends `SerialClose` and tears down the handle's read pump.

Frame multiplexing inside `pkg/platformsvc/client_impl.go`: a single
reader goroutine dispatches inbound messages to per-handle channels (the
existing pattern already used for GPS/Audio subscriptions). KISS data
rates (1200/9600 baud equivalents) are trivial vs the UDS bandwidth, so
no batching/coalescing is needed.

#### Modified: `pkg/app/wiring.go` (Android build)

When constructing the KISS manager (`Manager.StartSerial` dispatch path
exercised by `pkg/webapi/kiss.go` `notifyKissManager`), pass a
platform-aware `OpenFunc`:

```go
//go:build android
func newKissSerialOpenFunc(psv *platformsvc.Client) kiss.OpenFunc {
    return func(ctx context.Context, cfg kiss.SerialConfig) (io.ReadWriteCloser, error) {
        if cfg.IsBluetooth() {
            return psv.BtSerialOpen(ctx, cfg.Device) // MAC stored in Device column
        }
        // serial path on Android falls through to the default
        // (returns an error today since /dev/tty* is sandbox-blocked)
        return kiss.DefaultSerialOpen(cfg)
    }
}
```

Desktop builds (`//go:build !android`) construct `SerialConfig` with no
`OpenFunc` override — the existing `defaultSerialOpen` (uses
`go.bug.st/serial`) keeps handling `/dev/ttyUSB0`, `/dev/rfcomm0`, and
`/dev/cu.*` paths exactly as today.

#### Modified: `pkg/configstore/models.go`

No schema change. The existing `KissInterface.InterfaceType` column
already accepts `"bluetooth"` (per the explored enum constants); the
`Device` column carries the MAC for BT interfaces, the device path for
serial interfaces, the host for tcp-client, etc. `BaudRate` is unset
(or zero) for BT.

#### New REST endpoint: `GET /api/kiss/bonded-bt-devices`

```go
// Android only. Returns the live bonded-device list.
type BondedBtDevicesResponse struct {
    Devices []BondedBtDevice `json:"devices"`
}
type BondedBtDevice struct {
    MAC  string `json:"mac"`
    Name string `json:"name"`
}
```

Handler shells out to `platformsvc.Client.BondedBtDevices`; returns
`501 Not Implemented` on non-Android builds. The handler also subscribes
to `BondedBtDevicesResponse` push messages and exposes them via a
`GET /api/kiss/bonded-bt-devices/stream` SSE endpoint for live refresh
on `ACTION_BOND_STATE_CHANGED`. (Optional in v1; a polling Refresh
button covers the same need.)

### UI changes (`web/src/routes/Kiss.svelte`)

1. **Type select option set is platform-conditional.** Today shows
   `TCP server`, `TCP client (dial out)`, `Serial`. Android replaces
   this with `Bluetooth Serial` and `Network` (UI labels; wire values
   remain `bluetooth` and `tcp-client`).

2. **New Bluetooth branch in the add/edit form.** Rendered when
   `form.type === 'bluetooth'`. Contains:
   - A `<Select>` populated from `GET /api/kiss/bonded-bt-devices`,
     showing `{name} {mac}` per option. Refresh button next to the
     select calls the endpoint again. If the bonded list is empty,
     render a help hint pointing at Android Settings → Bluetooth.
   - **No baud-rate field** — RFCOMM is a byte stream.
   - **No Mode select** — BT TNCs are always TNC-mode (they own the
     modem and PTT). Mode is hardcoded to `tnc` server-side for
     `bluetooth` type interfaces.

3. **Interface name auto-fill.** When the operator selects a bonded
   device, `form.name` is auto-set to `kiss-bt-<slug(name)>` if empty
   or unchanged from a prior auto-fill. Operator can override.

4. **Interface card detail row.** The list view labels the type as
   "Bluetooth Serial" and shows the device's friendly name (resolved
   client-side from the bonded-list cache, with MAC as `title=` for
   tooltip).

5. **`ACTION_BOND_STATE_CHANGED` handling.** When the SSE stream or a
   polling refresh delivers a new bonded list, the open add/edit form
   re-renders its options without losing the operator's other field
   selections.

## Data flow — add a BT KISS TNC

1. Operator pairs Mobilinkd TNC4 in Android Settings → Bluetooth.
2. Opens Graywolf SPA → Kiss page → `+ Add KISS`.
3. Type select: picks `Bluetooth Serial`. Form shows the bonded picker.
4. Bonded list populates from `GET /api/kiss/bonded-bt-devices` (Go
   asks Kotlin via platformsvc; Kotlin returns the bonded list from
   `BluetoothAdapter.bondedDevices`).
5. Operator picks `Mobilinkd TNC4`. Name auto-fills to
   `kiss-bt-mobilinkd-tnc4`. Operator picks a channel.
6. `POST /api/kiss` writes a `KissInterface{Name, InterfaceType:"bluetooth",
   Device: "00:1B:DC:0F:11:22", Channel:1, Mode:"tnc", ...}`.
7. `notifyKissManager` dispatches to `Manager.StartSerial` with a
   `SerialConfig{Device:"00:1B:DC:0F:11:22", OpenFunc:newKissSerialOpenFunc(...)}`.
8. `SerialSupervisor` calls `OpenFunc`; the Android-build `OpenFunc`
   sees the BT path and calls `psv.BtSerialOpen(ctx, mac)`.
9. Platformsvc issues `SerialOpen{kind:BLUETOOTH, address:mac, handle:N}`.
10. Kotlin `BtSerialAdapter` connects RFCOMM on a worker thread, replies
    `SerialOpenAck{ok=true, handle:N}`, starts the pump pair.
11. Go's `SerialSupervisor` is now reading/writing KISS frames over an
    `io.ReadWriteCloser` that happens to be Bluetooth bytes. Inbound
    KISS frames flow through the existing RX fanout exactly as a serial
    KISS TNC's would; outbound KISS frames from the TX queue flow back.
12. Kiss page status refreshes — interface card shows "Connected", frame
    counters tick up.

## Error handling

| Failure | Detection | Response |
|---|---|---|
| Bonded device out of range mid-QSO | RFCOMM socket EOF; Kotlin pushes `SerialError{code:"rfcomm_closed"}` | Go-side `io.ReadWriteCloser.Read` returns `io.EOF`. `SerialSupervisor` runs its existing reconnect backoff (1s, 2s, 5s, 10s, capped). Kiss page card shows `Reconnecting…` state. When the TNC comes back into range and the next reconnect succeeds, the card flips to `Connected`. |
| Bonded device unpaired by the operator mid-session | `ACTION_BOND_STATE_CHANGED` with `BOND_NONE` for our MAC | Kotlin closes the socket; `SerialError{code:"bond_lost"}`. Go marks the interface failed and stops the supervisor. Kiss page shows `Unpaired` state with a CTA "Pair in Settings → re-select device". |
| `BLUETOOTH_CONNECT` permission denied | First connect attempt returns `permission_denied` | Kiss page shows a recoverable banner above the interface card with `Grant Bluetooth permission` button that triggers `requestPermission()`. Frame counters stay zero; no toast spam — one banner, one click to fix. |
| TNC paired but powered off | `connect()` throws `IOException` | `SerialOpenAck{ok=false, error:"connect failed: <message>"}`. Supervisor backs off and retries. Kiss page card shows `Waiting for TNC` state. |
| RFCOMM connect succeeds but no KISS frames ever arrive | KISS RX timeout (existing supervisor behavior) | Status stays `Connected` (the link is up) but the operator sees the frame counters not advancing. No code change — this is the operator's TNC misconfiguration to debug. |
| Schema mismatch on `Hello.schema_version` after APK upgrade | Existing platformsvc handler | Existing behavior: log warn, terminate, restart. Status quo. |
| Multiple Graywolf launches racing for the same TNC | Android `BluetoothSocket` is single-client per (device, UUID) | The second connect attempt fails; existing supervisor reports the error. Not a concern in practice (the foreground service is singleton). |

## Testing

### Go (`pkg/kiss/`, `pkg/platformsvc/`)

- `pkg/platformsvc/btserial_test.go` (build-tag `android`): mock
  `PlatformServer` exchanging the new proto messages with the client;
  asserts the `io.ReadWriteCloser` round-trip semantics, EOF on
  `SerialClose`, error on `SerialError`, multiplexed handles do not
  cross-talk.
- `pkg/kiss/serial_test.go` already exercises `SerialSupervisor` with
  an injected `OpenFunc`; add a case that returns the test mock's
  `ReadWriteCloser` and asserts KISS frame round-trip through the full
  RX/TX path.
- `pkg/webapi/kiss_test.go`: `notifyKissManager` builds a `SerialConfig`
  with `OpenFunc` set when the interface is `type=bluetooth` on Android
  builds. Drift guard: a build-tag-`!android` test asserts the
  `OpenFunc` is nil on desktop so the default open is used.

### Kotlin (`android/app/src/test/`)

- `platformsvc/BtSerialAdapterTest.kt`: a fake `BluetoothAdapter`
  surface returning a list of bonded `Device` shapes; asserts the
  worker-thread dispatch (failing the test if any RFCOMM call lands on
  the JUnit main thread), permission-denied handling, pump-pair
  shutdown on close. Uses kotlinx-coroutines-test for thread control.
- Round-trip integration test (build flavor `androidTest`): two
  in-process pumps connected by a `LocalSocketPair` standing in for a
  real RFCOMM socket; asserts bytes-in === bytes-out under chunking.

### Manual hardware matrix (pre-release)

Tracked in beta release notes per `.context/2026-05-01-android-app-design.md` §8.5:

- Pixel 8 + Mobilinkd TNC4 + Baofeng UV-5R
- Pixel 6a + Mobilinkd TNC3 + Yaesu FT-65R
- Samsung A54 + Kenwood TH-D75 (Bluetooth mode)
- Galaxy Fold 5 cover screen (320px viewport) bonded picker layout

## Phasing

| Phase | Deliverable | Effort |
|---|---|---|
| 1 | Proto extension: `platform.proto` new messages + Kotlin/Go regenerated bindings; drift guard passes. | 1 day |
| 2 | Kotlin `BtSerialAdapter`: bonded enumeration, RFCOMM open on worker thread, pump pair, `ACTION_BOND_STATE_CHANGED` receiver. Unit tests. | 3 days |
| 3 | Go `pkg/platformsvc/btserial.go` + `pkg/app/wiring.go` Android-build `OpenFunc` injection. `pkg/kiss` integration test. | 2 days |
| 4 | Webapi `GET /api/kiss/bonded-bt-devices` (+ optional SSE stream). | 1 day |
| 5 | `Kiss.svelte` Bluetooth branch: platform-conditional type select, bonded picker, name auto-fill, error banner. Card detail row relabel. | 2 days |
| 6 | Permission flow polish (banner + `requestPermission` integration); error-state UX for bond-lost/unpaired. | 1 day |
| 7 | Hardware matrix pass; handbook page (`docs/handbook/kiss-bluetooth.html`); wiki updates to `docs/wiki/code-map.md` (BtSerialAdapter row), `docs/wiki/system-topology.md` (TNC interfaces table gains Bluetooth row). | 2 days |

Total: ~12 days. Ships independently from the PTT-tab unification work
(see `2026-05-19-android-ptt-tab-design.md`); these two designs share no
files.

## What stays unchanged

- `pkg/kiss/serial.go` `SerialSupervisor`, backoff, reconnect, hot-reload.
- `pkg/kiss/manager.go` `StartSerial` dispatch.
- `pkg/configstore/models.go` `KissInterface` schema (the `bluetooth`
  enum value is already there).
- Existing KISS metrics (`graywolf_kiss_serial_*` — the BT path lights
  them up by virtue of using the same supervisor).
- Desktop Linux/macOS `Kiss.svelte` flow.
- `proto/graywolf.proto` (no changes; this is in `platform.proto`).

## Wiki updates required at implementation time

- `docs/wiki/code-map.md` — new row under the Kotlin platform-services
  table for `BtSerialAdapter.kt`.
- `docs/wiki/system-topology.md` — TNC interfaces (network) table gains
  a `KISS over Bluetooth (Android)` row.
- `docs/wiki/invariants.md` — possibly a new invariant N12 about the
  "all RFCOMM open/close paths run on a worker thread" rule (extending
  N11), or fold into N11. To decide during plan review.

## References

- Android architecture: `.context/2026-05-01-android-app-design.md` (esp. §3.3, §4.1, §10).
- KISS serial implementation: `docs/superpowers/plans/2026-05-16-kiss-serial-transport.md`.
- Companion design: `docs/superpowers/specs/2026-05-19-android-ptt-tab-design.md`.
- Relevant memories: `feedback_android_usb_open_worker_thread`,
  `feedback_uds_unlink_before_bind`, `feedback_hid_report_check_desktop_driver`.
