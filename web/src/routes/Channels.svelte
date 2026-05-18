<script>
  import { onMount, onDestroy } from 'svelte';
  import { Button, Input, Select, Toggle, AlertDialog } from '@chrissnell/chonky-ui';
  import { api, ApiError, postChannelPtt } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import { Platform } from '../lib/platform.js';
  import PageHeader from '../components/PageHeader.svelte';
  import Modal from '../components/Modal.svelte';
  import FormField from '../components/FormField.svelte';
  import { channelsStore, start as startChannelsStore, invalidate as refreshChannels } from '../lib/stores/channels.svelte.js';
  import { groupReferrers, totalReferrers } from '../lib/channelReferrers.js';
  import { blankForm, rowToForm, formToPayload, validateForm } from '../lib/channelForm.js';
  import ChannelRow from './channels/ChannelRow.svelte';

  // The Channels page itself hydrates the shared store: this page is
  // the cheapest place for a first-visit operator to land, so it
  // starts the poller idempotently. Other picker pages do the same;
  // whoever mounts first wins.
  let channels = $derived(channelsStore.list);
  let audioDevices = $state([]);
  let txTimings = $state({});
  let modalOpen = $state(false);
  let editing = $state(null);

  // Phase 5 two-step delete flow (D12).
  // Stage 1 ("impact") lists referrers grouped by type; the operator
  // chooses to cancel or proceed to stage 2. Stage 2 ("confirm")
  // requires typing the channel's exact name before the red button
  // enables. An unreferenced channel skips stage 1 and goes straight
  // to stage 2 with the same typed-name gate for consistency.
  let deleteTarget = $state(null);
  let deleteImpactOpen = $state(false);
  let deleteConfirmOpen = $state(false);
  let deleteReferrers = $state([]);
  let deleteNameInput = $state('');
  let deleteInFlight = $state(false);
  let deleteGroups = $derived(groupReferrers(deleteReferrers));
  let deleteTotal = $derived(totalReferrers(deleteReferrers));
  let deleteNameMatches = $derived(
    deleteTarget != null && deleteNameInput.trim() === deleteTarget.name
  );

  // Phase 3 -- channel PUT 409 confirm-and-force flow. Mirrors the
  // stage-1 impact dialog above: show the list of referrers that
  // would break if the mutation proceeded, let the operator cancel
  // or confirm, and on confirm retry the PUT with ?force=true
  // (same wire convention as ?cascade=true on DELETE).
  //
  // No typed-name gate here. A PUT that breaks referrers is
  // recoverable (the operator can edit again). A DELETE is not --
  // that's why the delete flow carries the stronger gate. The
  // referrer list itself is the confirmation surface.
  let putConfirmOpen = $state(false);
  let putReferrers = $state([]);
  let putPendingPayload = $state(null);
  let putPendingId = $state(null);
  let putServerError = $state('');
  let putInFlight = $state(false);
  let putGroups = $derived(groupReferrers(putReferrers));
  let putTotal = $derived(totalReferrers(putReferrers));
  // channel_type is a UI-only enum that drives the segmented picker
  // (D11). It is NOT serialized; the wire shape is still
  // input_device_id (nullable). 'modem' keeps all existing behavior;
  // 'kiss-tnc' hides audio fields and sends input_device_id=null.
  let form = $state({
    name: '',
    mode: 'aprs',
    channel_type: 'modem',
    input_device_id: '0', input_channel: '0',
    output_device_id: '0', output_channel: '0',
    modem_type: 'afsk', bit_rate: '1200', mark_freq: '1200', space_freq: '2200',
    tx_delay_ms: '300', tx_tail_ms: '100', slot_ms: '100', persist: '63', full_dup: false,
  });
  let errors = $state({});

  let isModemType = $derived(form.channel_type === 'modem');
  let isTxEnabled = $derived(isModemType && form.output_device_id !== '0');

  let inputDevices = $derived(audioDevices.filter(d => d.direction === 'input'));
  let outputDevices = $derived(audioDevices.filter(d => d.direction === 'output'));
  let inputDeviceOptions = $derived(inputDevices.map(d => ({ value: String(d.id), label: d.name })));
  let outputDeviceOptions = $derived([
    { value: '0', label: 'None (RX only)' },
    ...outputDevices.map(d => ({ value: String(d.id), label: d.name })),
  ]);

  const modemOptions = [
    { value: 'afsk', label: 'AFSK' },
    { value: 'psk', label: 'PSK' },
  ];

  const channelOptions = [
    { value: '0', label: '0 (Left/Mono)' },
    { value: '1', label: '1 (Right)' },
  ];

  // Android PTT method constants — must match PTT_METHOD_* in ptt_android.rs
  // and the Kotlin UsbPttAdapter dispatcher (Appendix B of the 4b spec).
  const PTT_METHOD_CP2102N_RTS = 1;
  const PTT_METHOD_CM108_HID   = 2;
  const PTT_METHOD_AIOC_CDC_DTR = 3;
  const PTT_METHOD_VOX          = 4;

  const androidPttOptions = [
    { value: PTT_METHOD_CP2102N_RTS,  label: 'CP2102N RTS (Digirig)' },
    { value: PTT_METHOD_AIOC_CDC_DTR, label: 'CDC-ACM DTR (AIOC)' },
    { value: PTT_METHOD_CM108_HID,    label: 'CM108 HID' },
    { value: PTT_METHOD_VOX,          label: 'VOX (no PTT wire)' },
  ];

  // USB role strings returned by GraywolfWebInterface.listUsbDevices()
  // keyed by PTT method int.
  const PTT_METHOD_USB_ROLE = {
    [PTT_METHOD_CP2102N_RTS]:  'CP2102N',
    [PTT_METHOD_AIOC_CDC_DTR]: 'AIOC',
    [PTT_METHOD_CM108_HID]:    'CM108',
  };

  // Android PTT form state (only used when Platform.kind === 'android')
  let androidPttMethod = $state(PTT_METHOD_CP2102N_RTS);
  let pttHeld = $state(false);
  let pttBusy = $state(false);
  let pttHeartbeatInterval = null;

  // USB hardware status state
  let usbDevice = $state(null);
  let usbStatusLoading = $state(false);
  let usbPollInterval = null;

  // Cleans up PTT hold state (called on release, cancel, unmount).
  function clearPttHold() {
    if (pttHeartbeatInterval !== null) {
      clearInterval(pttHeartbeatInterval);
      pttHeartbeatInterval = null;
    }
    pttHeld = false;
  }

  async function startTestPtt() {
    if (pttBusy || !editing?.id) return;
    pttHeld = true;
    pttBusy = true;
    try {
      await postChannelPtt(editing.id, true);
    } catch (err) {
      console.error('Test PTT key failed:', err);
      clearPttHold();
      pttBusy = false;
      return;
    }
    // Heartbeat: re-send keyed=true every 2s to keep Go-side watchdog alive.
    pttHeartbeatInterval = setInterval(async () => {
      try {
        await postChannelPtt(editing.id, true);
      } catch (err) {
        console.error('Test PTT heartbeat failed:', err);
        clearPttHold();
        pttBusy = false;
      }
    }, 2000);
    pttBusy = false;
  }

  async function stopTestPtt() {
    if (!pttHeld) return;
    clearPttHold();
    pttBusy = true;
    try {
      await postChannelPtt(editing.id, false);
    } catch (err) {
      console.error('Test PTT unkey failed:', err);
    } finally {
      pttBusy = false;
    }
  }

  // Poll USB device status via the Android JS bridge.
  function startUsbPoll() {
    if (usbPollInterval !== null) return;
    usbStatusLoading = true;
    const poll = () => {
      try {
        const raw = globalThis.GraywolfWebInterface?.listUsbDevices?.();
        const devices = raw ? JSON.parse(raw) : [];
        const role = PTT_METHOD_USB_ROLE[androidPttMethod];
        usbDevice = role ? (devices.find(d => d.role === role) || null) : null;
      } catch {
        usbDevice = null;
      }
      usbStatusLoading = false;
    };
    poll();
    usbPollInterval = setInterval(poll, 2000);
  }

  function stopUsbPoll() {
    if (usbPollInterval !== null) {
      clearInterval(usbPollInterval);
      usbPollInterval = null;
    }
    usbDevice = null;
    usbStatusLoading = false;
  }

  // Request USB permission via the Android JS bridge.
  // Registers a per-call callback on window.__usbResult so the Kotlin
  // side can fire it via evaluateJavascript("__usbResult(id, granted)").
  function requestGrant() {
    if (!usbDevice) return;
    // Prefix guarantees a non-empty alphanumeric id even if Math.random()
    // returns 0 (slice(2) of "0" is ""), which T9's Kotlin validator rejects.
    const callbackId = 'cb' + Math.random().toString(36).slice(2);
    // __usbResult is the global dispatcher Kotlin evaluateJavascript calls.
    if (!globalThis.__usbResult) {
      globalThis.__usbResult = (id, granted) => {
        const cb = globalThis.__usbCallbacks?.[id];
        if (cb) cb(granted);
        delete globalThis.__usbCallbacks?.[id];
      };
      globalThis.__usbCallbacks = {};
    }
    globalThis.__usbCallbacks[callbackId] = (granted) => {
      if (granted) {
        usbStatusLoading = true;
        // Re-poll immediately to refresh permission state.
        try {
          const raw = globalThis.GraywolfWebInterface?.listUsbDevices?.();
          const devices = raw ? JSON.parse(raw) : [];
          const role = PTT_METHOD_USB_ROLE[androidPttMethod];
          usbDevice = role ? (devices.find(d => d.role === role) || null) : null;
        } catch {
          usbDevice = null;
        }
        usbStatusLoading = false;
      }
    };
    try {
      globalThis.GraywolfWebInterface?.requestUsbPermission?.(
        usbDevice.vid,
        usbDevice.pid,
        callbackId,
      );
    } catch (err) {
      console.error('requestUsbPermission failed:', err);
    }
  }

  // When the edit modal opens for an Android channel, populate
  // androidPttMethod from the stored ptt config if present.
  function loadAndroidPttMethod(row) {
    if (Platform.kind !== 'android') return;
    // The ptt config is returned on the channel object as row.ptt.
    // If method === 'android', gpio_pin carries the method int.
    if (row.ptt?.method === 'android' && row.ptt?.gpio_pin) {
      androidPttMethod = row.ptt.gpio_pin;
    } else {
      androidPttMethod = PTT_METHOD_CP2102N_RTS;
    }
    startUsbPoll();
  }

  onMount(async () => {
    startChannelsStore();
    await Promise.all([loadChannels(), loadDevices(), loadTxTimings()]);
  });

  onDestroy(() => {
    clearPttHold();
    stopUsbPoll();
    // Tear down the global USB-grant dispatcher so a late callback after
    // unmount can't fire into a dead component.
    if (globalThis.__usbResult) {
      delete globalThis.__usbResult;
      delete globalThis.__usbCallbacks;
    }
  });

  // Legacy name; delegates to the shared store so every caller gets
  // the same refresh semantics (including pickers on other tabs).
  async function loadChannels() {
    await refreshChannels();
  }

  async function loadDevices() {
    audioDevices = await api.get('/audio-devices') || [];
  }

  async function loadTxTimings() {
    const list = await api.get('/tx-timing') || [];
    const map = {};
    for (const t of list) map[t.channel] = t;
    txTimings = map;
  }

  function openCreate() {
    editing = null;
    stopUsbPoll();
    clearPttHold();
    const defaultInput = inputDevices.length > 0 ? String(inputDevices[0].id) : '0';
    form = { ...blankForm(), input_device_id: defaultInput };
    errors = {};
    if (Platform.kind === 'android') {
      androidPttMethod = PTT_METHOD_CP2102N_RTS;
      startUsbPoll();
    }
    modalOpen = true;
  }

  async function openEdit(row) {
    editing = row;
    stopUsbPoll();
    clearPttHold();
    // Phase 2: input_device_id is nullable on the wire. Null means
    // KISS-TNC-only; any non-null value means modem-backed. The
    // segmented picker is read-only on edit (D11) — the "Convert…"
    // link below the badge is the only way to flip it.
    form = rowToForm(row, txTimings[row.id]);
    errors = {};
    loadAndroidPttMethod(row);
    modalOpen = true;
  }

  function validate() {
    errors = validateForm(form);
    return Object.keys(errors).length === 0;
  }

  async function handleSave() {
    if (!validate()) return;
    const data = formToPayload(form);
    await persistSave(data, { force: false });
  }

  // persistSave runs the actual PUT/POST + follow-up tx-timing save.
  // Factored out of handleSave so the Phase 3 409-force retry path
  // can reuse it without duplicating the tx-timing / modal-close /
  // reload dance. `force` adds ?force=true to the PUT query when
  // true; backend treats that as "I know this breaks referrers,
  // proceed anyway" (Phase 1 handoff).
  async function persistSave(data, { force }) {
    try {
      let channelId;
      if (editing) {
        const path = force
          ? `/channels/${editing.id}?force=true`
          : `/channels/${editing.id}`;
        await api.put(path, data);
        channelId = editing.id;
        toasts.success('Channel updated');
      } else {
        const created = await api.post('/channels', data);
        channelId = created.id;
        toasts.success('Channel created');
      }

      // Save TX timing if this is a TX-capable channel
      if (isTxEnabled && channelId) {
        const timingData = {
          channel: channelId,
          tx_delay_ms: parseInt(form.tx_delay_ms, 10),
          tx_tail_ms: parseInt(form.tx_tail_ms, 10),
          slot_ms: parseInt(form.slot_ms, 10),
          persist: parseInt(form.persist, 10),
          full_dup: form.full_dup,
        };
        await api.put(`/tx-timing/${channelId}`, timingData);
      }

      // On Android, persist the PTT method via POST /api/ptt.
      // Data shape: method='android', gpio_pin=method_int (1–4 per Appendix B).
      // The Go modembridge reads gpio_pin to determine which USB transport
      // to invoke in UsbPttAdapter (T7). This reuses the existing PttConfig
      // row's gpio_pin field as a method-int carrier rather than adding a
      // new schema column — the semantics are different from the desktop
      // gpio-line use but the field is otherwise unused on Android channels.
      if (Platform.kind === 'android' && channelId) {
        await api.post('/ptt', {
          channel_id: channelId,
          method: 'android',
          gpio_pin: androidPttMethod,
        });
      }

      modalOpen = false;
      stopUsbPoll();
      await Promise.all([loadChannels(), loadTxTimings()]);
    } catch (err) {
      // Phase 3 -- PUT 409 with referrers means the mutation would
      // break active config. Reuse the DELETE-cascade referrer-
      // grouping UI (channelReferrers.js) for consistency; the only
      // difference is the copy and the action (force vs cascade).
      // POST / non-409 paths fall through to the toast.
      if (
        editing &&
        !force &&
        err instanceof ApiError &&
        err.status === 409 &&
        Array.isArray(err.body?.referrers)
      ) {
        putReferrers = err.body.referrers;
        putPendingPayload = data;
        putPendingId = editing.id;
        putServerError = err.body?.error || err.message || '';
        putConfirmOpen = true;
        return;
      }
      toasts.error(err.message);
    }
  }

  // Called from the confirm dialog's Action button when the
  // operator acknowledges the referrer list and chooses to proceed.
  async function confirmForcePut() {
    if (!putPendingPayload || !putPendingId) return;
    const data = putPendingPayload;
    putInFlight = true;
    try {
      // editing can get cleared by other code paths; re-affirm it
      // from the id we captured when the 409 landed so the retry
      // routes to the correct row.
      const targetId = putPendingId;
      if (editing?.id !== targetId) {
        editing = channels.find((c) => c.id === targetId) || editing;
      }
      await persistSave(data, { force: true });
    } finally {
      putInFlight = false;
      putConfirmOpen = false;
      putReferrers = [];
      putPendingPayload = null;
      putPendingId = null;
      putServerError = '';
    }
  }

  // Cancel path: drop the pending payload and leave the edit modal
  // as-is. The operator's form state is preserved so they can
  // adjust the channel config and try again.
  function cancelForcePut() {
    putConfirmOpen = false;
    putReferrers = [];
    putPendingPayload = null;
    putPendingId = null;
    putServerError = '';
  }

  // Phase 5 two-step delete flow (D12).
  //
  // Click "Delete" → requestDelete(row):
  //   1. Fetch /api/channels/{id}/referrers.
  //   2. Empty list: skip the impact dialog; open the typed-name
  //      confirm dialog with cascade=false path.
  //   3. Non-empty list: open the impact dialog first. From there the
  //      operator clicks "Remove references…" to advance to the
  //      typed-name confirm dialog with cascade=true.
  //
  // Either way, the final Delete button is enabled only when the
  // operator types the channel's exact name. On confirm we call
  // DELETE with or without ?cascade=true depending on the path.
  async function requestDelete(row) {
    deleteTarget = row;
    deleteNameInput = '';
    deleteReferrers = [];
    try {
      const resp = await api.get(`/channels/${row.id}/referrers`);
      const refs = Array.isArray(resp?.referrers) ? resp.referrers : [];
      deleteReferrers = refs;
      if (refs.length === 0) {
        // Unreferenced — go straight to the typed-name confirm.
        deleteImpactOpen = false;
        deleteConfirmOpen = true;
      } else {
        deleteImpactOpen = true;
        deleteConfirmOpen = false;
      }
    } catch (err) {
      toasts.error(err.message);
      deleteTarget = null;
    }
  }

  function proceedToConfirm() {
    deleteImpactOpen = false;
    deleteConfirmOpen = true;
    deleteNameInput = '';
  }

  function cancelDelete() {
    deleteImpactOpen = false;
    deleteConfirmOpen = false;
    deleteTarget = null;
    deleteReferrers = [];
    deleteNameInput = '';
  }

  async function executeDelete() {
    if (!deleteTarget || !deleteNameMatches) return;
    const cascade = deleteReferrers.length > 0;
    const id = deleteTarget.id;
    deleteInFlight = true;
    try {
      const path = cascade ? `/channels/${id}?cascade=true` : `/channels/${id}`;
      await api.delete(path);
      toasts.success(cascade
        ? `Channel deleted along with ${deleteTotal} reference${deleteTotal === 1 ? '' : 's'}`
        : 'Channel deleted');
      await Promise.all([loadChannels(), loadTxTimings()]);
      deleteImpactOpen = false;
      deleteConfirmOpen = false;
      deleteTarget = null;
      deleteReferrers = [];
      deleteNameInput = '';
    } catch (err) {
      // A 409 here would mean a race (referrers appeared between our
      // GET and DELETE). Surface the same error channel; the impact
      // dialog route will naturally pick them up on the next click.
      if (err instanceof ApiError && err.status === 409 && Array.isArray(err.body?.referrers)) {
        deleteReferrers = err.body.referrers;
        deleteConfirmOpen = false;
        deleteImpactOpen = true;
        toasts.error('New references appeared — review and try again');
      } else {
        toasts.error(err.message);
      }
    } finally {
      deleteInFlight = false;
    }
  }
