<script>
  import { onMount } from 'svelte';
  import { Button, Input, Select, Box, Badge } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import FormField from '../components/FormField.svelte';

  let form = $state({
    source: 'serial', serial_port: '/dev/ttyACM0', baud_rate: '9600',
    gpsd_host: 'localhost', gpsd_port: '2947',
  });
  let loading = $state(false);
  let available = $state([]);
  let loadingAvail = $state(false);

  const sourceOptions = [
    { value: 'serial', label: 'Serial Port' },
    { value: 'gpsd', label: 'GPSD' },
    { value: 'none', label: 'None' },
  ];

  onMount(async () => {
    const data = await api.get('/gps');
    if (data) form = {
      source: data.source, serial_port: data.serial_port,
      baud_rate: String(data.baud_rate), gpsd_host: data.gpsd_host,
      gpsd_port: String(data.gpsd_port),
    };
  });

  async function handleSave(e) {
    e.preventDefault();
    loading = true;
    try {
      await api.put('/gps', {
        ...form, baud_rate: parseInt(form.baud_rate), gpsd_port: parseInt(form.gpsd_port),
      });
      toasts.success('GPS config saved');
    } catch (err) {
      toasts.error(err.message);
    } finally {
      loading = false;
    }
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

  function selectPort(port) {
    form.serial_port = port.path;
    toasts.success(`Selected ${port.path}`);
  }
</script>

<PageHeader title="GPS" subtitle="GPS source configuration">
  {#if form.source === 'serial'}
    <Button onclick={detectPorts} disabled={loadingAvail}>
      {loadingAvail ? 'Scanning...' : 'Detect Devices'}
    </Button>
  {/if}
</PageHeader>

<Box>
  <form onsubmit={handleSave}>
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
    <div class="form-actions">
      <Button variant="primary" type="submit" disabled={loading}>
        {loading ? 'Saving...' : 'Save'}
      </Button>
    </div>
  </form>
</Box>

{#if form.source === 'serial' && available.length > 0}
  <div class="section-label">Detected Serial Ports</div>
  <p class="section-hint">Click a port to use it.</p>
  <div class="port-grid">
    {#each available as port}
      <button
        class="port-card"
        class:selected={form.serial_port === port.path}
        class:warning={port.warning}
        onclick={() => selectPort(port)}
      >
        <div class="port-header">
          <strong class="port-name">{port.description}</strong>
          <div class="port-badges">
            {#if form.serial_port === port.path}
              <Badge variant="success">Selected</Badge>
            {/if}
            {#if port.is_usb}
              <Badge variant="info">USB</Badge>
            {/if}
            {#if port.recommended && !port.warning}
              <Badge variant="success">Recommended</Badge>
            {/if}
          </div>
        </div>
        <span class="port-path" title={port.path}>{port.path}</span>
        {#if port.vid && port.pid}
          <span class="port-meta">VID:PID {port.vid}:{port.pid}{port.serial_number ? ` · SN ${port.serial_number}` : ''}</span>
        {/if}
        {#if port.warning}
          <span class="port-warning">⚠ {port.warning}</span>
        {/if}
      </button>
    {/each}
  </div>
{/if}

<style>
  .form-actions { display: flex; justify-content: flex-end; margin-top: 16px; }

  .section-label {
    font-size: 12px;
    font-weight: 600;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.5px;
    margin-top: 24px;
    margin-bottom: 8px;
  }
  .section-hint {
    font-size: 13px;
    color: var(--text-muted);
    margin: -4px 0 10px;
  }

  .port-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 10px;
  }
  .port-card {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-height: 100px;
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
  .port-card:hover {
    border-color: var(--accent);
    background: var(--bg-secondary);
  }
  .port-card.selected {
    border-color: var(--success, #3fb950);
    background: var(--bg-secondary);
  }
  .port-card.warning {
    border-left: 3px solid var(--color-warning, #d29922);
  }
  .port-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: 8px;
  }
  .port-badges {
    display: flex;
    gap: 4px;
    flex-shrink: 0;
    flex-wrap: wrap;
    justify-content: flex-end;
  }
  .port-name {
    font-size: 14px;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .port-path {
    color: var(--text-secondary);
    font-family: var(--font-mono);
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .port-meta {
    font-size: 11px;
    color: var(--text-muted);
    font-family: var(--font-mono);
  }
  .port-warning {
    font-size: 11px;
    color: var(--color-warning, #d29922);
    margin-top: 4px;
  }
</style>
