<script>
  import { onMount, onDestroy } from 'svelte';
  import { Button, Input, Select, Badge, Checkbox } from '@chrissnell/chonky-ui';
  import { api, kissBt, kissUsb, kissSerial } from '../lib/api.js';
  import { Platform } from '../lib/platform.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import DataTable from '../components/DataTable.svelte';
  import Modal from '../components/Modal.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import FormField from '../components/FormField.svelte';
  import ChannelListbox from '../lib/components/ChannelListbox.svelte';
  import { channelsStore, start as startChannelsStore, invalidate as refreshChannels } from '../lib/stores/channels.svelte.js';
  import {
    countdownText,
    stateLabel,
    stateBadgeVariant,
    canRetryNow,
    formatLocalTime,
  } from '../lib/kissCountdown.js';

  // Module-level constants for poll cadence and tcp-client defaults.
  // Kept in one place so create / edit / payload code paths can't
  // drift. Values match plan D16 and Phase 4 tcp-client defaults.
  const KISS_POLL_MS = 2000;
  const CLOCK_TICK_MS = 1000;
  const CLOCK_TICK_MOD = 1_000_000;
  const RECONNECT_INIT_DEFAULT_MS = 1000;
  const RECONNECT_MAX_DEFAULT_MS = 300000;
  const TCP_PORT_DEFAULT = '8001';
  const TNC_INGRESS_RATE_DEFAULT = '50';
  const TNC_INGRESS_BURST_DEFAULT = '100';
  const BAUD_RATE_DEFAULT = '9600';
  // Standard serial line speeds offered in the Baud Rate dropdown. Covers
  // the rates real KISS TNCs use (1200 for classic AFSK packet, 9600 for
  // G3RUH and most USB TNCs, up to 115200 for high-speed links). A free-form
  // input previously accepted any value with no validation (issue #249); the
  // dropdown removes that footgun while still preserving a non-standard rate
  // already saved on an interface (see baudRateOptions).
  const STANDARD_BAUD_RATES = ['1200', '2400', '4800', '9600', '19200', '38400', '57600', '115200', '230400'];
  // Show a "stale" indicator if the 2 s poller has failed this many
  // consecutive times. At 2 s cadence, five consecutive failures ≈
  // 10 s of silence — long enough to be a real problem, short enough
  // that operators notice before the state is grossly out of date.
  const KISS_STALE_AFTER_FAILURES = 5;

  let items = $state([]);
  // Shared channelsStore subscription (D9). Direct list access —
  // ChannelListbox accepts objects with {id, name, backing}.
  let channels = $derived(channelsStore.list);
  // Consecutive /api/kiss poll failures. Reset on success.
  let pollFailures = $state(0);
  let pollIsStale = $derived(pollFailures >= KISS_STALE_AFTER_FAILURES);
  let modalOpen = $state(false);
  let editing = $state(null);
  let form = $state(emptyForm());

  // Delete confirmation (bound to the Delete-flavored ConfirmDialog).
  let confirmOpen = $state(false);
  let confirmMessage = $state('');
  let pendingDeleteId = $state(null);

  // Mode-change confirmation (distinct dialog so the operator isn't shown a
  // "Delete" button when the action is actually a routing flip).
  let modeChangeOpen = $state(false);
  let modeChangeMessage = $state('');

  // Row id whose status detail panel is currently expanded. Null when
  // none — clicking the status badge toggles.
  let expandedId = $state(null);

  // Clock tick (Phase 4 D16). We keep a rune-backed mutable number
  // bumped by setInterval(1000); every $derived countdown call reads
  // this so the UI re-renders each second without a separate store
  // per row.
  let clockTick = $state(0);
  let pollTimer = null;
  let clockTimer = null;

  const columns = [
    { key: 'type', label: 'Type' },
    { key: 'endpoint', label: 'Endpoint' },
    { key: 'channel', label: 'Channel' },
    { key: 'mode', label: 'Mode' },
    { key: 'status', label: 'Status' },
  ];

  // Platform-conditional type menu. On Android, operators see the
  // interface types we support there: Bluetooth (serial-over-RFCOMM to
  // a paired TNC), USB Serial, TCP-Client (which we relabel "Network"
  // because that's the term Android operators expect), and TCP server
  // (graywolf listens; a local KISS app — e.g. a same-device iGate
  // client — dials in over loopback). The on-device Go binary binds
  // the listen socket directly, so TCP server needs no platform
  // adapter (unlike Bluetooth/USB, which relay bytes over the platform
  // UDS); it's the same fully-supported code path as desktop. Desktop
  // keeps host-serial too. Platform.isAndroid is read reactively so the
  // menu re-derives if the JS bridge appears/disappears mid-session
  // (rare, but the reactive shape costs us nothing).
  const desktopTypeOptions = [
    { value: 'tcp',        label: 'TCP (server)' },
    { value: 'tcp-client', label: 'TCP Client' },
    { value: 'serial',     label: 'Serial' },
  ];
  const androidTypeOptions = [
    { value: 'bluetooth',  label: 'Bluetooth Serial' },
    { value: 'usbserial',  label: 'USB Serial' },
    { value: 'tcp',        label: 'TCP (server)' },
    { value: 'tcp-client', label: 'Network' },
  ];
  let typeOptions = $derived(Platform.isAndroid ? androidTypeOptions : desktopTypeOptions);

  // Bonded Bluetooth devices, fetched lazily the first time the
  // operator switches the type select to "bluetooth" with the modal
  // open. Refreshable via the Refresh button next to the picker.
  let bondedDevices = $state([]);
  let bondedLoading = $state(false);
  let bondedError = $state('');
  // Inline save-time validation error. Surfaced via the bonded-device
  // FormField's `error` prop alongside any network error from the
  // lazy loader. Cleared whenever the operator picks a device or the
  // modal reopens.
  let saveError = $state('');

  // Derived <Select> options for the bonded-device picker. chonky-ui's
  // Select doesn't honor `disabled` on individual options (it only
  // propagates value+label), so we don't ship a fake placeholder
  // entry — the Select's own `placeholder` prop handles the empty
  // state correctly. handleSave() guards against submitting an empty
  // serial_device for bluetooth interfaces.
  let bondedDeviceOptions = $derived(
    bondedDevices.map((d) => ({
      value: d.mac,
      label: `${d.name} (${d.mac})`,
    })),
  );

  // Combined error surfaced on the bonded-device FormField. Save-time
  // validation takes precedence over the network/loader error, since
  // the operator just acted and that's the more relevant signal.
  let bondedFieldError = $derived(saveError || bondedError);

  // Hint text for the bonded-device FormField. When the list is empty
  // and nothing is loading or errored, surface the "pair in Android
  // Settings first" guidance through the FormField's hint slot so it
  // gets a stable id and is wired into aria-describedby on the Select.
  //
  // Phase 6 (Option A scope): an empty list is also what we see when
  // BLUETOOTH_CONNECT was denied — BluetoothAdapter.bondedDevices
  // raises a SecurityException which BtSerialAdapter swallows into an
  // empty list. The two states are indistinguishable here, so the
  // empty-list copy nudges the operator toward the "Grant Bluetooth
  // permission" button rendered below the picker. A cleaner fix (a
  // distinct permission_denied flag on the proto response) is deferred
  // as a future enhancement; the empty-list-with-grant-button UX is
  // good enough for v1.
  let bondedHint = $derived(
    !bondedLoading && !bondedError && bondedDevices.length === 0
      ? 'No paired Bluetooth devices found. Pair your TNC in Android Settings → Bluetooth, then click Refresh. If you have already paired the TNC but no devices appear, grant Bluetooth permission below.'
      : 'Pair the TNC in Android Settings → Bluetooth first, then refresh.',
  );

  // True when the bonded list is empty and we have nothing else to
  // explain it — gate for the "Grant Bluetooth permission" button.
  // Android-only; on desktop the bondedError already says "Bluetooth
  // interfaces can only be configured from the Android app." so we
  // skip the button there.
  let showBtPermGrant = $derived(
    Platform.isAndroid &&
      form.type === 'bluetooth' &&
      !bondedLoading &&
      !bondedError &&
      bondedDevices.length === 0,
  );

  // Attached USB serial devices, loaded lazily when the operator selects
  // "usbserial" with the modal open. Refreshable; shape:
  // [{vid_pid, product, manufacturer, has_permission}].
  let usbDevices = $state([]);
  let usbLoading = $state(false);
  let usbError = $state('');

  let usbDeviceOptions = $derived(
    usbDevices.map((d) => ({
      value: d.vid_pid,
      label: d.product ? `${d.product} (${d.vid_pid})` : d.vid_pid,
    })),
  );

  // Combined error for the USB device FormField (save-time validation
  // takes precedence over the loader error, mirroring bondedFieldError).
  let usbFieldError = $derived(saveError || usbError);

  let usbHint = $derived(
    !usbLoading && !usbError && usbDevices.length === 0
      ? 'No USB serial devices detected. Plug in your KISS TNC over USB, then click Refresh. If it is plugged in but not listed, grant USB permission below.'
      : 'Plug the KISS TNC into the tablet over USB, then refresh.',
  );

  let selectedUsbDevice = $derived(
    usbDevices.find((d) => d.vid_pid === form.serial_device) || null,
  );

  // Show the "Grant USB permission" CTA when: on Android, usbserial type,
  // not loading, no error, and either no device selected (needs permission
  // to enumerate) or the selected device lacks has_permission.
  let showUsbPermGrant = $derived(
    Platform.isAndroid &&
      form.type === 'usbserial' &&
      !usbLoading &&
      !usbError &&
      (selectedUsbDevice ? !selectedUsbDevice.has_permission : usbDevices.length > 0 ? false : true),
  );

  // Host serial ports for the desktop "serial" interface type, loaded
  // lazily the first time the operator opens the modal with type=serial
  // (mirrors the bluetooth / usbserial lazy-loaders). Shape per entry:
  // {path, name, description, is_usb, recommended, warning}. Enumeration
  // is best-effort — on failure the operator falls back to typing the
  // port path manually in the Serial Device input below the dropdown.
  let serialPorts = $state([]);
  let serialPortsLoading = $state(false);
  let serialPortsError = $state('');

  let serialPortOptions = $derived(
    serialPorts.map((p) => ({
      value: p.path,
      label: p.description && p.description !== p.name ? `${p.description} (${p.path})` : p.path,
    })),
  );

  let serialPortsHint = $derived(
    serialPortsError
      ? serialPortsError
      : !serialPortsLoading && serialPorts.length === 0
        ? 'No serial ports detected. Plug in the KISS TNC and click Refresh, or type the port path manually below.'
        : 'Pick a detected port to fill in the device path, or type it manually below.',
  );

  // Baud-rate dropdown options. Built from STANDARD_BAUD_RATES, with the
  // current form value prepended when it isn't one of the standards so
  // editing an interface saved with a non-standard rate doesn't silently
  // drop or rewrite it.
  let baudRateOptions = $derived.by(() => {
    const opts = STANDARD_BAUD_RATES.map((r) => ({ value: r, label: r }));
    const cur = form.baud_rate;
    if (cur && !STANDARD_BAUD_RATES.includes(String(cur))) {
      opts.unshift({ value: String(cur), label: `${cur} (custom)` });
    }
    return opts;
  });

  const modeOptions = [
    { value: 'modem', label: 'Modem' },
    { value: 'tnc', label: 'TNC' },
  ];

  // Hint text is the primary explanation of Mode — option labels are
  // deliberately terse. Wired to the <Select> via aria-describedby so
  // screen readers announce it when the field gains focus.
  let modeHint = $derived(
    form.mode === 'tnc'
      ? "Peer is a hardware TNC supplying off-air RX. Frames are routed to igate, digipeater, messages, and map — never auto-retransmitted."
      : "Peer is an APRS app. Frames it sends are queued for transmission on graywolf's radio."
  );

  const modeLabels = { modem: 'Modem', tnc: 'TNC' };

  function emptyForm() {
    return {
      type: 'tcp',
      tcp_port: TCP_PORT_DEFAULT,
      // tcp (server) bind scope: false => listen on all interfaces
      // (0.0.0.0), true => loopback only (127.0.0.1). Off by default to
      // match desktop semantics; the operator opts into local-only.
      local_only: false,
      // tcp-client fields:
      remote_host: '',
      remote_port: TCP_PORT_DEFAULT,
      reconnect_init_ms: String(RECONNECT_INIT_DEFAULT_MS),
      reconnect_max_ms: String(RECONNECT_MAX_DEFAULT_MS),
      serial_device: '',
      baud_rate: BAUD_RATE_DEFAULT,
      channel: '1',
      mode: 'modem',
      tnc_ingress_rate_hz: TNC_INGRESS_RATE_DEFAULT,
      tnc_ingress_burst: TNC_INGRESS_BURST_DEFAULT,
      // Phase 3 D4: governor-TX opt-in. Default false for new rows —
      // operators must knowingly enable cross-channel digipeat / beacon
      // / iGate transmission via a KISS-TNC link. Only meaningful when
      // Mode == "tnc".
      allow_tx_from_governor: false,
      // When the operator enables this on a Mode=modem interface,
      // frames a connected KISS client submits for TX are ALSO
      // offered to the iGate's RF->IS gate after the TX governor
      // accepts them. Default off — operator opts in.
      gate_tx_to_is: false,
      // Whether graywolf runs this interface. New rows start enabled;
      // the operator can disable an existing one to release its device
      // (e.g. a Bluetooth rfcomm tty) without losing the config.
      enabled: true,
    };
  }

  // Phase 4 D16: poll /api/kiss every 2s while the page is open so
  // the Status column reflects live supervisor state. The shared
  // channelsStore handles the channel list; this is the per-page
  // interface poller.
  async function refreshItems() {
    try {
      items = (await api.get('/kiss')) || [];
      pollFailures = 0;
    } catch (err) {
      // Leave the old list in place; surfacing a toast on every
      // failed 2 s poll would be obnoxious. Instead bump the failure
      // counter — once we cross KISS_STALE_AFTER_FAILURES the header
      // renders a muted "stale" pill so operators know the data is
      // no longer fresh.
      pollFailures = pollFailures + 1;
      if (pollFailures === KISS_STALE_AFTER_FAILURES) {
        console.warn('kiss: /api/kiss poll has failed', pollFailures, 'times in a row');
      }
    }
  }

  onMount(async () => {
    await refreshItems();
    startChannelsStore();
    pollTimer = setInterval(refreshItems, KISS_POLL_MS);
    clockTimer = setInterval(() => {
      clockTick = (clockTick + 1) % CLOCK_TICK_MOD;
    }, CLOCK_TICK_MS);
    // Pre-warm the bonded list on Android so the table can show
    // friendly names for Bluetooth rows immediately on first paint.
    // Uses a silent fetch — if it fails we just fall back to bare
    // MACs in the table, and the operator will see the real error
    // when they open the modal and trigger the lazy loader there.
    if (Platform.isAndroid) prewarmBondedDevices();
  });

  onDestroy(() => {
    if (pollTimer) clearInterval(pollTimer);
    if (clockTimer) clearInterval(clockTimer);
  });

  function openCreate() {
    editing = null;
    form = emptyForm();
    // emptyForm() defaults to `tcp` (the desktop common case). On
    // Android, Bluetooth-to-a-paired-TNC is the far more common setup,
    // so default new interfaces there to bluetooth. (tcp is selectable
    // on Android too — this is a default-value choice, not a menu
    // restriction.)
    if (Platform.isAndroid) {
      form.type = 'bluetooth';
    }
    saveError = '';
    // Default AllowTxFromGovernor=true for new tcp-client rows per
    // plan D4. When the operator flips to tcp-client we pre-check
    // the governor-TX checkbox so the common case (outbound TNC for
    // digipeat/beacon) works one-click.
    modalOpen = true;
  }

  function openEdit(row) {
    editing = row;
    saveError = '';
    form = {
      ...row,
      tcp_port: String(row.tcp_port ?? ''),
      local_only: !!row.local_only,
      remote_host: row.remote_host || '',
      remote_port: String(row.remote_port || ''),
      reconnect_init_ms: String(row.reconnect_init_ms || RECONNECT_INIT_DEFAULT_MS),
      reconnect_max_ms: String(row.reconnect_max_ms || RECONNECT_MAX_DEFAULT_MS),
      baud_rate: String(row.baud_rate),
      channel: String(row.channel || 1),
      mode: row.mode || 'modem',
      tnc_ingress_rate_hz: String(row.tnc_ingress_rate_hz || 50),
      tnc_ingress_burst: String(row.tnc_ingress_burst || 100),
      allow_tx_from_governor: !!row.allow_tx_from_governor,
      gate_tx_to_is: !!row.gate_tx_to_is,
      // Carry the persisted enabled state forward. KISS PUT is a
      // full-resource replace, so echoing it keeps a disabled interface
      // disabled across edits to unrelated fields. Default true for
      // legacy rows whose response predates the field.
      enabled: row.enabled !== false,
    };
    modalOpen = true;
  }

  // Phase 4 D4: new tcp-client rows default governor-TX on. Watch
  // the form.type field and flip the checkbox when the operator
  // selects tcp-client for the first time on a new row. (Not fired
  // on edit — operator's existing choice is preserved.)
  $effect(() => {
    if (editing) return;
    if (form.type === 'tcp-client' && !form._clientDefaultsApplied) {
      form.allow_tx_from_governor = true;
      form.mode = 'tnc'; // tcp-client + governor TX only makes sense in TNC mode
      form._clientDefaultsApplied = true;
    }
  });

  // Phase 5: Bluetooth-typed interfaces are always TNCs (the paired
  // device IS the modem). Force Mode=tnc whenever the operator flips
  // type to bluetooth so the Mode-gated UI (rate/burst, governor-TX
  // checkbox) renders, and so buildPayload() doesn't try to send
  // mode=modem to a server that would reject it.
  $effect(() => {
    if (form.type === 'bluetooth' && form.mode !== 'tnc') {
      form.mode = 'tnc';
    }
  });

  // Lazy-load bonded devices the first time the operator opens the
  // modal AND selects bluetooth. Re-runs only when the gate
  // conditions flip back to satisfied — operator can clear an error
  // and click Refresh to retry without remounting. Gated on
  // Platform.isAndroid: on desktop the GET returns 501 by design, so
  // we short-circuit with a friendly message instead of letting the
  // operator stare at "HTTP 501".
  $effect(() => {
    if (form.type !== 'bluetooth' || !modalOpen) return;
    if (!Platform.isAndroid) {
      if (!bondedError) {
        bondedError = 'Bluetooth interfaces can only be configured from the Android app.';
      }
      return;
    }
    if (bondedDevices.length === 0 && !bondedLoading && !bondedError) {
      loadBondedDevices();
    }
  });

  async function loadBondedDevices() {
    bondedLoading = true;
    bondedError = '';
    try {
      const resp = await kissBt.bondedDevices();
      bondedDevices = resp?.devices ?? [];
    } catch (err) {
      bondedError = err?.message ?? 'Failed to load Bluetooth devices';
    } finally {
      bondedLoading = false;
    }
  }

  // Phase 6 (Option A scope): prompt the operator to grant
  // BLUETOOTH_CONNECT via the Android JS bridge, then re-poll the
  // bonded-device list on success. The lambda is wired through
  // MainActivity (which owns requestPermissions()); the WebAppInterface
  // method exists only on Android, so the optional-chain on
  // window.GraywolfWebInterface is the desktop guard.
  //
  // window.__btResult is wired up BEFORE invoking the bridge so a
  // synchronous already-granted reply (API <31 or permission already
  // granted) can't race past us. The dispatcher is one-shot: it only
  // re-loads the bonded list when the id matches; other in-flight ids
  // are ignored (none today, but defensive against a future caller).
  //
  // NOTE: A "Bond lost" UI state (the supervisor reports an
  // ex-bonded device went Unpaired) is intentionally NOT plumbed here.
  // That would require a new state on the backend response and a
  // signal up through the supervisor; it's deferred as a future
  // enhancement.
  function requestBtPerm() {
    if (!Platform.isAndroid) return;
    if (!globalThis.GraywolfWebInterface?.requestBluetoothPermission) return;
    // Prefix guarantees a non-empty alphanumeric id even if Math.random()
    // returns 0 — same callback-id pattern the Android USB-grant flow uses.
    const callbackId = 'bt-' + Math.random().toString(36).slice(2);
    const prev = globalThis.__btResult;
    globalThis.__btResult = (id, granted) => {
      if (id !== callbackId) return;
      // One-shot: restore (or clear) the previous handler so a stale
      // callback fired after we tear down can't refire loadBondedDevices.
      if (prev) globalThis.__btResult = prev;
      else delete globalThis.__btResult;
      if (granted) loadBondedDevices();
    };
    try {
      globalThis.GraywolfWebInterface.requestBluetoothPermission(callbackId);
    } catch (err) {
      console.error('requestBluetoothPermission failed:', err);
      // Roll back the dispatcher swap so a later callback for a
      // different id isn't dropped on the floor.
      if (prev) globalThis.__btResult = prev;
      else delete globalThis.__btResult;
    }
  }

  // Lazy-load attached USB serial devices the first time the operator
  // opens the modal with the "usbserial" type selected. Mirrors the
  // bluetooth lazy-loader pattern above. Gated on Platform.isAndroid:
  // desktop returns 501, so we short-circuit with a friendly message.
  $effect(() => {
    if (form.type !== 'usbserial' || !modalOpen) return;
    if (!Platform.isAndroid) {
      if (!usbError) {
        usbError = 'USB serial interfaces can only be configured from the Android app.';
      }
      return;
    }
    if (usbDevices.length === 0 && !usbLoading && !usbError) {
      loadUsbDevices();
    }
  });

  async function loadUsbDevices() {
    usbLoading = true;
    usbError = '';
    try {
      const resp = await kissUsb.availableDevices();
      usbDevices = resp?.devices ?? [];
    } catch (err) {
      usbError = err?.message ?? 'Failed to load USB serial devices';
    } finally {
      usbLoading = false;
    }
  }

  // Lazy-load host serial ports the first time the operator opens the modal
  // with the "serial" type selected. Unlike bluetooth/usbserial this works
  // on every desktop platform (the ports are enumerated on the host running
  // graywolf), so there's no Platform gate. Re-runs only when the gate flips
  // back to satisfied — the operator can clear an error and click Refresh.
  $effect(() => {
    if (form.type !== 'serial' || !modalOpen) return;
    if (serialPorts.length === 0 && !serialPortsLoading && !serialPortsError) {
      loadSerialPorts();
    }
  });

  async function loadSerialPorts() {
    serialPortsLoading = true;
    serialPortsError = '';
    try {
      serialPorts = (await kissSerial.availablePorts()) ?? [];
    } catch (err) {
      serialPortsError = err?.message ?? 'Failed to load serial ports';
    } finally {
      serialPortsLoading = false;
    }
  }

  // Grant USB permission for the selected device via the Android JS bridge.
  // Uses the shared __usbResult / __usbCallbacks dispatcher pattern (same
  // one the PTT picker uses in androidDeviceSource.js). The dispatcher is
  // installed idempotently so Kiss.svelte and the PTT picker can coexist.
  // On grant, re-poll the device list so has_permission flips.
  function requestUsbPerm() {
    if (!Platform.isAndroid) return;
    const bridge = globalThis.GraywolfWebInterface;
    if (!bridge?.requestUsbPermission) return;
    const dev = selectedUsbDevice;
    if (!dev) return;
    const [vidHex, pidHex] = dev.vid_pid.split(':');
    const vid = parseInt(vidHex, 16);
    const pid = parseInt(pidHex, 16);
    if (Number.isNaN(vid) || Number.isNaN(pid)) return;
    // Install the singleton dispatcher idempotently (matches androidDeviceSource.js).
    if (!globalThis.__usbResult) {
      globalThis.__usbResult = (id, granted) => {
        const cb = globalThis.__usbCallbacks?.[id];
        if (cb) cb(granted);
        delete globalThis.__usbCallbacks?.[id];
      };
      globalThis.__usbCallbacks = {};
    }
    const callbackId = 'cb' + Math.random().toString(36).slice(2);
    globalThis.__usbCallbacks[callbackId] = (granted) => {
      if (granted) loadUsbDevices();
    };
    try {
      bridge.requestUsbPermission(vid, pid, callbackId);
    } catch (err) {
      console.error('requestUsbPermission failed:', err);
      delete globalThis.__usbCallbacks[callbackId];
    }
  }

  // Silent background fetch used only to pre-populate friendly names
  // in the table. Failures are intentionally not surfaced — the modal
  // path (loadBondedDevices) is the one that reports errors to the
  // operator when it's actionable.
  async function prewarmBondedDevices() {
    try {
      const resp = await kissBt.bondedDevices();
      if (Array.isArray(resp?.devices)) bondedDevices = resp.devices;
    } catch {
      /* swallow — falls back to bare MACs in the table */
    }
  }

  // Auto-fill a friendly slug name when the operator picks a bonded
  // device. Only overwrites empty names or names that look like a
  // previous auto-fill (kiss-bt-*) so manually-named interfaces are
  // preserved on re-pick. The form/DTO doesn't model a name field
  // today, but interfaces carry .name server-side (used in the
  // NeedsReconfig banner); leaving the helper in place means a
  // future name input wires up for free.
  function autofillBtName() {
    if (!form.serial_device) return;
    // Operator just picked a device — clear any "pick a device first"
    // validation error so it doesn't linger as visual noise.
    saveError = '';
    const d = bondedDevices.find((x) => x.mac === form.serial_device);
    if (!d) return;
    const slug = d.name
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-|-$/g, '');
    const autoName = `kiss-bt-${slug}`;
    if (!form.name || (typeof form.name === 'string' && form.name.startsWith('kiss-bt-'))) {
      form.name = autoName;
    }
  }

  function buildPayload() {
    const data = {
      type: form.type,
      channel: parseInt(form.channel) || 1,
      mode: form.mode,
      tnc_ingress_rate_hz: parseInt(form.tnc_ingress_rate_hz) || 0,
      tnc_ingress_burst: parseInt(form.tnc_ingress_burst) || 0,
      // Only send allow_tx_from_governor when Mode=tnc. The backend
      // validator rejects the combination (Mode!=tnc with allow=true)
      // so defensively coerce here to match UI visibility.
      allow_tx_from_governor: form.mode === 'tnc' ? !!form.allow_tx_from_governor : false,
      // Only meaningful in Mode=modem (Mode=tnc already feeds the
      // iGate via the RX fanout). Force false outside that mode so
      // the persisted value matches the UI's visibility rule.
      gate_tx_to_is: form.mode === 'modem' ? !!form.gate_tx_to_is : false,
      // Full-resource replace: always send enabled so a PUT never
      // silently re-enables a disabled interface (the backend defaults a
      // missing flag to true). Default true for new rows.
      enabled: form.enabled !== false,
    };
    switch (form.type) {
      case 'tcp':
        data.tcp_port = parseInt(form.tcp_port) || 0;
        data.local_only = !!form.local_only;
        break;
      case 'tcp-client':
        data.remote_host = (form.remote_host || '').trim();
        data.remote_port = parseInt(form.remote_port) || 0;
        data.reconnect_init_ms = parseInt(form.reconnect_init_ms) || RECONNECT_INIT_DEFAULT_MS;
        data.reconnect_max_ms = parseInt(form.reconnect_max_ms) || RECONNECT_MAX_DEFAULT_MS;
        break;
      case 'serial':
      case 'bluetooth':
      case 'usbserial':
        data.serial_device = form.serial_device || '';
        data.baud_rate = parseInt(form.baud_rate) || 9600;
        break;
    }
    return data;
  }

  async function commitSave() {
    const data = buildPayload();
    try {
      if (editing) {
        await api.put(`/kiss/${editing.id}`, data);
        toasts.success('KISS config updated');
      } else {
        await api.post('/kiss', data);
        toasts.success('KISS config created');
      }
      modalOpen = false;
      await refreshItems();
      refreshChannels();
    } catch (err) {
      toasts.error(err.message);
    }
  }

  function handleSave() {
    // Client-side guard: chonky-ui's Select doesn't honor per-option
    // `disabled`, so without this check an operator could leave the
    // bonded-device picker at its empty placeholder and submit a
    // bluetooth interface with serial_device=''. The server would
    // accept it and the supervisor would fail to dial later. Catch
    // it here and surface the error inline on the FormField.
    if (form.type === 'bluetooth' && !form.serial_device) {
      saveError = 'Pick a bonded device before saving.';
      return;
    }
    if (form.type === 'usbserial' && !form.serial_device) {
      saveError = 'Pick a USB serial device before saving.';
      return;
    }
    saveError = '';
    // Mode changes on an existing interface take effect the instant the
    // server restarts the per-interface KISS server — connected peers see
    // routing behavior flip under them. Make the operator confirm it.
    if (editing && form.mode !== editing.mode) {
      modeChangeMessage = `Change mode from ${modeLabels[editing.mode] || editing.mode} to ${modeLabels[form.mode] || form.mode}? Connected peers will see routing behavior change immediately.`;
      modeChangeOpen = true;
      return;
    }
    commitSave();
  }

  // labelForType maps the stored interface-type discriminator to the
  // operator-facing label. The tcp-client relabel to "Network" on
  // Android mirrors typeOptions so the modal and the table stay in
  // sync. Falls through to the raw type for forward-compat with any
  // server-side type we don't recognize yet.
  function labelForType(t) {
    switch (t) {
      case 'tcp':        return 'TCP (server)';
      case 'tcp-client': return Platform.isAndroid ? 'Network' : 'TCP Client';
      case 'serial':     return 'Serial';
      case 'bluetooth':  return 'Bluetooth Serial';
      case 'usbserial':  return 'USB Serial';
      default:           return t;
    }
  }

  // friendlyDevice returns a human-readable endpoint string for the
  // interface row. For bluetooth rows we cross-reference the cached
  // bondedDevices list (populated when the modal was open) so the
  // operator sees "Mobilinkd TNC3 (AA:BB:...)" instead of a bare MAC.
  // Falls back to the bare device id when we don't have a name yet
  // (e.g. table rendered before the modal has ever been opened).
  function friendlyDevice(iface) {
    const dev = iface.device || iface.serial_device || '';
    if (iface.type !== 'bluetooth') return dev;
    const match = bondedDevices.find(
      (d) => d.mac === iface.device || d.mac === iface.serial_device,
    );
    return match ? `${match.name} (${dev})` : dev;
  }

  function describeRow(row) {
    const mode = modeLabels[row.mode] || 'Modem';
    if (row.type === 'tcp') return `TCP server on port ${row.tcp_port}, ${mode}`;
    if (row.type === 'tcp-client') return `TCP client → ${row.remote_host}:${row.remote_port}, ${mode}`;
    if (row.type === 'serial') {
      const dev = (row.serial_device || '').trim();
      return dev ? `serial ${dev}, ${mode}` : `serial, ${mode}`;
    }
    if (row.type === 'bluetooth') {
      const dev = friendlyDevice(row);
      return dev ? `bluetooth ${dev}, ${mode}` : `bluetooth, ${mode}`;
    }
    if (row.type === 'usbserial') {
      const dev = (row.serial_device || '').trim();
      return dev ? `usb ${dev}, ${mode}` : `usb, ${mode}`;
    }
    return `#${row.id}, ${mode}`;
  }

  function endpointText(row) {
    if (row.type === 'tcp') return row.local_only ? `127.0.0.1:${row.tcp_port}` : `:${row.tcp_port}`;
    if (row.type === 'tcp-client') return `${row.remote_host}:${row.remote_port}`;
    if (row.type === 'serial') return row.serial_device || '—';
    if (row.type === 'bluetooth') return friendlyDevice(row) || '—';
    if (row.type === 'usbserial') return row.serial_device || '—';
    return '—';
  }

  function handleDelete(row) {
    pendingDeleteId = row.id;
    confirmMessage = `Delete KISS interface (${describeRow(row)}) on channel ${row.channel}?`;
    confirmOpen = true;
  }

  async function confirmDelete() {
    const id = pendingDeleteId;
    pendingDeleteId = null;
    if (id == null) return;
    try {
      await api.delete(`/kiss/${id}`);
      toasts.success('Interface deleted');
      await refreshItems();
      refreshChannels();
    } catch (err) {
      toasts.error(err.message);
    }
  }

  function toggleExpanded(id) {
    expandedId = expandedId === id ? null : id;
  }

  async function handleRetryNow(row) {
    try {
      // Use api.post so 401 → login redirect and structured ApiError
      // handling (body JSON parsing, status propagation) match every
      // other write on this page. No body required for this endpoint.
      await api.post(`/kiss/${row.id}/reconnect`, null);
      toasts.success('Reconnect triggered');
      // Refresh immediately so the operator sees state move.
      await refreshItems();
    } catch (err) {
      toasts.error(err.message);
    }
  }

  // Enable/disable an interface in one click. Disabling stops the
  // supervisor and releases the device (closing the serial fd / socket)
  // instead of looping reconnect attempts; enabling restarts it. The
  // saved channel and config are preserved either way. Uses the focused
  // /enabled endpoint so we don't have to round-trip the whole form.
  async function handleToggleEnabled(row) {
    const next = row.enabled === false; // disabled -> enable, else disable
    try {
      await api.put(`/kiss/${row.id}/enabled`, { enabled: next });
      toasts.success(next ? 'Interface enabled' : 'Interface disabled — device released');
      await refreshItems();
    } catch (err) {
      toasts.error(err.message);
    }
  }

  // Healthglyph: live / down based on state. Client-only rows (never
  // server-listen) are what really benefit from this; server-listen
  // reports StateListening which is always "live" (the listener bound
  // successfully), so a green dot.
  function healthGlyph(state) {
    const connected = state === 'connected' || state === 'listening';
    return connected ? '●' : '○';
  }
  function healthClass(state) {
    return (state === 'connected' || state === 'listening') ? 'health-live' : 'health-down';
  }
