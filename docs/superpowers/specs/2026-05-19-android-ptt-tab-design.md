# Unified PTT Page — Design

Date: 2026-05-19
Status: Approved (brainstorming)
Scope: `web/src/routes/Ptt.svelte` + companion sub-components;
removal of the Android PTT block from `web/src/routes/channels/ChannelEditModal.svelte`
and the corresponding save-branch in `web/src/routes/Channels.svelte`.
Backend, proto, and `PttConfig` schema unchanged.

## Goal

Replace the per-platform PTT-config UI divergence introduced by PR #157
with a single `Ptt.svelte` that works for both Android and desktop. The
PTT tab becomes the only place an operator configures push-to-talk on
any platform — consistent with every other Graywolf setting tab and
ending the "Android PTT lives inside the Channels modal" friction.

## Decisions (locked during brainstorming)

| Decision | Choice | Why |
|---|---|---|
| Data model | Keep `PttConfig` per-channel everywhere | PR #157 already added `PttConfig.PttMethod` as a first-class column (migration v22). On Android the "station PTT" feel falls out naturally: Android has at most one modem-backed channel, so at most one PTT row. No new singleton table, no v23 migration. |
| API surface | Keep `/api/ptt` endpoints unchanged | One set of handlers. No Android-only endpoint. |
| Proto / IPC | `ConfigurePtt.ptt_method` path unchanged | Already plumbed end-to-end by PR #157 per invariant 9c. |
| Component shape | One `Ptt.svelte`; method options array branches by platform | Same shell, same card, same dialogs. ~20 lines of data differ between platforms; zero logic divergence. |
| Add/edit modal | Split into two dialogs (Change Method → Change Device) | Replaces today's single cramped modal. UX win on desktop too. |
| Channel selector | Auto-hide when exactly one modem-backed channel needs a PTT row | Generic rule, not Android-specific. Single-radio desktop users get the same nicety. |
| `+ Add PTT` button | Visible only when at least one modem-backed channel has no PttConfig row | Same conditional both platforms. On Android the button disappears once the row exists. |
| Detected-vs-Recommended classification | Server-side flag on each device row | Identical JS rendering both platforms. Desktop already classifies via `/api/ptt/available`; Android response from `UsbDeviceListRequest` gets the same `recommended: bool` shape. |
| Test PTT | 1-second key/unkey from the modal footer | Traverses the full real proto/JNI/Kotlin chain so it verifies the whole path. |
| `Off` method | Explicit method (no PTT keying), not "no row exists" | Lets an operator say "I have no PTT here, don't warn me." |
| `ptt_method` integer | Internal only; not surfaced in UI | Operators see method labels; the enum integer stays in code, logs, and invariant 9c documentation. |

## Non-goals

- No new station-global singleton table. The earlier proposal that
  introduced `android_ppt_configs` is explicitly dropped.
- No data migration. `PttConfig` rows from PR #157 are read by the new
  UI as-is.
- No removal of the read-only PTT indicator on the Channels page card —
  that's a useful "what keys this channel" surface and stays.
- No changes to `proto/graywolf.proto`, `pkg/modembridge/session.go`
  `ConfigurePtt` construction, or `android/.../UsbPttAdapter.kt`
  dispatch. These all work correctly today.
- No "VOX as the operator-default fallback" behavior. VOX is an explicit
  method choice; absence of config means `method='none'`.

## Architecture

### What's shared vs branched (the contract)

