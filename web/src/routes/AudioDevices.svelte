<script>
  import { onMount } from 'svelte';
  import { Button, Input, Select, Badge, AlertDialog } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import Modal from '../components/Modal.svelte';
  import FormField from '../components/FormField.svelte';

  let devices = $state([]);
  let available = $state([]);
  let loadingAvail = $state(false);
  let modalOpen = $state(false);
  let editing = $state(null);
  let form = $state(emptyForm());
  let errors = $state({});
  let deleteTarget = $state(null);
  let deleteOpen = $state(false);

  function emptyForm() {
    return { name: '', device_path: '', sample_rate: '48000', channels: '1', source_type: 'soundcard', direction: 'input' };
  }

  onMount(loadDevices);

  async function loadDevices() {
    devices = await api.get('/audio-devices') || [];
  }

  async function refreshAvailable() {
    loadingAvail = true;
    try {
      available = await api.get('/audio-devices/available') || [];
      toasts.success(`Found ${available.length} audio device(s)`);
    } catch (err) {
      toasts.error(err.message);
    } finally {
      loadingAvail = false;
    }
  }

  function openCreate() {
    editing = null;
    form = emptyForm();
    errors = {};
    modalOpen = true;
  }

  function openEdit(row) {
    editing = row;
    form = { ...row, sample_rate: String(row.sample_rate), channels: String(row.channels) };
    errors = {};
    modalOpen = true;
  }

  function openCreateFromAvail(dev) {
    editing = null;
    form = {
      name: dev.name,
      device_path: dev.path,
      sample_rate: String(dev.sample_rates[dev.sample_rates.length - 1]),
      channels: String(dev.channels[0]),
      source_type: 'soundcard',
      direction: dev.is_input ? 'input' : 'output',
    };
    errors = {};
    modalOpen = true;
  }

  function handleModalClose() {
    editing = null;
    form = emptyForm();
    errors = {};
  }

  function validate() {
    const e = {};
    if (!form.name.trim()) e.name = 'Required';
    if (!form.device_path.trim()) e.device_path = 'Required';
    errors = e;
    return Object.keys(e).length === 0;
  }

  async function handleSave(e) {
    e.preventDefault();
    if (!validate()) return;
    const data = { ...form, sample_rate: parseInt(form.sample_rate), channels: parseInt(form.channels) };
    try {
      if (editing) {
        await api.put(`/audio-devices/${editing.id}`, data);
        toasts.success('Audio device updated');
      } else {
        await api.post('/audio-devices', data);
        toasts.success('Audio device added');
      }
      modalOpen = false;
      await loadDevices();
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
      await api.delete(`/audio-devices/${deleteTarget.id}`);
      toasts.success('Device deleted');
      await loadDevices();
    } catch (err) {
      toasts.error(err.message);
    } finally {
      deleteOpen = false;
      deleteTarget = null;
    }
  }

  let hasInput = $derived(devices.some(d => d.direction === 'input'));
  let hasOutput = $derived(devices.some(d => d.direction === 'output'));
  let inputDevices = $derived(devices.filter(d => d.direction === 'input'));
  let outputDevices = $derived(devices.filter(d => d.direction === 'output'));

  function truncatePath(p, max = 40) {
    if (!p || p.length <= max) return p || '—';
    return '...' + p.slice(-(max - 3));
  }
</script>

<PageHeader title="Audio Devices" subtitle="Sound card configuration">
  <Button onclick={refreshAvailable} disabled={loadingAvail}>
    {loadingAvail ? 'Scanning...' : 'Detect Devices'}
  </Button>
  <Button variant="primary" onclick={openCreate}>+ Add Device</Button>
</PageHeader>