</script>

<PageHeader title="KISS Interfaces" subtitle="KISS interface configuration">
  {#if pollIsStale}
    <!-- Stale indicator (D16 polish): /api/kiss has failed ≥5 times
         consecutively; status columns may be out of date. Muted on
         purpose — this is an FYI, not an alarm. -->
    <span class="poll-stale-pill" role="status" aria-live="polite" title="Status updates paused because /api/kiss is unreachable. Current rows may be out of date.">
      <span aria-hidden="true">○</span>
      Status stale
    </span>
  {/if}
  <Button variant="primary" onclick={openCreate}>+ Add KISS</Button>
</PageHeader>

{#each items.filter(i => i.needs_reconfig) as reconfigRow (reconfigRow.id)}
  <!-- Phase 5 D12: a cascade delete of a referenced channel nulls
       this interface's Channel and flags NeedsReconfig. The operator
       is expected to pick a new channel and save to clear the flag. -->
  <div class="needs-reconfig-banner" role="status" aria-live="polite">
    <span class="needs-reconfig-icon" aria-hidden="true">⚠</span>
    <span>
      <strong>{reconfigRow.name || `KISS interface #${reconfigRow.id}`}</strong>'s channel was deleted.
      Reassign a channel and save to clear this warning.
    </span>
    <Button variant="ghost" onclick={() => openEdit(reconfigRow)}>Edit</Button>
  </div>
{/each}

<DataTable
  {columns}
  rows={items}
  onEdit={openEdit}
  onDelete={handleDelete}
  cells={{ type: typeCell, endpoint: endpointCell, mode: modeCell, status: statusCell }}
/>

{#snippet modeCell(value, _row)}
  <Badge variant={value === 'tnc' ? 'success' : 'info'}>{modeLabels[value] || 'Modem'}</Badge>
{/snippet}

{#snippet typeCell(value, _row)}
  {#if value === 'tcp-client'}
    <Badge variant="info">{labelForType(value)}</Badge>
  {:else if value === 'tcp'}
    <Badge>TCP Server</Badge>
  {:else if value === 'bluetooth'}
    <Badge variant="info">{labelForType(value)}</Badge>
  {:else if value === 'usbserial'}
    <Badge variant="info">{labelForType(value)}</Badge>
  {:else if value === 'serial'}
    <Badge>{labelForType(value)}</Badge>
  {:else}
    <Badge>{value || '—'}</Badge>
  {/if}
{/snippet}

{#snippet endpointCell(_value, row)}
  <span class="endpoint">{endpointText(row)}</span>
{/snippet}

{#snippet statusCell(_value, row)}
  {@const disabled = row.enabled === false}
  <div class="status-cell" data-tick={clockTick}>
    <!-- clockTick is in data-tick so any change triggers snippet
         re-render; that propagates to the countdownText call below. -->
    <button
      type="button"
      class="status-btn"
      aria-expanded={expandedId === row.id}
      aria-controls={`status-detail-${row.id}`}
      aria-label={`Status for KISS interface ${row.id}: ${disabled ? 'Disabled' : stateLabel(row.state)}`}
      onclick={(e) => { e.stopPropagation(); toggleExpanded(row.id); }}
    >
      {#if disabled}
        <!-- A disabled interface is not running, so live supervisor
             state is meaningless. Show a neutral "Disabled" pill instead
             of the stale/empty state. -->
        <span class="health-disabled" aria-hidden="true">○</span>
        <Badge>Disabled</Badge>
      {:else}
        <span class={healthClass(row.state)} aria-hidden="true">{healthGlyph(row.state)}</span>
        <Badge variant={stateBadgeVariant(row.state)}>{stateLabel(row.state)}</Badge>
        {#if row.state === 'backoff'}
          <span class="countdown">{countdownText(row.retry_at_unix_ms)}</span>
        {/if}
      {/if}
    </button>
    {#if expandedId === row.id}
      <div id={`status-detail-${row.id}`} class="status-detail" role="region" aria-label="Status detail">
        {#if disabled}
          <div class="detail-row"><span class="detail-label">Endpoint:</span> <span>{endpointText(row)}</span></div>
          <div class="detail-row detail-muted">Disabled — the device is released and reconnection is paused. The configuration is preserved.</div>
        {:else if row.type === 'tcp-client'}
          <div class="detail-row"><span class="detail-label">Peer:</span> <span>{row.peer_addr || `${row.remote_host}:${row.remote_port}`}</span></div>
          <div class="detail-row"><span class="detail-label">Connected since:</span> <span>{formatLocalTime(row.connected_since) || '—'}</span></div>
          <div class="detail-row"><span class="detail-label">Reconnect count:</span> <span>{row.reconnect_count || 0}</span></div>
          <div class="detail-row"><span class="detail-label">Backoff:</span> <span>{row.backoff_seconds || 0}s</span></div>
          {#if row.last_error}
            <div class="detail-row detail-err"><span class="detail-label">Last error:</span> <span>{row.last_error}</span></div>
          {/if}
        {:else if row.type === 'tcp'}
          <div class="detail-row"><span class="detail-label">Listening:</span> <span>{row.local_only ? `127.0.0.1:${row.tcp_port} (local only)` : `:${row.tcp_port}`}</span></div>
        {:else if row.type === 'bluetooth'}
          <div class="detail-row"><span class="detail-label">Device:</span> <span>{friendlyDevice(row) || '—'}</span></div>
        {:else if row.type === 'usbserial'}
          <div class="detail-row"><span class="detail-label">Device:</span> <span>{row.serial_device || '—'}</span></div>
        {:else}
          <div class="detail-row"><span class="detail-label">Device:</span> <span>{row.serial_device || '—'}</span></div>
        {/if}
        <div class="detail-actions">
          {#if !disabled && row.type === 'tcp-client' && canRetryNow(row.state)}
            <Button variant="primary" onclick={(e) => { e.stopPropagation?.(); handleRetryNow(row); }}>Retry now</Button>
          {/if}
          {#if disabled}
            <Button variant="primary" onclick={(e) => { e.stopPropagation?.(); handleToggleEnabled(row); }}>Enable</Button>
          {:else}
            <Button variant="ghost" onclick={(e) => { e.stopPropagation?.(); handleToggleEnabled(row); }}>Disable</Button>
          {/if}
        </div>
      </div>
    {/if}
  </div>
{/snippet}

<Modal bind:open={modalOpen} title={editing ? 'Edit KISS' : 'New KISS Interface'}>
    {#if form.type !== 'bluetooth'}
      <FormField label="Mode" id="kiss-mode" hint={modeHint}>
        {#snippet children(describedBy)}
          <Select id="kiss-mode" bind:value={form.mode} options={modeOptions} aria-describedby={describedBy} />
        {/snippet}
      </FormField>
    {/if}
    <FormField label="Type" id="kiss-type">
      <Select id="kiss-type" bind:value={form.type} options={typeOptions} />
    </FormField>
    {#if form.type === 'tcp'}
      <FormField label="TCP Port" id="kiss-port">
        <Input id="kiss-port" bind:value={form.tcp_port} type="number" placeholder="8001" />
      </FormField>
      <!-- Bind scope for the KISS listener. Unchecked binds all
           interfaces (0.0.0.0); checked binds loopback (127.0.0.1) so
           only apps on this device can connect — the common case for an
           on-device iGate client. -->
      <div class="field checkbox-field">
        <label class="checkbox-row" for="kiss-local-only">
          <Checkbox id="kiss-local-only" bind:checked={form.local_only} />
          <span>Local only (this device)</span>
        </label>
        <span class="field-hint">
          Listen on loopback (127.0.0.1) instead of every network interface.
          Recommended when a KISS app on this same device connects in — it
          keeps the port off your Wi-Fi/LAN.
        </span>
      </div>
    {:else if form.type === 'tcp-client'}
      <FormField
        label="Remote Host"
        id="kiss-remote-host"
        hint="Hostname or IP of the remote KISS TNC to dial."
      >
        <Input id="kiss-remote-host" bind:value={form.remote_host} placeholder="lora.example.com" />
      </FormField>
      <FormField
        label="Remote Port"
        id="kiss-remote-port"
        hint="TCP port on the remote KISS TNC."
      >
        <Input id="kiss-remote-port" bind:value={form.remote_port} type="number" min={1} max={65535} placeholder="8001" />
      </FormField>
    {:else if form.type === 'bluetooth'}
      <FormField
        label="Bonded device"
        id="kiss-bt-device"
        hint={bondedHint}
        error={bondedFieldError}
      >
        {#snippet children(describedBy)}
          <div class="bt-picker">
            <Select
              id="kiss-bt-device"
              bind:value={form.serial_device}
              options={bondedDeviceOptions}
              placeholder={bondedLoading ? 'Loading…' : 'Select a bonded device'}
              onValueChange={autofillBtName}
              aria-describedby={describedBy}
            />
            <Button variant="secondary" onclick={loadBondedDevices} disabled={bondedLoading}>
              Refresh
            </Button>
          </div>
          {#if showBtPermGrant}
            <!-- Phase 6 (Option A): the supervisor returns an empty
                 bonded list both when nothing is paired AND when
                 BLUETOOTH_CONNECT was denied (the SecurityException is
                 swallowed in BtSerialAdapter). The grant button is
                 Android-only and lets the operator fire the runtime
                 permission dialog without bouncing through system
                 Settings. -->
            <div class="bt-perm-row">
              <Button variant="secondary" onclick={requestBtPerm}>
                Grant Bluetooth permission
              </Button>
            </div>
          {/if}
        {/snippet}
      </FormField>
    {:else if form.type === 'usbserial'}
      <FormField
        label="USB device"
        id="kiss-usb-device"
        hint={usbHint}
        error={usbFieldError}
      >
        {#snippet children(describedBy)}
          <div class="bt-picker">
            <Select
              id="kiss-usb-device"
              bind:value={form.serial_device}
              options={usbDeviceOptions}
              placeholder={usbLoading ? 'Loading…' : 'Select a USB serial device'}
              onValueChange={() => { saveError = ''; }}
              aria-describedby={describedBy}
            />
            <Button variant="secondary" onclick={loadUsbDevices} disabled={usbLoading}>
              Refresh
            </Button>
          </div>
          {#if showUsbPermGrant}
            <div class="bt-perm-row">
              <Button variant="secondary" onclick={requestUsbPerm}>
                Grant USB permission
              </Button>
            </div>
          {/if}
        {/snippet}
      </FormField>
      <FormField
        label="Baud Rate"
        id="kiss-usb-baud"
        hint="Serial line speed. Must match the TNC's configured baud rate. Default 9600."
      >
        <Select id="kiss-usb-baud" bind:value={form.baud_rate} options={baudRateOptions} />
      </FormField>
    {:else if form.type === 'serial'}
      <FormField
        label="Detected ports"
        id="kiss-serial-detected"
        hint={serialPortsHint}
      >
        {#snippet children(describedBy)}
          <div class="bt-picker">
            <Select
              id="kiss-serial-detected"
              bind:value={form.serial_device}
              options={serialPortOptions}
              placeholder={serialPortsLoading ? 'Loading…' : 'Select a detected port'}
              aria-describedby={describedBy}
            />
            <Button variant="secondary" onclick={loadSerialPorts} disabled={serialPortsLoading}>
              Refresh
            </Button>
          </div>
        {/snippet}
      </FormField>
      <FormField
        label="Serial Device"
        id="kiss-serial"
        hint="Port the KISS TNC is attached to. Linux: /dev/ttyUSB0 or /dev/ttyACM0. macOS: /dev/cu.usbserial-*. Windows: COM1, COM3. Pick from Detected ports above, or type it here."
      >
        <Input id="kiss-serial" bind:value={form.serial_device} placeholder="/dev/ttyUSB0 or COM3" />
      </FormField>
      <FormField
        label="Baud Rate"
        id="kiss-baud"
        hint="Serial line speed. Must match the TNC's configured baud rate. Default 9600."
      >
        <Select id="kiss-baud" bind:value={form.baud_rate} options={baudRateOptions} />
      </FormField>
    {/if}
    <FormField label="Channel" id="kiss-channel">
      {#if channels.length > 0}
        <ChannelListbox
          id="kiss-channel"
          bind:value={form.channel}
          valueType="string"
          channels={channels}
        />
      {:else}
        <Input id="kiss-channel" bind:value={form.channel} type="number" placeholder="1" />
      {/if}
    </FormField>
    {#if form.mode === 'tnc'}
      <!-- Governor-TX opt-in (Phase 3 D4). Default unchecked — the
           operator must explicitly enable transmission via this
           interface to avoid surprise loops. -->
      <div class="field checkbox-field">
        <label class="checkbox-row" for="kiss-allow-tx">
          <Checkbox id="kiss-allow-tx" bind:checked={form.allow_tx_from_governor} />
          <span>Allow digipeater/beacon/iGate to transmit on this interface</span>
        </label>
        <span class="field-warning">Do not enable this feature if your TNC is configured for digipeating or iGate mode: it will produce packet loops!</span>
      </div>
    {/if}
    {#if form.mode === 'modem'}
      <!-- Gating opt-in for connected KISS clients (YAAC, Xastir,
           APRSdroid, etc.). When enabled, packets the client submits
           for TX are also offered to the iGate's RF->IS gate after
           the TX governor accepts them. -->
      <div class="field checkbox-field">
        <label class="checkbox-row" for="kiss-gate-tx-to-is">
          <Checkbox id="kiss-gate-tx-to-is" bind:checked={form.gate_tx_to_is} />
          <span>Also forward transmissions from connected clients to APRS-IS</span>
        </label>
        <span class="field-hint">
          Useful when a KISS app (YAAC, Xastir, APRSdroid) sends through graywolf
          and you want its packets to reach APRS-IS without that app holding its
          own APRS-IS connection. The iGate must be enabled; its filter rules
          (NOGATE, RFONLY, TCPIP) still apply.
        </span>
      </div>
    {/if}
    {#if form.mode === 'tnc'}
      <!-- Per-interface ingress rate limiter. Only meaningful in TNC mode
           (Modem-mode ingest goes to the TxGovernor, not the RX fanout). -->
      <div class="advanced-section">
        <div class="advanced-label">Advanced</div>
        <FormField label="Ingress Rate (frames/sec)" id="kiss-rate"
          hint="Token-bucket refill rate for inbound frames. Default 50.">
          <Input id="kiss-rate" bind:value={form.tnc_ingress_rate_hz} type="number" min={0} max={10000} placeholder="50" />
        </FormField>
        <FormField label="Ingress Burst" id="kiss-burst"
          hint="Maximum burst size before rate limiting kicks in. Default 100.">
          <Input id="kiss-burst" bind:value={form.tnc_ingress_burst} type="number" min={0} max={100000} placeholder="100" />
        </FormField>
        {#if form.type === 'tcp-client'}
          <FormField label="Reconnect initial delay (ms)" id="kiss-reconnect-init"
            hint="First backoff delay on dial failure. Subsequent failures grow exponentially up to the max.">
            <Input id="kiss-reconnect-init" bind:value={form.reconnect_init_ms} type="number" min={250} max={3600000} placeholder="1000" />
          </FormField>
          <FormField label="Reconnect max delay (ms)" id="kiss-reconnect-max"
            hint="Upper bound on the backoff schedule.">
            <Input id="kiss-reconnect-max" bind:value={form.reconnect_max_ms} type="number" min={250} max={3600000} placeholder="300000" />
          </FormField>
        {/if}
      </div>
    {/if}
    <div class="modal-actions">
      <Button onclick={() => modalOpen = false}>Cancel</Button>
      <Button variant="primary" onclick={handleSave}>{editing ? 'Save' : 'Create'}</Button>
    </div>
</Modal>

<ConfirmDialog
  bind:open={confirmOpen}
  title="Delete Interface"
  message={confirmMessage}
  confirmLabel="Delete"
  onConfirm={confirmDelete}
/>

<ConfirmDialog
  bind:open={modeChangeOpen}
  title="Change Interface Mode"
  message={modeChangeMessage}
  confirmLabel="Change Mode"
  confirmVariant="primary"
  onConfirm={commitSave}
/>

<style>
  .poll-stale-pill {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    padding: 0.125rem 0.5rem;
    margin-right: 0.5rem;
    font-size: 0.75rem;
    color: var(--text-muted, #888);
    background: var(--surface-muted, rgba(128, 128, 128, 0.08));
    border: 1px solid var(--border-muted, rgba(128, 128, 128, 0.2));
    border-radius: 999px;
  }

  .modal-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 16px; }
  .checkbox-field {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-bottom: 12px;
  }
  .checkbox-row {
    display: flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
    font-size: 14px;
  }
  .field-warning {
    font-size: 12px;
    color: var(--color-danger, #d32f2f);
    line-height: 1.4;
  }
  .field-hint {
    display: block;
    margin-top: 0.4rem;
    font-size: 0.875rem;
    color: var(--text-secondary, #666);
  }
  .advanced-section {
    margin-top: 8px;
    padding-top: 12px;
    border-top: 1px solid var(--border-color);
  }
  .advanced-label {
    font-size: 11px;
    font-weight: 600;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.5px;
    margin-bottom: 8px;
  }
  .endpoint {
    font-family: var(--font-mono, monospace);
    font-size: 13px;
  }
  .status-cell {
    display: flex;
    flex-direction: column;
    gap: 6px;
    align-items: flex-start;
  }
  .status-btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    background: transparent;
    border: 1px solid transparent;
    border-radius: 4px;
    padding: 2px 6px;
    cursor: pointer;
    color: inherit;
    font: inherit;
  }
  .status-btn:hover,
  .status-btn:focus-visible {
    border-color: var(--border-color);
    outline: none;
  }
  .health-live { color: var(--color-success, #4caf50); font-size: 14px; }
  .health-down { color: var(--color-warning, #ffa000); font-size: 14px; }
  .health-disabled { color: var(--text-secondary, #9e9e9e); font-size: 14px; }
  .countdown {
    font-size: 12px;
    color: var(--text-secondary);
    margin-left: 4px;
  }
  .status-detail {
    background: var(--bg-elevated, #fafafa);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    padding: 8px 10px;
    font-size: 13px;
    min-width: 260px;
  }
  .detail-row {
    display: flex;
    gap: 8px;
    margin-bottom: 4px;
  }
  .detail-label {
    color: var(--text-secondary);
    min-width: 120px;
  }
  .detail-err {
    color: var(--color-error, #d32f2f);
  }
  .detail-muted {
    color: var(--text-secondary);
    font-style: italic;
  }
  .detail-actions {
    margin-top: 8px;
  }
  /* Phase 5 D12: NeedsReconfig banner. Yellow/warning row that
     advertises a KISS interface whose channel was swept by a cascade
     delete. Clearing happens via the edit form when the operator
     saves a valid channel. */
  .needs-reconfig-banner {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    margin-bottom: 8px;
    background: var(--color-warning-muted, rgba(212, 167, 44, 0.15));
    border-left: 3px solid var(--color-warning, #d4a72c);
    border-radius: var(--radius);
    color: var(--text-primary);
    font-size: 13px;
  }
  .needs-reconfig-icon {
    font-size: 16px;
    color: var(--color-warning, #d4a72c);
    flex-shrink: 0;
  }
  /* Bluetooth bonded-device picker: Select + Refresh button on one
     row. chonky inputs ship margin-bottom:1rem which shifts the
     button down inside a flex row — override locally per the project
     convention (see feedback_chonky_input_alignment in memory). */
  .bt-picker {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .bt-picker :global(select),
  .bt-picker :global(input) {
    margin: 0 !important;
    flex: 1 1 auto;
  }
  /* Grant-permission button row sits below the bt-picker; the small
     top margin separates it from the picker without making the
     control feel detached from the FormField. */
  .bt-perm-row {
    margin-top: 8px;
    display: flex;
  }
</style>