| Surface | Status | Notes |
|---|---|---|
| `PttConfig` schema | **Shared** | As-is post PR #157. |
| `/api/ptt` GET/POST/PUT/DELETE | **Shared** | No new endpoint. |
| `ConfigurePtt` proto path | **Shared** | Unchanged. |
| `Ptt.svelte` page shell (header, readiness strip, grid, detected sections, modal hosts) | **Shared** | Same component file. |
| PTT card template | **Shared** | Two-row Method/Device with `Change ›` buttons, `Test PTT (1s)` in footer. |
| Dialog A chrome (method picker) | **Shared** | `<MethodPicker/>` sub-component. |
| Dialog B chrome (device picker) | **Shared** | `<DevicePicker/>` sub-component. |
| Channel-selector auto-hide rule | **Shared** | Hide when exactly one modem-backed channel needs a PttConfig row. |
| Detected/Recommended classification | **Shared rendering** | `recommended: bool` flag comes from the server. |
| **Dialog A method options** | **Branched** (data only) | Two arrays of method objects keyed off platform. ~20 lines of data, not logic. |
| **Dialog B device source** | **Branched** (backend) | Android: `UsbDeviceListRequest` filtered by class. Desktop: existing `/api/ptt/available` + `/api/ptt/gpio-chips/{chip}/lines`. |

### Component layout (`web/src/routes/`)

```
Ptt.svelte                  -- the page; renders shell + grid + dialogs
  ptt/
    PttCard.svelte          -- the two-row Method/Device card with Test PTT
    DialogChangeMethod.svelte   -- Dialog A; props: method options array, current method
    DialogChangeDevice.svelte   -- Dialog B; props: method, device source adapter
    MethodPicker.svelte         -- radio-card list, takes method[] array
    DevicePicker.svelte         -- list with selection + permission state
    devices/
      androidDeviceSource.js    -- queries platformsvc via /api/ptt/available
      desktopDeviceSource.js    -- queries /api/ptt/available, /api/ptt/gpio-chips
      methodOptions.android.js  -- the 5 Android methods (off + Digirig + AIOC + CM108 HID + VOX)
      methodOptions.desktop.js  -- the 6 desktop methods (off + serial_rts + serial_dtr + gpio + cm108 + rigctld)
```

Platform branching is confined to `ptt/devices/`. Everything else is
platform-blind.

### Method option arrays (the entire divergence)

```js
// methodOptions.android.js
export const ANDROID_METHODS = [
  { wire: { method: 'none' },                       label: 'Off — no PTT',
    meta: 'Graywolf does not key the radio.' },
  { wire: { method: 'android', ptt_method: 1 },     label: 'Digirig (CP2102N RTS)',
    meta: 'USB-serial RTS keys the radio. Most common option.',
    deviceClass: 'cp2102n' },
  { wire: { method: 'android', ptt_method: 3 },     label: 'AIOC (CDC-ACM DTR)',
    meta: 'For AIOC firmware ≥ 1.2.0. DTR=1 / RTS=0.',
    deviceClass: 'cdc-acm' },
  { wire: { method: 'android', ptt_method: 2 },     label: 'CM108 HID GPIO',
    meta: 'Generic CM108-class adapters with GPIO 3 wired externally to PTT. Not for Digirig or AIOC.',
    deviceClass: 'hid-cm108' },
  { wire: { method: 'android', ptt_method: 4 },     label: 'VOX (no keying)',
    meta: 'Radio detects audio and keys itself. No USB device required.',
    deviceClass: null },
];

// methodOptions.desktop.js
export const DESKTOP_METHODS = [
  { wire: { method: 'none' },        label: 'Off — no PTT', /* ... */ },
  { wire: { method: 'serial_rts' },  label: 'Serial RTS', /* ... */ },
  { wire: { method: 'serial_dtr' },  label: 'Serial DTR', /* ... */ },
  { wire: { method: 'gpio' },        label: 'GPIO', /* ... */ },
  { wire: { method: 'cm108' },       label: 'CM108 HID GPIO', /* ... */ },
  { wire: { method: 'rigctld' },     label: 'rigctld', /* ... */ },
];
```