<!-- Station readiness -->
<div class="readiness">
  <div class="readiness-item" class:ready={hasInput}>
    <div class="readiness-icon">{hasInput ? '●' : '○'}</div>
    <div class="readiness-info">
      <span class="readiness-label">Receive (RX)</span>
      {#if hasInput}
        <span class="readiness-detail">{inputDevices.length} input device{inputDevices.length !== 1 ? 's' : ''} configured</span>
      {:else}
        <span class="readiness-detail needs">Needs an input device (microphone / receiver audio)</span>
      {/if}
    </div>
  </div>
  <div class="readiness-item" class:ready={hasOutput}>
    <div class="readiness-icon">{hasOutput ? '●' : '○'}</div>
    <div class="readiness-info">
      <span class="readiness-label">Transmit (TX)</span>
      {#if hasOutput}
        <span class="readiness-detail">{outputDevices.length} output device{outputDevices.length !== 1 ? 's' : ''} configured — also requires PTT</span>
      {:else}
        <span class="readiness-detail needs">Needs an output device + PTT configuration</span>
      {/if}
    </div>
  </div>
</div>

<!-- Configured devices -->
<div class="section-label">Configured Devices</div>
{#if devices.length === 0}
  <div class="empty-state">No audio devices configured. Detect devices below or add one manually.</div>
{:else}
  <div class="device-grid">
    {#each devices as dev}
      <div class="device-card">
        <div class="device-header">
          <span class="device-name">{dev.name}</span>
          <div class="device-badges">
            <Badge variant={dev.direction === 'input' ? 'info' : 'success'}>
              {dev.direction === 'input' ? 'Input' : 'Output'}
            </Badge>
            <Badge variant="default">{dev.source_type}</Badge>
          </div>
        </div>
        <div class="device-details">
          <div class="detail-row">
            <span class="detail-label">Path</span>
            <span class="detail-value" title={dev.device_path}>{truncatePath(dev.device_path)}</span>
          </div>
          <div class="detail-row">
            <span class="detail-label">Sample Rate</span>
            <span class="detail-value">{dev.sample_rate} Hz</span>
          </div>
          <div class="detail-row">
            <span class="detail-label">Channels</span>
            <span class="detail-value">{dev.channels === 1 ? 'Mono' : 'Stereo'}</span>
          </div>
        </div>
        <div class="device-actions">
          <Button variant="ghost" onclick={() => openEdit(dev)}>Edit</Button>
          <Button variant="danger" onclick={() => confirmDelete(dev)}>Delete</Button>
        </div>
      </div>
    {/each}
  </div>
{/if}

<!-- Available devices from hardware scan -->
{#if available.length > 0}
  <div class="section-label" style="margin-top: 24px;">Detected Hardware</div>
  <p class="section-hint">Click a device to add it to your configuration.</p>
  <div class="avail-grid">
    {#each available as dev}
      <button class="avail-card" onclick={() => openCreateFromAvail(dev)}>
        <div class="avail-header">
          <strong class="avail-name">{dev.name}</strong>
          <Badge variant={dev.is_input ? 'info' : 'success'}>
            {dev.is_input ? 'Input' : 'Output'}
          </Badge>
        </div>
        {#if dev.host_api}
          <span class="avail-api">{dev.host_api}</span>
        {/if}
        <span class="avail-path" title={dev.path}>{truncatePath(dev.path, 50)}</span>
        <div class="avail-caps">
          <span>Rates: {dev.sample_rates.join(', ')} Hz</span>
          <span>Channels: {dev.channels.join(', ')}</span>
        </div>
        {#if dev.is_default}
          <Badge variant="success">System Default</Badge>
        {/if}
      </button>
    {/each}
  </div>
{/if}

<!-- Add/Edit modal -->
<Modal bind:open={modalOpen} title={editing ? 'Edit Audio Device' : 'Add Audio Device'} onClose={handleModalClose}>
  <form onsubmit={handleSave}>
    <FormField label="Name" error={errors.name} id="ad-name">
      <Input id="ad-name" bind:value={form.name} placeholder="USB Sound Card" />
    </FormField>
    <FormField label="Device Path" error={errors.device_path} id="ad-path">
      <Input id="ad-path" bind:value={form.device_path} placeholder="hw:0,0" />
    </FormField>
    <FormField label="Direction" id="ad-dir">
      <Select id="ad-dir" bind:value={form.direction} options={[
        { value: 'input', label: 'Input (Microphone / Receiver)' },
        { value: 'output', label: 'Output (Speaker / Transmitter)' },
      ]} />
    </FormField>
    <FormField label="Source Type" id="ad-type">
      <Select id="ad-type" bind:value={form.source_type} options={[
        { value: 'soundcard', label: 'Sound Card' },
        { value: 'flac', label: 'FLAC File' },
        { value: 'stdin', label: 'Standard Input' },
        { value: 'sdr_udp', label: 'SDR UDP Stream' },
      ]} />
    </FormField>
    <FormField label="Sample Rate" id="ad-rate">
      <Select id="ad-rate" bind:value={form.sample_rate} options={[
        { value: '8000', label: '8000 Hz' },
        { value: '16000', label: '16000 Hz' },
        { value: '44100', label: '44100 Hz' },
        { value: '48000', label: '48000 Hz' },
        { value: '96000', label: '96000 Hz' },
      ]} />
    </FormField>
    <FormField label="Channels" id="ad-ch">
      <Select id="ad-ch" bind:value={form.channels} options={[
        { value: '1', label: 'Mono' },
        { value: '2', label: 'Stereo' },
      ]} />
    </FormField>
    <div class="modal-actions">
      <Button onclick={() => modalOpen = false}>Cancel</Button>
      <Button variant="primary" type="submit">{editing ? 'Save' : 'Add'}</Button>
    </div>
  </form>
</Modal>

<!-- Delete confirmation -->
<AlertDialog bind:open={deleteOpen}>
  <AlertDialog.Content>
    <AlertDialog.Title>Delete Audio Device</AlertDialog.Title>
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
  /* Station readiness */
  .readiness {
    display: flex;
    gap: 16px;
    margin-bottom: 24px;
    flex-wrap: wrap;
  }
  .readiness-item {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    flex: 1;
    min-width: 260px;
    padding: 12px 16px;
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius);
    border-left: 3px solid var(--text-muted);
  }
  .readiness-item.ready {
    border-left-color: var(--success, #3fb950);
  }
  .readiness-icon {
    font-size: 16px;
    line-height: 1.2;
    color: var(--text-muted);
  }
  .readiness-item.ready .readiness-icon {
    color: var(--success, #3fb950);
  }
  .readiness-info {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .readiness-label {
    font-weight: 600;
    font-size: 14px;
  }
  .readiness-detail {
    font-size: 12px;
    color: var(--text-secondary);
  }
  .readiness-detail.needs {
    color: var(--text-muted);
    font-style: italic;
  }

  .section-label {
    font-size: 12px;
    font-weight: 600;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.5px;
    margin-bottom: 8px;
  }
  .section-hint {
    font-size: 13px;
    color: var(--text-muted);
    margin: -4px 0 10px;
  }

  .empty-state {
    text-align: center;
    color: var(--text-muted);
    padding: 32px;
    border: 1px dashed var(--border-color);
    border-radius: var(--radius);
    margin-bottom: 16px;
  }

  /* Configured device cards */
  .device-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
    gap: 12px;
    margin-bottom: 16px;
  }
  .device-card {
    display: flex;
    flex-direction: column;
    padding: 16px;
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius);
  }
  .device-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 12px;
    gap: 8px;
  }
  .device-name {
    font-weight: 600;
    font-size: 15px;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .device-badges {
    display: flex;
    gap: 4px;
    flex-shrink: 0;
  }
  .device-details {
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
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .device-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    margin-top: 12px;
    padding-top: 12px;
    border-top: 1px solid var(--border-color);
  }

  /* Available device cards */
  .avail-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 10px;
  }
  .avail-card {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-height: 120px;
    padding: 14px;
    background: var(--bg-tertiary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius);
    cursor: pointer;
    color: var(--text-primary);
    text-align: left;
    font-size: 13px;
    transition: border-color 0.15s, background 0.15s;
  }
  .avail-card:hover {
    border-color: var(--accent);
    background: var(--bg-secondary);
  }
  .avail-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .avail-name {
    font-size: 14px;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .avail-api {
    font-size: 11px;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.3px;
  }
  .avail-path {
    color: var(--text-secondary);
    font-family: var(--font-mono);
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .avail-caps {
    display: flex;
    flex-direction: column;
    gap: 2px;
    font-size: 12px;
    color: var(--text-muted);
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
