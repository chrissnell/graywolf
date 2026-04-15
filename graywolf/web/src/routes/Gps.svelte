<script>
  import { onMount } from 'svelte';
  import { Button, Input, Select, Badge, AlertDialog } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import Modal from '../components/Modal.svelte';
  import FormField from '../components/FormField.svelte';

  let config = $state(null);
  let available = $state([]);
  let loadingAvail = $state(false);
  let modalOpen = $state(false);
  let form = $state(emptyForm());
  let disableOpen = $state(false);

  const sourceOptions = [
    { value: 'serial', label: 'Serial Port' },
    { value: 'gpsd', label: 'GPSD' },
  ];

  const sourceLabels = Object.fromEntries(sourceOptions.map(o => [o.value, o.label]));

  function emptyForm() {
    return {
      source: 'serial', serial_port: '/dev/ttyACM0', baud_rate: '9600',
      gpsd_host: 'localhost', gpsd_port: '2947',
    };
  }

  onMount(async () => {
    await loadConfig();
  });

  async function loadConfig() {
    const data = await api.get('/gps');
    if (data) config = data;
  }

  async function detectPorts() {
    loadingAvail = true;
    try {
      available = await api.get('/gps/available') || [];
      toasts.success(`Found ${available.length} serial port(s)`);
    } catch (err) {
      toasts.error(err.message);
    } finally {
      loadingAvail = false;
    }
  }

  function openCreate() {
    form = emptyForm();
    modalOpen = true;
  }

  function openEdit() {
    if (!config) return;
    form = {
      source: config.source,
      serial_port: config.serial_port || '/dev/ttyACM0',
      baud_rate: String(config.baud_rate || 9600),
      gpsd_host: config.gpsd_host || 'localhost',
      gpsd_port: String(config.gpsd_port || 2947),
    };
    modalOpen = true;
  }

  function openCreateFromAvail(port) {
    form = {
      source: 'serial',
      serial_port: port.path,
      baud_rate: '9600',
      gpsd_host: 'localhost',
      gpsd_port: '2947',
    };
    modalOpen = true;
  }

  async function handleSave() {
    try {
      await api.put('/gps', {
        ...form, baud_rate: parseInt(form.baud_rate), gpsd_port: parseInt(form.gpsd_port),
      });
      toasts.success('GPS config saved');
      modalOpen = false;
      await loadConfig();
    } catch (err) {
      toasts.error(err.message);
    }
  }

  async function executeDisable() {
    try {
      await api.put('/gps', {
        source: 'none', serial_port: '', baud_rate: 9600,
        gpsd_host: 'localhost', gpsd_port: 2947,
      });
      toasts.success('GPS disabled');
      await loadConfig();
    } catch (err) {
      toasts.error(err.message);
    } finally {
      disableOpen = false;
    }
  }

  let hasGps = $derived(config && config.source && config.source !== 'none');
</script>