`Ptt.svelte` picks one based on `Platform.kind === 'android'` (same
client-side detection PR #157 already uses) and passes it to
`<MethodPicker/>`. No conditional rendering branches in the templates.

### `/api/ptt/available` Android-side wiring

Today the handler enumerates host devices on desktop. On Android, the
handler should return a unified `[]PttDevice` shape sourced from
`UsbDeviceListRequest` over platformsvc:

```go
type PttDevice struct {
    Path        string `json:"path"`        // android: empty (use VID:PID); desktop: /dev/ttyUSB0
    Description string `json:"description"`
    Type        string `json:"type"`        // "serial" | "cm108" | "gpio" | "usb-cdc-acm" | "usb-cp2102n" | "usb-hid"
    USBVendor   string `json:"usb_vendor,omitempty"`
    USBProduct  string `json:"usb_product,omitempty"`
    Recommended bool   `json:"recommended"` // server-side classification
    Warning     string `json:"warning,omitempty"`
    HasPermission *bool `json:"has_permission,omitempty"` // android-only; tri-state for desktop's N/A
}
```

Server-side classification rules:

- **Android**: VID:PID `10c4:ea60` (CP2102N) → recommended, type
  `usb-cp2102n`. `1209:7388` (AIOC) → recommended, type `usb-cdc-acm`.
  CM108-family VID:PIDs (`0d8c:013c`, etc.) → recommended *only if*
  GPIO output reporting is supported, type `usb-hid`. Other USB devices
  → not recommended, listed under "Other detected devices".
- **Desktop**: existing rules (CP2102/AIOC/CM108 = recommended,
  generic serial ports = other). Unchanged.

### Channel-selector auto-hide

Implementation in `Ptt.svelte`:

```svelte
const modemBackedChannels = $derived(
  channels.filter(c => c.input_device_id != null && c.mode !== 'packet')
);
const channelsNeedingPtt = $derived(
  modemBackedChannels.filter(c => !pttConfigs.has(c.id))
);
const showChannelSelector = $derived(channelsNeedingPtt.length > 1);
const showAddButton = $derived(channelsNeedingPtt.length > 0);
```

When adding (not editing) and `showChannelSelector` is false, the
channel for the new `PttConfig` row is auto-set to `channelsNeedingPtt[0].id`
and the selector is omitted. Editing always omits the selector
(channel is fixed to the row being edited).

### Two-dialog flow

```
[ Station PTT card ]
   |
   |-- [Change Method ›]  -->  Dialog A (MethodPicker)
   |                              |
   |                              |-- Save & next ›
   |                              |     |
   |                              |     |-- method needs device? --> Dialog B
   |                              |     |-- method is Off/VOX?    --> close, save
   |
   |-- [Change Device ›]  -->  Dialog B (DevicePicker, filtered by current method)
                                  |
                                  |-- Save --> close, save
                                  |-- < Back --> Dialog A
```

For first-time setup (no PTT row yet), clicking `+ Add PTT` opens
Dialog A. After picking a USB-requiring method and `Save & next ›`, B
opens automatically. `Off` / `VOX` skip B and close.

For the detected-device shortcut on the Recommended cards: clicking a
recommended device opens Dialog B directly with the matching method
pre-selected and the device pre-selected. One Save and you're done.
Clicking a card under "Other" opens Dialog A first (method isn't
obvious) and falls through normally.

## UI removal — what comes out

`web/src/routes/channels/ChannelEditModal.svelte`:
- Remove `import AndroidPttFields from './AndroidPttFields.svelte'`.
- Remove the `androidPttMethod` state.
- Remove the `{#if Platform.kind === 'android'} <AndroidPttFields .../> {/if}` block.
- Remove the `androidPttMethod` field from `handleSave`'s payload.

`web/src/routes/channels/AndroidPttFields.svelte`:
- Delete the file. The functionality lives in `Ptt.svelte` now.

`web/src/routes/Channels.svelte`:
- Remove the `androidPttMethod` parameter from `handleSave`'s callback signature.
- Remove the `if (Platform.kind === 'android' && androidPttMethod != null && channelId) { await api.post('/ppt', ...) }` save branch.

The Channels page's read-only PTT indicator row (added pre-PR #157)
stays — operators want at-a-glance visibility of "what keys this channel"
without leaving the Channels page.

## Data flow — Android first-time setup

1. Operator opens PTT tab on Android tablet. No PttConfig rows yet.
2. Readiness strip shows "No PTT configured." `+ Add PTT` visible.
3. Click `+ Add PTT` → Dialog A opens. Method options = `ANDROID_METHODS`.
   Channel selector hidden (one modem-backed channel; auto-selected).
4. Operator picks `Digirig (CP2102N RTS)` → `Save & next ›`.
5. Dialog B opens. Device list filtered to `deviceClass: 'cp2102n'`.
   Shows the connected CP2102N device with "Permission OK" or "Request permission".
6. Operator selects the device → `Save`.
7. Frontend `POST /api/ptt { channel_id: <auto>, method: 'android', ptt_method: 1, device_path: '...' }`.
8. Existing `pkg/webapi/ptt.go` `upsertPttConfig` handler writes the row.
9. `pkg/modembridge/session.go` builds `ConfigurePtt{ptt_method: 1, ...}` per invariant 9c.
10. Rust modem dispatches to `AndroidPtt`; JNI relays to Kotlin
    `UsbPttAdapter.keyCp2102nRts()`.
11. Card flips to "Active". Operator clicks `Test PTT (1s)`; the same
    chain fires for a brief key+unkey to verify everything.

## Data flow — multi-radio desktop edit

1. Operator already has three PTT configs (2m Serial RTS, 70cm GPIO, 6m rigctld).
2. PTT tab renders three cards.
3. Operator wants to swap the GPIO line on 70cm.
4. Clicks `Change Device ›` on the 70cm card. Dialog B opens with
   method = `gpio` already known; Dialog A is bypassed.
5. Dialog B fetches `/api/ptt/gpio-chips/{chip}/lines` (existing endpoint),
   shows the available lines on the chosen chip. Operator picks a different line.
6. `Save` → `PUT /api/ptt/{channel_id}` with the new `gpio_line` value.

## Error handling

Same handler-level error paths as today's Ptt.svelte (invalid device
path, KISS-only channel rejection by `validatePttChannelBacking`,
rigctld test-connection failure, etc.). Two new UI-level edge cases:

