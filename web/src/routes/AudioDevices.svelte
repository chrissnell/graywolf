<script>
  import { onMount } from 'svelte';
  import { Button, Input, Select } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import DataTable from '../components/DataTable.svelte';
  import Modal from '../components/Modal.svelte';
  import FormField from '../components/FormField.svelte';

  let devices = $state([]);
  let available = $state([]);
  let loadingAvail = $state(false);
  let modalOpen = $state(false);
  let editing = $state(null);
  let form = $state({ name: '', device_path: '', sample_rate: '48000', channels: '1' });
  let errors = $state({});

  const columns = [
    { key: 'name', label: 'Name' },
    { key: 'device_path', label: 'Device Path' },
    { key: 'sample_rate', label: 'Sample Rate' },
    { key: 'channels', label: 'Channels' },
  ];

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
    form = { name: '', device_path: '', sample_rate: '48000', channels: '1' };
    errors = {};
    modalOpen = true;
  }

  function openEdit(row) {
    editing = row;
    form = { ...row, sample_rate: String(row.sample_rate), channels: String(row.channels) };
    errors = {};
    modalOpen = true;
  }

  function selectAvailDevice(dev) {
    form.name = dev.name;
    form.device_path = dev.path;
    form.sample_rate = String(dev.sample_rates[dev.sample_rates.length - 1]);
    form.channels = String(dev.channels[0]);
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

  async function handleDelete(row) {
    if (!confirm(`Delete "${row.name}"?`)) return;
    try {
      await api.delete(`/audio-devices/${row.id}`);
      toasts.success('Device deleted');
      await loadDevices();
    } catch (err) {
      toasts.error(err.message);
    }
  }
</script>

<PageHeader title="Audio Devices" subtitle="Sound card configuration">
  <Button onclick={refreshAvailable} disabled={loadingAvail}>
    {loadingAvail ? 'Scanning...' : 'Refresh Available'}
  </Button>
  <Button variant="primary" onclick={openCreate}>+ Add Device</Button>
</PageHeader>

{#if available.length > 0}
  <div class="available-panel">
    <h3 class="avail-title">Available Devices</h3>
    <div class="avail-grid">
      {#each available as dev}
        <button class="avail-card" onclick={() => { openCreate(); selectAvailDevice(dev); }}>
          <strong>{dev.name}</strong>
          <span class="avail-path">{dev.path}</span>
          <span class="avail-detail">
            Rates: {dev.sample_rates.join(', ')} Hz | Ch: {dev.channels.join(', ')}
          </span>
        </button>
      {/each}
    </div>
  </div>
{/if}

<DataTable {columns} rows={devices} onEdit={openEdit} onDelete={handleDelete} />

<Modal bind:open={modalOpen} title={editing ? 'Edit Audio Device' : 'Add Audio Device'}>
  <form onsubmit={handleSave}>
    <FormField label="Name" error={errors.name} id="ad-name">
      <Input id="ad-name" bind:value={form.name} placeholder="USB Sound Card" />
    </FormField>
    <FormField label="Device Path" error={errors.device_path} id="ad-path">
      <Input id="ad-path" bind:value={form.device_path} placeholder="hw:0,0" />
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

<style>
  .available-panel {
    margin-bottom: 16px;
    padding: 12px;
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius);
  }
  .avail-title {
    font-size: 13px;
    color: var(--text-secondary);
    margin-bottom: 10px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .avail-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
    gap: 8px;
  }
  .avail-card {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 10px;
    background: var(--bg-tertiary);
    border: 1px solid var(--border-color);
    border-radius: var(--radius);
    cursor: pointer;
    color: var(--text-primary);
    text-align: left;
    font-family: var(--font-mono);
    font-size: 13px;
    transition: border-color 0.15s;
  }
  .avail-card:hover {
    border-color: var(--accent);
  }
  .avail-path { color: var(--text-muted); font-size: 12px; }
  .avail-detail { color: var(--text-muted); font-size: 11px; }
  .modal-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    margin-top: 16px;
  }
</style>