<PageHeader title="GPS" subtitle="GPS source configuration">
  <Button onclick={detectPorts} disabled={loadingAvail}>
    {loadingAvail ? 'Scanning...' : 'Detect Devices'}
  </Button>
  {#if !hasGps}
    <Button variant="primary" onclick={openCreate}>+ Configure GPS</Button>
  {/if}
</PageHeader>

<!-- GPS readiness -->
<div class="readiness">
  <div class="readiness-item" class:ready={hasGps}>
    <div class="readiness-icon">{hasGps ? '●' : '○'}</div>
    <div class="readiness-info">
      <span class="readiness-label">GPS</span>
      {#if hasGps}
        <span class="readiness-detail">GPS configured via {sourceLabels[config.source] || config.source}</span>
      {:else}
        <span class="readiness-detail needs">No GPS configured — position reporting requires a GPS source</span>
      {/if}
    </div>
  </div>
</div>

<!-- Configured GPS -->
<div class="section-label">Configured GPS</div>
{#if !hasGps}
  <div class="empty-state">No GPS configured. Detect devices below or add one manually.</div>
{:else}
  <div class="device-grid">
    <div class="device-card">
      <div class="device-header">
        <span class="device-name">GPS Source</span>
        <div class="device-badges">
          <Badge variant="success">{sourceLabels[config.source] || config.source}</Badge>
        </div>
      </div>
      <div class="device-details">
        {#if config.source === 'serial'}
          <div class="detail-row">
            <span class="detail-label">Serial Port</span>
            <span class="detail-value">{config.serial_port || '—'}</span>
          </div>
          <div class="detail-row">
            <span class="detail-label">Baud Rate</span>
            <span class="detail-value">{config.baud_rate}</span>
          </div>
        {:else if config.source === 'gpsd'}
          <div class="detail-row">
            <span class="detail-label">Host</span>
            <span class="detail-value">{config.gpsd_host || 'localhost'}</span>
          </div>
          <div class="detail-row">
            <span class="detail-label">Port</span>
            <span class="detail-value">{config.gpsd_port || 2947}</span>
          </div>
        {/if}
      </div>
      <div class="device-actions">
        <Button variant="ghost" onclick={openEdit}>Edit</Button>
        <Button variant="danger" onclick={() => disableOpen = true}>Disable</Button>
      </div>
    </div>
  </div>
{/if}

<!-- Available devices from hardware scan -->
{#if available.length > 0}
  <div class="section-label" style="margin-top: 24px;">Detected Hardware</div>
  <p class="section-hint">Click a device to configure GPS with it.</p>
  <div class="avail-grid">
    {#each available as port}
      <button class="avail-card" class:warning={port.warning} onclick={() => openCreateFromAvail(port)}>
        <div class="avail-header">
          <strong class="avail-name">{port.description}</strong>
          <div class="avail-badges">
            {#if port.is_usb}
              <Badge variant="info">USB</Badge>
            {/if}
            {#if port.recommended && !port.warning}
              <Badge variant="success">Recommended</Badge>
            {/if}
          </div>
        </div>
        <span class="avail-path" title={port.path}>{port.path}</span>
        {#if port.vid && port.pid}
          <span class="avail-usb">VID:PID {port.vid}:{port.pid}{port.serial_number ? ` · SN ${port.serial_number}` : ''}</span>
        {/if}
        {#if port.warning}
          <span class="avail-warning">⚠ {port.warning}</span>
        {/if}
      </button>
    {/each}
  </div>
{/if}

<!-- Add/Edit modal -->
<Modal bind:open={modalOpen} title={hasGps ? 'Edit GPS Config' : 'Configure GPS'} onClose={() => form = emptyForm()}>
  <FormField label="Source" id="gps-source">
    <Select id="gps-source" bind:value={form.source} options={sourceOptions} />
  </FormField>
  {#if form.source === 'serial'}
    <FormField label="Serial Port" id="gps-serial">
      <Input id="gps-serial" bind:value={form.serial_port} placeholder="/dev/ttyACM0" />
    </FormField>
    <FormField label="Baud Rate" id="gps-baud">
      <Select id="gps-baud" bind:value={form.baud_rate} options={[
        { value: '4800', label: '4800' },
        { value: '9600', label: '9600' },
        { value: '38400', label: '38400' },
        { value: '115200', label: '115200' },
      ]} />
    </FormField>
  {:else if form.source === 'gpsd'}
    <FormField label="GPSD Host" id="gps-host">
      <Input id="gps-host" bind:value={form.gpsd_host} placeholder="localhost" />
    </FormField>
    <FormField label="GPSD Port" id="gps-port">
      <Input id="gps-port" bind:value={form.gpsd_port} type="number" placeholder="2947" />
    </FormField>
  {/if}
  <div class="modal-actions">
    <Button onclick={() => modalOpen = false}>Cancel</Button>
    <Button variant="primary" onclick={handleSave}>{hasGps ? 'Save' : 'Configure'}</Button>
  </div>
</Modal>

<!-- Disable confirmation -->
<AlertDialog bind:open={disableOpen}>
  <AlertDialog.Content>
    <AlertDialog.Title>Disable GPS</AlertDialog.Title>
    <AlertDialog.Description>
      Are you sure you want to disable GPS? Position reporting will be unavailable.
    </AlertDialog.Description>
    <div class="modal-footer">
      <AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
      <AlertDialog.Action class="danger-action" onclick={executeDisable}>Disable</AlertDialog.Action>
    </div>
  </AlertDialog.Content>
</AlertDialog>

<style>
  /* Readiness */
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

  /* Configured device card */
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
    min-height: 80px;
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
  .avail-card.warning {
    border-left: 3px solid var(--color-warning, #d29922);
  }
  .avail-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .avail-badges {
    display: flex;
    gap: 4px;
    flex-shrink: 0;
  }
  .avail-name {
    font-size: 14px;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .avail-path {
    color: var(--text-secondary);
    font-family: var(--font-mono);
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .avail-usb {
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--text-muted);
  }
  .avail-warning {
    font-size: 11px;
    color: var(--color-warning, #d29922);
    margin-top: 4px;
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