| Failure | Detection | Response |
|---|---|---|
| Operator opens PTT tab with no modem-backed channels | `channels.filter(...modem-backed).length === 0` | Empty state: "No PTT-eligible channels. PTT applies to audio-modem channels — configure a modem-backed channel on the Channels page first." `+ Add PTT` button disabled. (Same message on both platforms.) |
| Android: chosen USB device's permission is later revoked (rare; user does it explicitly) | `/api/ppt/available` returns the device with `has_permission: false` | The Station PTT card shows a warning badge and a "Re-grant permission" CTA next to the device row. Test PTT fails fast with the same error. |
| Dialog B is opened but no devices of the required class are detected | Empty list from the device source | Dialog B body shows an empty-state card explaining what device is needed (e.g., "Plug in a Digirig — looking for a CP2102N USB-serial device") with a Refresh button. |

## Testing

### Svelte unit tests (`web/src/routes/ptt/`)

- `MethodPicker.test.js`: feeds in `ANDROID_METHODS`, asserts radio
  rendering, selected state, change events. Same test with `DESKTOP_METHODS`.
- `DevicePicker.test.js`: feeds a mock device list with mixed
  `recommended` and `has_permission` states; asserts filtering by
  method's `deviceClass`, permission-prompt CTA wiring, empty state.
- `Ptt.routing.test.js` (integration): mounts `Ptt.svelte` with mocked
  `/api/ptt` and `/api/ptt/available` responses; asserts the "click
  Recommended → Dialog B opens with method preselected → Save → POST"
  shortcut, and the standalone "+ Add PTT → Dialog A → Save & next →
  Dialog B → Save → POST" flow.
- `Ptt.channelSelector.test.js`: asserts the auto-hide rule
  (`channelsNeedingPtt.length > 1` → visible; else hidden+auto-filled).

### Go (`pkg/webapi/`)

- `pkg/webapi/ptt_test.go` already exercises the `upsertPttConfig`
  handler. Add a case: POST with `{method:'android', ptt_method:N}`
  succeeds and persists; verify the `ChannelPttFromModel` summary in
  the GET response.
- `pkg/webapi/ptt_devices_test.go` (new): asserts the `Recommended`
  flag is set correctly for the Android device shapes (CP2102N → true,
  AIOC → true, FT232R → false).

