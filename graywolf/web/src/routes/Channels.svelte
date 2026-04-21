<script>
  import { onMount } from 'svelte';
  import { Button, Input, Select, Badge, Toggle, AlertDialog } from '@chrissnell/chonky-ui';
  import { api, ApiError } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import Modal from '../components/Modal.svelte';
  import FormField from '../components/FormField.svelte';
  import { channelsStore, start as startChannelsStore, invalidate as refreshChannels } from '../lib/stores/channels.svelte.js';
  import {
    healthGlyph,
    healthText,
    summaryLabel,
    ariaLabel as backingAriaLabel,
    tooltipText as backingTooltip,
    HEALTH_LIVE,
    HEALTH_DOWN,
  } from '../lib/channelBacking.js';
  import { groupReferrers, totalReferrers } from '../lib/channelReferrers.js';

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
  // channel_type is a UI-only enum that drives the segmented picker
  // (D11). It is NOT serialized; the wire shape is still
  // input_device_id (nullable). 'modem' keeps all existing behavior;
  // 'kiss-tnc' hides audio fields and sends input_device_id=null.
  let form = $state({
    name: '',
    channel_type: 'modem',
    input_device_id: '0', input_channel: '0',
    output_device_id: '0', output_channel: '0',
    modem_type: 'afsk', bit_rate: '1200', mark_freq: '1200', space_freq: '2200',
    tx_delay_ms: '300', tx_tail_ms: '100', slot_ms: '100', persist: '63', full_dup: false,
  });
  let errors = $state({});
  // Convert-type dialog (D11 edit flow). When the user clicks
  // "Convert…" on an edit form, we stage the target type here and
  // surface a confirmation explaining the delete+create semantics.
  let convertOpen = $state(false);
  let convertTargetType = $state(null);

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
    { value: 'gfsk', label: 'GFSK' },
    { value: 'psk', label: 'PSK' },
    { value: 'fsk', label: 'FSK' },
  ];

  const channelOptions = [
    { value: '0', label: '0 (Left/Mono)' },
    { value: '1', label: '1 (Right)' },
  ];

  const txTimingDefaults = {
    tx_delay_ms: '300', tx_tail_ms: '100', slot_ms: '100', persist: '63', full_dup: false,
  };

  onMount(async () => {
    startChannelsStore();
    await Promise.all([loadChannels(), loadDevices(), loadTxTimings()]);
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

  function deviceName(id) {
    if (!id || id === 0) return null;
    const d = audioDevices.find(d => d.id === id);
    return d ? d.name : `Device #${id}`;
  }

  function channelLabel(ch) {
    return ch === 0 ? 'Left/Mono' : ch === 1 ? 'Right' : `Ch ${ch}`;
  }

  function openCreate() {
    editing = null;
    const defaultInput = inputDevices.length > 0 ? String(inputDevices[0].id) : '0';
    form = {
      name: '',
      channel_type: 'modem',
      input_device_id: defaultInput, input_channel: '0',
      output_device_id: '0', output_channel: '0',
      modem_type: 'afsk', bit_rate: '1200', mark_freq: '1200', space_freq: '2200',
      ...txTimingDefaults,
    };
    errors = {};
    modalOpen = true;
  }

  async function openEdit(row) {
    editing = row;
    const timing = txTimings[row.id] || {};
    // Phase 2: input_device_id is nullable on the wire. Null means
    // KISS-TNC-only; any non-null value means modem-backed. The
    // segmented picker is read-only on edit (D11) — the "Convert…"
    // link below the badge is the only way to flip it.
    const channelType = row.input_device_id == null ? 'kiss-tnc' : 'modem';
    form = {
      ...row,
      channel_type: channelType,
      input_device_id: row.input_device_id == null ? '0' : String(row.input_device_id),
      input_channel: String(row.input_channel),
      output_device_id: String(row.output_device_id),
      output_channel: String(row.output_channel),
      bit_rate: String(row.bit_rate),
      mark_freq: String(row.mark_freq),
      space_freq: String(row.space_freq),
      tx_delay_ms: String(timing.tx_delay_ms ?? 300),
      tx_tail_ms: String(timing.tx_tail_ms ?? 100),
      slot_ms: String(timing.slot_ms ?? 100),
      persist: String(timing.persist ?? 63),
      full_dup: timing.full_dup ?? false,
    };
    errors = {};
    modalOpen = true;
  }

  function validate() {
    const e = {};
    if (!form.name.trim()) e.name = 'Required';
    if (isModemType) {
      if (!form.input_device_id || form.input_device_id === '0') {
        e.input_device_id = 'Required';
      }
    }
    if (isTxEnabled) {
      const p = parseInt(form.persist);
      if (isNaN(p) || p < 0 || p > 255) e.persist = 'Must be 0–255';
    }
    errors = e;
    return Object.keys(e).length === 0;
  }

  // buildPayload shapes the form into the ChannelRequest DTO. KISS-TNC
  // channels send input_device_id: null and force the audio/output
  // fields to zero (the backend validator also enforces this, but we
  // match it here so the UI never submits a known-invalid row).
  function buildPayload() {
    const base = {
      name: form.name,
      modem_type: form.modem_type,
      bit_rate: parseInt(form.bit_rate, 10),
      mark_freq: parseInt(form.mark_freq, 10),
      space_freq: parseInt(form.space_freq, 10),
      input_channel: parseInt(form.input_channel, 10),
      output_channel: parseInt(form.output_channel, 10),
    };
    if (form.channel_type === 'kiss-tnc') {
      return {
        ...base,
        input_device_id: null,
        input_channel: 0,
        output_device_id: 0,
        output_channel: 0,
      };
    }
    return {
      ...base,
      input_device_id: parseInt(form.input_device_id, 10),
      output_device_id: parseInt(form.output_device_id, 10),
    };
  }

  async function handleSave() {
    if (!validate()) return;
    const data = buildPayload();
    try {
      let channelId;
      if (editing) {
        await api.put(`/channels/${editing.id}`, data);
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

      modalOpen = false;
      await Promise.all([loadChannels(), loadTxTimings()]);
    } catch (err) {
      toasts.error(err.message);
    }
  }

  // D11: edit-time type conversion is modeled as delete + create.
  // Dependent rows (beacons/digi rules/igate references) may become
  // orphaned; Phase 5 will add the proper cascade flow. We warn
  // explicitly so the operator doesn't discover that silently.
  function requestConvert(newType) {
    if (!editing) return;
    convertTargetType = newType;
    convertOpen = true;
  }

  async function executeConvert() {
    if (!editing || !convertTargetType) return;
    const originalId = editing.id;
    const targetType = convertTargetType;
    try {
      // Stage the new channel payload by flipping channel_type and
      // clearing audio fields as appropriate.
      const nextForm = {
        ...form,
        channel_type: targetType,
      };
      if (targetType === 'kiss-tnc') {
        nextForm.input_device_id = '0';
        nextForm.output_device_id = '0';
      } else {
        // Defaulting to the first available input device keeps the
        // user from landing on a form with no valid device
        // selection.
        const defaultInput = inputDevices.length > 0 ? String(inputDevices[0].id) : '0';
        nextForm.input_device_id = defaultInput;
      }
      form = nextForm;
      // Delete the original row; the user must then Save to create
      // the new-shape row. This is intentionally two-step so the
      // operator can cancel mid-flight without losing data.
      await api.delete(`/channels/${originalId}`);
      toasts.info('Channel deleted. Review the form and Save to create the converted channel.');
      // Flip the modal from edit mode to create mode so Save does a
      // POST rather than a PUT against the deleted id.
      editing = null;
      await loadChannels();
    } catch (err) {
      toasts.error(err.message);
    } finally {
      convertOpen = false;
      convertTargetType = null;
    }
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
  <div class="empty-state">No channels configured. Add a channel to start decoding packets.</div>
{:else}
  <div class="channel-grid">
    {#each channels as ch}
      {@const isKissOnly = ch.input_device_id == null}
      <div class="channel-card">
        <div class="channel-header">
          <span class="channel-name">{ch.name}</span>
          <div class="channel-badges">
            {#if isKissOnly}
              <Badge variant="info">KISS-TNC only</Badge>
            {:else}
              <Badge variant="default">{ch.modem_type.toUpperCase()}</Badge>
              {#if ch.output_device_id && ch.output_device_id !== 0}
                <Badge variant="success">RX/TX</Badge>
              {:else}
                <Badge variant="info">RX</Badge>
              {/if}
            {/if}
          </div>
        </div>

        {#if !isKissOnly}
          <div class="channel-devices">
            <div class="device-link">
              <span class="device-direction">RX</span>
              <div class="device-info">
                <span class="device-name-ref">{deviceName(ch.input_device_id) || '—'}</span>
                <span class="device-ch">{channelLabel(ch.input_channel)}</span>
              </div>
            </div>
            {#if ch.output_device_id && ch.output_device_id !== 0}
              <div class="device-link">
                <span class="device-direction tx">TX</span>
                <div class="device-info">
                  <span class="device-name-ref">{deviceName(ch.output_device_id)}</span>
                  <span class="device-ch">{channelLabel(ch.output_channel)}</span>
                </div>
              </div>
            {/if}
          </div>
        {:else}
          <div class="channel-kiss-only-note">
            Serviced by a KISS TNC interface (configured on the KISS page).
          </div>
        {/if}

        {#if ch.backing}
          {@const h = ch.backing.health}
          {@const glyphClass = h === HEALTH_LIVE ? 'live' : h === HEALTH_DOWN ? 'down' : 'unbound'}
          <div class="backing-row"
               aria-label={backingAriaLabel(ch)}
               title={backingTooltip(ch.backing)}>
            <span class="backing-label">Backing</span>
            <span class="backing-summary">
              <span class="glyph {glyphClass}" aria-hidden="true">{healthGlyph(h)}</span>
              <span class="backing-text">{summaryLabel(ch.backing)} · {healthText(h)}</span>
            </span>
          </div>
        {/if}

        <div class="channel-details">
          {#if !isKissOnly}
            <div class="detail-row">
              <span class="detail-label">Bit Rate</span>
              <span class="detail-value">{ch.bit_rate} bps</span>
            </div>
            <div class="detail-row">
              <span class="detail-label">Mark / Space</span>
              <span class="detail-value">{ch.mark_freq} / {ch.space_freq} Hz</span>
            </div>
            {#if ch.output_device_id && ch.output_device_id !== 0 && txTimings[ch.id]}
              {@const t = txTimings[ch.id]}
              <div class="detail-row">
                <span class="detail-label">TXD / Tail</span>
                <span class="detail-value">{t.tx_delay_ms} / {t.tx_tail_ms} ms</span>
              </div>
              <div class="detail-row">
                <span class="detail-label">CSMA</span>
                <span class="detail-value">p{t.persist} slot {t.slot_ms}ms{t.full_dup ? ' FDX' : ''}</span>
              </div>
            {/if}
          {/if}
        </div>

        <div class="channel-actions">
          <Button variant="ghost" onclick={() => openEdit(ch)}>Edit</Button>
          <Button variant="danger" onclick={() => requestDelete(ch)}>Delete</Button>
        </div>
      </div>
    {/each}
  </div>
{/if}

<!-- Add/Edit modal -->
<div class="wide-modal">
<Modal bind:open={modalOpen} title={editing ? 'Edit Channel' : 'New Channel'}>
  <!-- Channel type picker (D11). Segmented on create; read-only badge
       on edit with a "Convert…" affordance that opens a destructive
       confirmation. -->
  <div class="channel-type-row">
    <span class="channel-type-label" id="channel-type-label">Channel type</span>
    {#if editing}
      <span class="channel-type-badge">
        {#if form.channel_type === 'modem'}Modem-backed{:else}KISS-TNC only{/if}
      </span>
      <button type="button" class="convert-link"
              onclick={() => requestConvert(form.channel_type === 'modem' ? 'kiss-tnc' : 'modem')}>
        Convert to {form.channel_type === 'modem' ? 'KISS-TNC only' : 'Modem-backed'}…
      </button>
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
    <Button onclick={() => modalOpen = false}>Cancel</Button>
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

<!-- Convert-type confirmation (D11).
     Conversion is a delete + create, not an in-place rewrite, because
     the backend validator forbids mutating a channel's backing shape
     mid-flight; switching types invalidates dependent beacons / digi
     rules / igate references (Phase 5 will add the proper cascade). -->
<AlertDialog bind:open={convertOpen}>
  <AlertDialog.Content>
    <AlertDialog.Title>Convert channel type</AlertDialog.Title>
    <AlertDialog.Description>
      Converting this channel is equivalent to deleting and recreating
      it. All dependent beacons, digipeater rules, iGate references,
      and KISS interface bindings may become invalid and require
      manual fix-up. Continue?
    </AlertDialog.Description>
    <div class="modal-footer">
      <AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
      <AlertDialog.Action class="danger-action" onclick={executeConvert}>
        Delete and convert
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

  .channel-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
    gap: 12px;
  }

  .channel-card {
    display: flex;
    flex-direction: column;
    padding: 16px;
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius);
  }

  .channel-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 12px;
    gap: 8px;
  }
  .channel-name {
    font-weight: 600;
    font-size: 15px;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .channel-badges {
    display: flex;
    gap: 4px;
    flex-shrink: 0;
  }

  /* RX/TX device links */
  .channel-devices {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-bottom: 12px;
    padding: 10px;
    background: var(--bg-tertiary);
    border-radius: var(--radius);
  }
  .device-link {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .device-direction {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.5px;
    color: var(--color-info);
    background: var(--color-info-muted);
    padding: 2px 6px;
    border-radius: 3px;
    flex-shrink: 0;
    min-width: 26px;
    text-align: center;
  }
  .device-direction.tx {
    color: var(--color-success);
    background: var(--color-success-muted);
  }
  .device-info {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
    font-size: 13px;
  }
  .device-name-ref {
    color: var(--text-primary);
    font-weight: 500;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .device-ch {
    color: var(--text-secondary);
    font-size: 12px;
    flex-shrink: 0;
  }

  .channel-details {
    display: flex;
    flex-direction: column;
    gap: 6px;
    flex: 1;
  }
  .detail-row {
    display: flex;
    justify-content: space-between;
    font-size: 13px;
    gap: 12px;
  }
  .detail-label {
    color: var(--text-secondary);
    flex-shrink: 0;
  }
  .detail-value {
    font-family: var(--font-mono);
    color: var(--text-primary);
    text-align: right;
  }

  .channel-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    margin-top: 12px;
    padding-top: 12px;
    border-top: 1px solid var(--border-color);
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

  /* Backing summary row on each channel card. Kept deliberately muted
     so the primary RX/TX device info stays the visual focus; the
     backing line is for "where does a TX frame go" disambiguation. */
  .backing-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    margin-bottom: 10px;
    padding: 6px 10px;
    background: var(--bg-tertiary);
    border-radius: var(--radius);
    font-size: 12px;
    color: var(--text-secondary);
  }
  .backing-label {
    font-weight: 700;
    letter-spacing: 0.5px;
    text-transform: uppercase;
  }
  .backing-summary {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }
  .backing-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .glyph {
    display: inline-flex;
    width: 12px;
    height: 12px;
    line-height: 1;
    font-size: 12px;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  }
  .glyph.live {
    color: var(--color-success, #2ea043);
  }
  .glyph.down {
    color: var(--color-warning, #d4a72c);
  }
  .glyph.unbound {
    color: var(--text-muted, #888);
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
  .convert-link {
    background: none;
    border: none;
    color: var(--color-info, #388bfd);
    padding: 0;
    cursor: pointer;
    font: inherit;
    text-decoration: underline;
  }
  .convert-link:hover {
    text-decoration: none;
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

  .channel-kiss-only-note {
    padding: 10px;
    background: var(--bg-tertiary);
    border-radius: var(--radius);
    font-size: 13px;
    color: var(--text-secondary);
    margin-bottom: 12px;
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
</style>
