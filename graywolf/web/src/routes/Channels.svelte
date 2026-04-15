<script>
  import { onMount } from 'svelte';
  import { Button, Input, Select, Badge, Toggle, AlertDialog } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import Modal from '../components/Modal.svelte';
  import FormField from '../components/FormField.svelte';

  let channels = $state([]);
  let audioDevices = $state([]);
  let txTimings = $state({});
  let modalOpen = $state(false);
  let editing = $state(null);
  let deleteTarget = $state(null);
  let deleteOpen = $state(false);
  let form = $state({
    name: '', input_device_id: '0', input_channel: '0',
    output_device_id: '0', output_channel: '0',
    modem_type: 'afsk', bit_rate: '1200', mark_freq: '1200', space_freq: '2200',
    tx_delay_ms: '300', tx_tail_ms: '100', slot_ms: '100', persist: '63', full_dup: false,
  });
  let errors = $state({});

  let isTxEnabled = $derived(form.output_device_id !== '0');

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
    await Promise.all([loadChannels(), loadDevices(), loadTxTimings()]);
  });

  async function loadChannels() {
    channels = await api.get('/channels') || [];
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
      name: '', input_device_id: defaultInput, input_channel: '0',
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
    form = {
      ...row,
      input_device_id: String(row.input_device_id),
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
    if (!form.input_device_id || form.input_device_id === '0') e.input_device_id = 'Required';
    if (isTxEnabled) {
      const p = parseInt(form.persist);
      if (isNaN(p) || p < 0 || p > 255) e.persist = 'Must be 0–255';
    }
    errors = e;
    return Object.keys(e).length === 0;
  }

  async function handleSave() {
    if (!validate()) return;
    const data = {
      ...form,
      input_device_id: parseInt(form.input_device_id, 10),
      input_channel: parseInt(form.input_channel, 10),
      output_device_id: parseInt(form.output_device_id, 10),
      output_channel: parseInt(form.output_channel, 10),
      bit_rate: parseInt(form.bit_rate, 10),
      mark_freq: parseInt(form.mark_freq, 10),
      space_freq: parseInt(form.space_freq, 10),
    };
    // Strip fields not in ChannelRequest DTO
    delete data.id;
    delete data.tx_delay_ms;
    delete data.tx_tail_ms;
    delete data.slot_ms;
    delete data.persist;
    delete data.full_dup;

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

  function confirmDelete(row) {
    deleteTarget = row;
    deleteOpen = true;
  }

  async function executeDelete() {
    if (!deleteTarget) return;
    try {
      await api.delete(`/channels/${deleteTarget.id}`);
      toasts.success('Channel deleted');
      await Promise.all([loadChannels(), loadTxTimings()]);
    } catch (err) {
      toasts.error(err.message);
    } finally {
      deleteOpen = false;
      deleteTarget = null;
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
      <div class="channel-card">
        <div class="channel-header">
          <span class="channel-name">{ch.name}</span>
          <div class="channel-badges">
            <Badge variant="default">{ch.modem_type.toUpperCase()}</Badge>
            {#if ch.output_device_id && ch.output_device_id !== 0}
              <Badge variant="success">RX/TX</Badge>
            {:else}
              <Badge variant="info">RX</Badge>
            {/if}
          </div>
        </div>

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

        <div class="channel-details">
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
        </div>

        <div class="channel-actions">
          <Button variant="ghost" onclick={() => openEdit(ch)}>Edit</Button>
          <Button variant="danger" onclick={() => confirmDelete(ch)}>Delete</Button>
        </div>
      </div>
    {/each}
  </div>
{/if}

<!-- Add/Edit modal -->
<div class="wide-modal">
<Modal bind:open={modalOpen} title={editing ? 'Edit Channel' : 'New Channel'}>
  <div class="form-grid-2">
    <FormField label="Name" error={errors.name} id="ch-name">
      <Input id="ch-name" bind:value={form.name} placeholder="VHF APRS" />
    </FormField>
    <FormField label="Modem Type" id="ch-modem">
      <Select id="ch-modem" bind:value={form.modem_type} options={modemOptions} />
    </FormField>
  </div>
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

  <div class="modal-actions">
    <Button onclick={() => modalOpen = false}>Cancel</Button>
    <Button variant="primary" onclick={handleSave}>{editing ? 'Save' : 'Create'}</Button>
  </div>
</Modal>
</div>

<!-- Delete confirmation -->
<AlertDialog bind:open={deleteOpen}>
  <AlertDialog.Content>
    <AlertDialog.Title>Delete Channel</AlertDialog.Title>
    <AlertDialog.Description>
      Are you sure you want to delete "{deleteTarget?.name}"? This cannot be undone.
    </AlertDialog.Description>
    <div class="modal-footer">
      <AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
      <AlertDialog.Action class="danger-action" onclick={executeDelete}>Delete</AlertDialog.Action>
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
</style>