### Channels-modal removal regression test

- `web/src/routes/channels/ChannelEditModal.test.js`: after the
  AndroidPttFields removal, assert that opening the modal on Android
  (`Platform.kind === 'android'`) does NOT render any PTT-related
  elements. Existing channel-only fields (callsign, audio devices, mode,
  etc.) render unchanged.

### Manual

- Android tablet: full first-time Digirig setup via the new flow;
  swap to AIOC method (re-prompts for device); Test PTT fires per the
  USB analyzer trace.
- Desktop Linux multi-radio: three PttConfig rows; edit each via the
  split-dialog flow; verify no data loss vs the existing single-modal flow.
- Single-radio desktop: channel selector auto-hides; `+ Add PTT`
  disappears after the one row is created.

## Phasing

| Phase | Deliverable | Effort |
|---|---|---|
| 1 | Extract sub-components from current `Ptt.svelte`: `PttCard.svelte`, `MethodPicker.svelte`, `DevicePicker.svelte`. No behavior change; pass-through props mirror today's modal. | 2 days |
| 2 | Split single modal into `DialogChangeMethod.svelte` + `DialogChangeDevice.svelte` with the auto-chain flow. Card gains two `Change …` buttons. Existing desktop methods only at this phase — Android methods land in phase 4. | 2 days |
| 3 | Channel-selector auto-hide rule + `+ Add PTT` visibility rule. Generic, both platforms. | 1 day |
| 4 | Add `methodOptions.android.js` + `methodOptions.desktop.js`. Wire `Platform.kind` selector. Add Android device-source backed by `/api/ptt/available` (Android branch returns USB device list). | 2 days |
| 5 | Server-side `Recommended` classification: emit `recommended: bool` on every device row in `/api/ptt/available`. Update `DevicePicker` to render the Recommended/Other split. | 2 days |
| 6 | Click-to-configure shortcut on Recommended cards: open Dialog B directly with method + device preselected. | 1 day |
| 7 | Remove `AndroidPttFields.svelte`, the `{#if Platform.kind === 'android'}` block from `ChannelEditModal.svelte`, and the Android save-branch from `Channels.svelte`. ChannelEditModal regression test. | 1 day |
| 8 | Wiki updates: `docs/wiki/code-map.md` (Ptt sub-component rows, AndroidPttFields removed); `docs/wiki/invariants.md` (note that PTT config lives only in the PTT tab; channel modal is channel-only). | 0.5 day |

Total: ~11.5 days. Phases 1–3 are pure-desktop refactor wins; the
Android unification lands in phases 4–7. Each phase ships independently.

## What stays unchanged

- `pkg/configstore/models.go` `PttConfig` schema.
- `pkg/configstore/migrate.go` (no migration v23).
- `pkg/webapi/ptt.go` handler signatures and routes.
- `pkg/webapi/dto/ptt.go` request/response shapes (the existing
  `ptt_method` field carries Android transports as before).
- `pkg/modembridge/session.go` `ConfigurePtt` construction.
- `proto/graywolf.proto` `ConfigurePtt` message.
- `graywolf-modem/src/tx/ptt.rs` `PttMethod::Android` arm.
- `android/.../usb/UsbPttAdapter.kt` `keyCp2102nRts`/`keyCm108Hid`/`keyAiocCdcRts` functions.
- Channels page read-only PTT indicator row.
- Desktop Linux/macOS PTT data flow.

## References

- PR #157: introduced the Android-in-channels divergence this design reverses.
- Invariant 9c (`docs/wiki/invariants.md`): the `ptt_method` first-class field contract — preserved by this design.
- Companion design: `docs/superpowers/specs/2026-05-19-android-bluetooth-kiss-tnc-design.md`.
- Android architecture context: `.context/2026-05-01-android-app-design.md` (esp. §3.3, §5.2, §5.5).
- Relevant memories: `feedback_ui_design_quality`, `feedback_single_user_station`, `feedback_chonky_ui_workflow`, `feedback_chonky_input_alignment`.
