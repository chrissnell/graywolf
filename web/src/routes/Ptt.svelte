<script>
  import { onMount } from 'svelte';
  import { Button, Input, Select, Badge, AlertDialog } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import Modal from '../components/Modal.svelte';
  import FormField from '../components/FormField.svelte';

  let items = $state([]);
  let available = $state([]);
  let loadingAvail = $state(false);
  let modalOpen = $state(false);
  let editing = $state(null);
  let form = $state(emptyForm());
  let errors = $state({});
  let deleteTarget = $state(null);
  let deleteOpen = $state(false);

  const methodOptions = [
    { value: 'none', label: 'None' },
    { value: 'serial_rts', label: 'Serial RTS' },
    { value: 'serial_dtr', label: 'Serial DTR' },
    { value: 'gpio', label: 'GPIO' },
    { value: 'cm108', label: 'CM108' },
  ];

  const methodLabels = Object.fromEntries(methodOptions.map(o => [o.value, o.label]));

  function emptyForm() {
    return { channel_id: '1', method: 'none', device_path: '', gpio_pin: '0' };
  }

  onMount(loadItems);

  async function loadItems() {
    items = await api.get('/ptt') || [];
  }

  async function refreshAvailable() {
    loadingAvail = true;
    try {
      available = await api.get('/ptt/available') || [];
      toasts.success(`Found ${available.length} PTT-capable device(s)`);
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

  function openEdit(item) {
    editing = item;
    form = {
      channel_id: String(item.channel_id),
      method: item.method,
      device_path: item.device_path || '',
      gpio_pin: String(item.gpio_pin || 0),
    };
    errors = {};
    modalOpen = true;
  }

  function openCreateFromAvail(dev) {
    editing = null;
    const method = dev.type === 'gpio' ? 'gpio'
      : dev.type === 'cm108' ? 'cm108'
      : 'serial_rts';
    form = {
      channel_id: '1',
      method,
      device_path: dev.path,
      gpio_pin: '0',
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
    if (form.method !== 'none' && !form.device_path.trim()) e.device_path = 'Device path required';
    errors = e;
    return Object.keys(e).length === 0;
  }

  async function handleSave(e) {
    e.preventDefault();
    if (!validate()) return;
    const data = { ...form, channel_id: parseInt(form.channel_id), gpio_pin: parseInt(form.gpio_pin) };
    try {
      if (editing) {
        await api.put(`/ptt/${editing.id}`, data);
        toasts.success('PTT config updated');
      } else {
        await api.post('/ptt', data);
        toasts.success('PTT config created');
      }
      modalOpen = false;
      await loadItems();
    } catch (err) {
      toasts.error(err.message);
    }
  }

  function confirmDelete(item) {
    deleteTarget = item;
    deleteOpen = true;
  }

  async function executeDelete() {
    if (!deleteTarget) return;
    try {
      await api.delete(`/ptt/${deleteTarget.id}`);
      toasts.success('PTT config deleted');
      await loadItems();
    } catch (err) {
      toasts.error(err.message);
    } finally {
      deleteOpen = false;
      deleteTarget = null;
    }
  }

  let hasPtt = $derived(items.some(p => p.method !== 'none'));

  function truncatePath(p, max = 40) {
    if (!p || p.length <= max) return p || '—';
    return '...' + p.slice(-(max - 3));
  }

  function typeBadgeVariant(type) {
    if (type === 'serial') return 'info';
    if (type === 'gpio') return 'warning';
    if (type === 'cm108') return 'success';
    return 'default';
  }
</script>

<PageHeader title="PTT Configuration" subtitle="Push-to-talk settings per channel">
  <Button onclick={refreshAvailable} disabled={loadingAvail}>
    {loadingAvail ? 'Scanning...' : 'Detect Devices'}
  </Button>
  <Button variant="primary" onclick={openCreate}>+ Add PTT</Button>
</PageHeader>

<!-- PTT readiness -->
<div class="readiness">
  <div class="readiness-item" class:ready={hasPtt}>
    <div class="readiness-icon">{hasPtt ? '●' : '○'}</div>
    <div class="readiness-info">
      <span class="readiness-label">Push-to-Talk</span>
      {#if hasPtt}
        <span class="readiness-detail">{items.filter(p => p.method !== 'none').length} channel(s) with PTT configured</span>
      {:else}
        <span class="readiness-detail needs">No PTT configured — transmit requires a PTT method</span>
      {/if}
    </div>
  </div>
</div>

<!-- Configured PTT devices -->
<div class="section-label">Configured PTT</div>
{#if items.length === 0}
  <div class="empty-state">No PTT configurations. Detect devices below or add one manually.</div>
{:else}
  <div class="device-grid">
    {#each items as item}
      <div class="device-card">
        <div class="device-header">
          <span class="device-name">Channel {item.channel_id}</span>
          <div class="device-badges">
            <Badge variant={item.method === 'none' ? 'default' : 'success'}>
              {methodLabels[item.method] || item.method}
            </Badge>
          </div>
        </div>
        <div class="device-details">
          {#if item.method !== 'none'}
            <div class="detail-row">
              <span class="detail-label">Device</span>
              <span class="detail-value" title={item.device_path}>{truncatePath(item.device_path)}</span>
            </div>
          {/if}
          {#if item.method === 'gpio'}
            <div class="detail-row">
              <span class="detail-label">GPIO Pin</span>
              <span class="detail-value">{item.gpio_pin}</span>
            </div>
          {/if}
          {#if item.method === 'none'}
            <div class="detail-row">
              <span class="detail-label">Status</span>
              <span class="detail-value muted">No PTT method set</span>
            </div>
          {/if}
        </div>
        <div class="device-actions">
          <Button variant="ghost" onclick={() => openEdit(item)}>Edit</Button>
          <Button variant="danger" onclick={() => confirmDelete(item)}>Delete</Button>
        </div>
      </div>
    {/each}
  </div>
{/if}

<!-- Available devices from hardware scan -->
{#if available.length > 0}
  <div class="section-label" style="margin-top: 24px;">Detected Hardware</div>
  <p class="section-hint">Click a device to create a PTT configuration for it.</p>
  <div class="avail-grid">
    {#each available as dev}
      <button class="avail-card" onclick={() => openCreateFromAvail(dev)}>
        <div class="avail-header">
          <strong class="avail-name">{dev.name}</strong>
          <Badge variant={typeBadgeVariant(dev.type)}>
            {dev.type}
          </Badge>
        </div>
        <span class="avail-path" title={dev.path}>{truncatePath(dev.path, 50)}</span>
      </button>
    {/each}
  </div>
{/if}

<!-- Add/Edit modal -->
<Modal bind:open={modalOpen} title={editing ? 'Edit PTT Config' : 'New PTT Config'} onClose={handleModalClose}>
  <form onsubmit={handleSave}>
    <FormField label="Channel ID" id="ptt-ch">
      <Input id="ptt-ch" bind:value={form.channel_id} type="number" />
    </FormField>
    <FormField label="Method" id="ptt-method">
      <Select id="ptt-method" bind:value={form.method} options={methodOptions} />
    </FormField>
    {#if form.method !== 'none'}
      <FormField label="Device Path" error={errors.device_path} id="ptt-dev">
        <Input id="ptt-dev" bind:value={form.device_path} placeholder="Select a detected device or enter path" />
      </FormField>
    {/if}
    {#if form.method === 'gpio'}
      <FormField label="GPIO Pin" id="ptt-gpio">
        <Input id="ptt-gpio" bind:value={form.gpio_pin} type="number" />
      </FormField>
    {/if}
    <div class="modal-actions">
      <Button onclick={() => modalOpen = false}>Cancel</Button>
      <Button variant="primary" type="submit">{editing ? 'Save' : 'Create'}</Button>
    </div>
  </form>
</Modal>

<!-- Delete confirmation -->
<AlertDialog bind:open={deleteOpen}>
  <AlertDialog.Content>
    <AlertDialog.Title>Delete PTT Config</AlertDialog.Title>
    <AlertDialog.Description>
      Are you sure you want to delete the PTT configuration for Channel {deleteTarget?.channel_id}? This cannot be undone.
    </AlertDialog.Description>
    <div class="modal-footer">
      <AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
      <AlertDialog.Action class="danger-action" onclick={executeDelete}>Delete</AlertDialog.Action>
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
  .detail-value.muted {
    color: var(--text-muted);
    font-style: italic;
    font-family: inherit;
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
  .avail-path {
    color: var(--text-secondary);
    font-family: var(--font-mono);
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
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