</script>

<PageHeader title="Channels" subtitle="Radio channel configuration">
  <Button variant="primary" onclick={openCreate}>+ Add Channel</Button>
</PageHeader>

{#if channels.length === 0}
  <div class="empty-state">
    No channels configured. Add a channel to start decoding RF packets.
    <br />
    <span class="empty-state-hint">
      Running an APRS-IS-only station? You don't need a channel — set your
      <a href="#/callsign">station callsign</a>, then enable the
      <a href="#/igate">iGate</a>. Messages will route over APRS-IS automatically.
    </span>
  </div>
{:else}
  <div class="channel-grid">
    {#each channels as ch}
      <ChannelRow
        channel={ch}
        txTiming={txTimings[ch.id]}
        {audioDevices}
        onEdit={openEdit}
        onDelete={requestDelete}
      />
    {/each}
  </div>
{/if}

<!-- Add/Edit modal -->
<div class="wide-modal">
<Modal bind:open={modalOpen} title={editing ? 'Edit Channel' : 'New Channel'}>
  <!-- Channel type picker (D11). Segmented on create; read-only badge
       on edit. -->
  <div class="channel-type-row">
    <span class="channel-type-label" id="channel-type-label">Channel type</span>
    {#if editing}
      <span class="channel-type-badge">
        {#if form.channel_type === 'modem'}Modem-backed{:else}KISS-TNC only{/if}
      </span>
    {:else}
      <div class="segmented" role="radiogroup" aria-labelledby="channel-type-label">
        <button type="button"
                role="radio"
                aria-checked={form.channel_type === 'modem'}
                class="segment"
                class:active={form.channel_type === 'modem'}
                onclick={() => form.channel_type = 'modem'}>
          Modem-backed
        </button>
        <button type="button"
                role="radio"
                aria-checked={form.channel_type === 'kiss-tnc'}
                class="segment"
                class:active={form.channel_type === 'kiss-tnc'}
                onclick={() => form.channel_type = 'kiss-tnc'}>
          KISS-TNC only
        </button>
      </div>
    {/if}
  </div>

  <div class="form-grid-2">
    <FormField label="Name" error={errors.name} id="ch-name">
      <Input id="ch-name" bind:value={form.name} placeholder="VHF APRS" />
    </FormField>
    {#if isModemType}
      <FormField label="Modem Type" id="ch-modem">
        <Select id="ch-modem" bind:value={form.modem_type} options={modemOptions} />
      </FormField>
    {/if}
  </div>

  {#if Platform.kind === 'android' && isModemType}
    <!-- Android PTT section: method picker, Test PTT toggle, USB status,
         audio routing. Gated on platform at runtime — desktop is unaffected. -->
    <div class="android-ptt-section">
      <h4 class="section-label">PTT (Android)</h4>
      <div class="android-ptt-row">
        <FormField label="PTT method" id="ch-android-ptt">
          <Select
            id="ch-android-ptt"
            bind:value={androidPttMethod}
            options={androidPttOptions}
          />
        </FormField>
        <button
          type="button"
          class="test-ptt-btn"
          class:ptt-held={pttHeld}
          onpointerdown={startTestPtt}
          onpointerup={stopTestPtt}
          onpointercancel={stopTestPtt}
          onpointerleave={stopTestPtt}
          disabled={!editing?.id || pttBusy}
          aria-label={pttHeld ? 'PTT keyed — release to unkey' : 'Press and hold to key transmitter'}
        >
          {pttHeld ? 'KEYED' : '⚡ Test PTT'}
        </button>
      </div>

      <div class="usb-status">
        <span class="usb-status-label">USB hardware:</span>
        {#if usbStatusLoading}
          <span class="usb-status-value">…</span>
        {:else if usbDevice}
          <span class="usb-status-value">
            {usbDevice.name}
            ({usbDevice.permission_granted ? 'Granted ✓' : 'Not granted'})
          </span>
          {#if !usbDevice.permission_granted}
            <button type="button" class="grant-btn" onclick={requestGrant}>Grant access</button>
          {/if}
        {:else}
          <span class="usb-status-value usb-none">none detected</span>
        {/if}
      </div>

      <div class="audio-routing">
        <span class="usb-status-label">Audio routing:</span>
        <!-- TODO: future ticket to pull from a status endpoint once available -->
        <span class="usb-status-value">USB audio (auto)</span>
      </div>
    </div>
  {/if}

  <FormField
    label="Mode"
    hint="APRS only: beacon, digipeater, iGate, and messages may transmit. Packet only: AX.25 connected-mode terminal sessions only; APRS subsystems are blocked. APRS + Packet: both, on a shared channel."
    id="ch-mode"
  >
    <Select
      id="ch-mode"
      bind:value={form.mode}
      aria-label="Channel mode"
      options={[
        { value: 'aprs', label: 'APRS only' },
        { value: 'packet', label: 'Packet only' },
        { value: 'aprs+packet', label: 'APRS + Packet' },
      ]}
    />
  </FormField>

  {#if isModemType}
    <div class="form-grid-4">
      <FormField label="Input Device" error={errors.input_device_id} id="ch-indev">
        <Select id="ch-indev" bind:value={form.input_device_id} options={inputDeviceOptions} />
      </FormField>
      <FormField label="Input Channel" id="ch-inch">
        <Select id="ch-inch" bind:value={form.input_channel} options={channelOptions} />
      </FormField>
      <FormField label="Output Device" id="ch-outdev">
        <Select id="ch-outdev" bind:value={form.output_device_id} options={outputDeviceOptions} />
      </FormField>
      {#if isTxEnabled}
        <FormField label="Output Channel" id="ch-outch">
          <Select id="ch-outch" bind:value={form.output_channel} options={channelOptions} />
        </FormField>
      {/if}
    </div>
    <div class="form-grid-3">
      <FormField label="Bit Rate" id="ch-baud">
        <Input id="ch-baud" bind:value={form.bit_rate} type="number" placeholder="1200" />
      </FormField>
      <FormField label="Mark Freq (Hz)" id="ch-mark">
        <Input id="ch-mark" bind:value={form.mark_freq} type="number" placeholder="1200" />
      </FormField>
      <FormField label="Space Freq (Hz)" id="ch-space">
        <Input id="ch-space" bind:value={form.space_freq} type="number" placeholder="2200" />
      </FormField>
    </div>

    {#if isTxEnabled}
      <div class="tx-timing-section">
        <h4 class="section-label">Transmit Timing</h4>
        <div class="form-grid-4">
          <FormField label="TX Delay (ms)" id="ch-txd"
            hint="Key-up time before sending. 300ms typical.">
            <Input id="ch-txd" bind:value={form.tx_delay_ms} type="number" placeholder="300" />
          </FormField>
          <FormField label="TX Tail (ms)" id="ch-txt"
            hint="Hold time after last byte. 100ms typical.">
            <Input id="ch-txt" bind:value={form.tx_tail_ms} type="number" placeholder="100" />
          </FormField>
          <FormField label="Slot Time (ms)" id="ch-slot"
            hint="CSMA listen interval. 100ms is standard.">
            <Input id="ch-slot" bind:value={form.slot_ms} type="number" placeholder="100" />
          </FormField>
          <FormField label="Persistence (0-255)" id="ch-persist" error={errors.persist}
            hint="TX probability = (val+1)/256. 63 ≈ 25%.">
            <Input id="ch-persist" bind:value={form.persist} type="number" placeholder="63" />
          </FormField>
        </div>
        <Toggle bind:checked={form.full_dup} label="Full Duplex" />
      </div>
    {/if}
  {:else}
    <div class="kiss-only-explainer">
      This channel is serviced by a KISS TNC interface (configured on
      the <a href="#/kiss">KISS page</a>). No audio device, modem, or
      CSMA timing is required — frames route through the attached
      KISS-TNC backend.
    </div>
  {/if}

  <div class="modal-actions">
    <Button onclick={() => { clearPttHold(); stopUsbPoll(); modalOpen = false; }}>Cancel</Button>
    <Button variant="primary" onclick={handleSave}>{editing ? 'Save' : 'Create'}</Button>
  </div>
</Modal>
</div>

<!-- Phase 5 two-step delete: stage 1 = impact dialog (only when the
     channel has referrers). Lists what the cascade will do to each
     dependent row, grouped by type, so the operator has an informed
     sense of scope before hitting the typed-name gate. -->
<AlertDialog bind:open={deleteImpactOpen}>
  <AlertDialog.Content>
    <AlertDialog.Title>Delete channel {deleteTarget?.name ?? ''}?</AlertDialog.Title>
    <AlertDialog.Description>
      This channel has {deleteTotal} reference{deleteTotal === 1 ? '' : 's'}. Deleting it will affect:
    </AlertDialog.Description>
    <ul class="referrer-groups">
      {#each deleteGroups as g (g.type)}
        <li>
          <strong>{g.items.length} {g.label}</strong>{#if g.action}<span class="referrer-action"> — {g.action}</span>{/if}{#if g.items.some((i) => i.name)}:
            <span class="referrer-items">
              {#each g.items as item, idx (item.id)}{idx > 0 ? ', ' : ''}{item.name || `#${item.id}`}{/each}
            </span>
          {/if}
        </li>
      {/each}
    </ul>
    <div class="modal-footer">
      <AlertDialog.Cancel onclick={cancelDelete}>Cancel</AlertDialog.Cancel>
      <AlertDialog.Action class="secondary-action" onclick={proceedToConfirm}>
        Remove references…
      </AlertDialog.Action>
    </div>
  </AlertDialog.Content>
</AlertDialog>

<!-- Phase 5 two-step delete: stage 2 = typed-name confirm. Fires for
     unreferenced channels directly (no stage 1) and for referenced
     channels after the operator clicks through the impact dialog.
     The delete button only enables when the typed name matches exactly. -->
<AlertDialog bind:open={deleteConfirmOpen}>
  <AlertDialog.Content>
    <AlertDialog.Title>
      {#if deleteReferrers.length > 0}
        Delete channel and {deleteTotal} reference{deleteTotal === 1 ? '' : 's'}
      {:else}
        Delete channel {deleteTarget?.name ?? ''}?
      {/if}
    </AlertDialog.Title>
    <AlertDialog.Description>
      This cannot be undone. To confirm, type the channel name exactly:
      <strong>{deleteTarget?.name ?? ''}</strong>
    </AlertDialog.Description>
    <label class="confirm-label">
      Channel name
      <input
        type="text"
        class="confirm-input"
        bind:value={deleteNameInput}
        autocomplete="off"
        aria-label={`Type ${deleteTarget?.name ?? ''} to confirm delete`}
      />
    </label>
    <div class="modal-footer">
      <AlertDialog.Cancel onclick={cancelDelete}>Cancel</AlertDialog.Cancel>
      <AlertDialog.Action
        class="danger-action"
        onclick={executeDelete}
        disabled={!deleteNameMatches || deleteInFlight}
      >
        {#if deleteReferrers.length > 0}
          Delete channel and {deleteTotal} reference{deleteTotal === 1 ? '' : 's'}
        {:else}
          Delete channel
        {/if}
      </AlertDialog.Action>
    </div>
  </AlertDialog.Content>
</AlertDialog>

<!-- Phase 3 -- channel PUT 409 "force" confirmation. Mirrors the
     stage-1 delete impact dialog above (same AlertDialog shape, same
     groupReferrers() rendering) but the Action retries the PUT with
     ?force=true instead of cascading a delete. No typed-name gate:
     a broken-referrer PUT is recoverable by editing again. -->
<AlertDialog bind:open={putConfirmOpen}>
  <AlertDialog.Content>
    <AlertDialog.Title>Update channel and break references?</AlertDialog.Title>
    <AlertDialog.Description>
      This channel update would break the following active config.
      {#if putServerError}
        <span class="put-error-reason">Reason: {putServerError}</span>
      {/if}
    </AlertDialog.Description>
    <ul class="referrer-groups">
      {#each putGroups as g (g.type)}
        <li>
          <strong>{g.items.length} {g.label}</strong>{#if g.items.some((i) => i.name)}:
            <span class="referrer-items">
              {#each g.items as item, idx (item.id)}{idx > 0 ? ', ' : ''}{item.name || `#${item.id}`}{/each}
            </span>
          {/if}
        </li>
      {/each}
    </ul>
    <p class="put-force-note">
      Saving will apply the change anyway. The referrers listed above
      will remain in the database but may fail to transmit until you
      fix them on their respective pages.
    </p>
    <div class="modal-footer">
      <AlertDialog.Cancel onclick={cancelForcePut}>Cancel</AlertDialog.Cancel>
      <AlertDialog.Action
        class="danger-action"
        onclick={confirmForcePut}
        disabled={putInFlight}
      >
        Save channel and break {putTotal} reference{putTotal === 1 ? '' : 's'}
      </AlertDialog.Action>
    </div>
  </AlertDialog.Content>
</AlertDialog>

<style>
  .empty-state {
    text-align: center;
    color: var(--text-muted);
    padding: 32px;
    border: 1px dashed var(--border-color);
    border-radius: var(--radius);
  }
  .empty-state-hint {
    display: inline-block;
    margin-top: 8px;
    font-size: 13px;
    color: var(--text-muted);
  }
  .empty-state-hint a {
    color: var(--color-primary);
    text-decoration: underline;
  }

  .channel-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
    gap: 12px;
  }

  /* Wider modal for channel editor */
  .wide-modal :global(.modal) {
    width: min(860px, 94vw);
  }
  .wide-modal :global(.modal-body) {
    overflow-y: auto;
  }
  .form-grid-2 {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 0 16px;
  }
  .form-grid-3 {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 0 16px;
  }
  .form-grid-4 {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 0 16px;
  }

  /* TX Timing section in modal */
  .tx-timing-section {
    margin-top: 12px;
    padding-top: 12px;
    border-top: 1px solid var(--border-color);
  }
  .section-label {
    margin: 0 0 6px 0;
    font-size: 15px;
    font-weight: 600;
  }

  /* Android PTT section */
  .android-ptt-section {
    margin-top: 12px;
    padding-top: 12px;
    border-top: 1px solid var(--border-color);
  }
  .android-ptt-row {
    display: flex;
    align-items: flex-end;
    gap: 10px;
    flex-wrap: wrap;
  }
  .android-ptt-row :global(.form-field) {
    flex: 1;
    min-width: 180px;
  }
  .test-ptt-btn {
    padding: 8px 14px;
    border: 2px solid var(--border-color);
    border-radius: 6px;
    background: var(--bg-surface, #fff);
    font-size: 14px;
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
    margin-bottom: 1px; /* align with Select baseline */
    touch-action: none; /* prevent scroll on pointer hold */
  }
  .test-ptt-btn:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
  .test-ptt-btn.ptt-held {
    border-color: #e53e3e;
    color: #e53e3e;
    background: #fff5f5;
  }
  .usb-status,
  .audio-routing {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 6px;
    font-size: 13px;
  }
  .usb-status-label {
    font-weight: 600;
    color: var(--text-secondary, #555);
  }
  .usb-status-value {
    color: var(--text-primary, #111);
  }
  .usb-none {
    color: var(--text-secondary, #888);
    font-style: italic;
  }
  .grant-btn {
    padding: 2px 8px;
    border: 1px solid var(--border-color);
    border-radius: 4px;
    background: transparent;
    font-size: 12px;
    cursor: pointer;
  }

  .modal-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    margin-top: 16px;
  }
  .modal-footer {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    padding: 1.25rem 1.5rem 1.5rem;
  }
  :global(.danger-action) {
    background: var(--color-danger) !important;
    color: white !important;
  }

  /* D11 channel-type segmented control + edit-time read-only badge. */
  .channel-type-row {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-bottom: 12px;
    flex-wrap: wrap;
  }
  .channel-type-label {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-secondary);
    min-width: 110px;
  }
  .segmented {
    display: inline-flex;
    border: 1px solid var(--border-color);
    border-radius: var(--radius);
    overflow: hidden;
  }
  .segment {
    padding: 8px 14px;
    min-height: 40px;
    background: var(--bg-secondary);
    border: none;
    border-right: 1px solid var(--border-color);
    color: var(--text-primary);
    font: inherit;
    cursor: pointer;
  }
  .segment:last-child {
    border-right: none;
  }
  .segment.active {
    background: var(--color-info-muted, rgba(56, 139, 253, 0.15));
    color: var(--color-info, #388bfd);
    font-weight: 600;
  }
  .segment:focus-visible {
    outline: 2px solid var(--color-info, #388bfd);
    outline-offset: -2px;
  }

  .channel-type-badge {
    display: inline-block;
    padding: 4px 10px;
    border-radius: var(--radius);
    background: var(--bg-tertiary);
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary);
  }
  .kiss-only-explainer {
    padding: 10px 12px;
    background: var(--bg-tertiary);
    border-left: 3px solid var(--color-info, #388bfd);
    border-radius: var(--radius);
    font-size: 13px;
    color: var(--text-secondary);
    margin-bottom: 8px;
  }
  .kiss-only-explainer a {
    color: var(--color-info, #388bfd);
  }

  /* Phase 5 two-step delete flow */
  .referrer-groups {
    margin: 12px 1.5rem 0 1.5rem;
    padding: 10px 12px;
    background: var(--bg-tertiary);
    border-radius: var(--radius);
    list-style: disc inside;
    font-size: 13px;
    color: var(--text-primary);
    line-height: 1.6;
  }
  .referrer-groups li + li {
    margin-top: 2px;
  }
  .referrer-action {
    color: var(--text-secondary);
    font-style: italic;
  }
  .referrer-items {
    color: var(--text-secondary);
  }
  .confirm-label {
    display: block;
    margin: 12px 1.5rem 0 1.5rem;
    font-size: 13px;
    color: var(--text-secondary);
  }
  .confirm-input {
    display: block;
    width: 100%;
    margin-top: 4px;
    padding: 8px 10px;
    min-height: 40px;
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius);
    color: var(--text-primary);
    font: inherit;
  }
  .confirm-input:focus-visible {
    outline: 2px solid var(--color-info, #388bfd);
    outline-offset: -2px;
  }
  :global(.secondary-action) {
    background: var(--bg-tertiary) !important;
    color: var(--text-primary) !important;
  }

  /* Phase 3 -- channel PUT 409 confirm dialog copy. Inline "Reason:"
     clause reflects the server's concrete explanation (e.g. "no
     output device configured") so the operator sees why the
     mutation breaks referrers without guessing. */
  .put-error-reason {
    display: block;
    margin-top: 6px;
    font-size: 13px;
    color: var(--color-danger, #f85149);
  }
  .put-force-note {
    margin: 12px 1.5rem 0 1.5rem;
    font-size: 13px;
    color: var(--text-secondary);
    line-height: 1.5;
  }
</style>
