<script>
  import { onMount } from 'svelte';
  import { Button, Input, Toggle, Select, Box } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import FormField from '../components/FormField.svelte';
  import DataTable from '../components/DataTable.svelte';
  import Modal from '../components/Modal.svelte';

  let activeTab = $state('config');

  // Config state
  let form = $state({
    enabled: true, server: 'rotate.aprs2.net', port: '14580',
    callsign: '', passcode: '', filter: '',
  });
  let loading = $state(false);

  // Filters state
  let filters = $state([]);
  let modalOpen = $state(false);
  let editing = $state(null);
  let filterForm = $state({ name: '', type: 'range', pattern: '', enabled: true });
  let errors = $state({});

  const columns = [
    { key: 'name', label: 'Name' },
    { key: 'type', label: 'Type' },
    { key: 'pattern', label: 'Pattern' },
    { key: 'enabled', label: 'Enabled' },
  ];

  const typeOptions = [
    { value: 'range', label: 'Range' },
    { value: 'prefix', label: 'Prefix' },
    { value: 'budlist', label: 'Budlist' },
    { value: 'type', label: 'Type' },
    { value: 'symbol', label: 'Symbol' },
    { value: 'digipeater', label: 'Digipeater' },
    { value: 'object', label: 'Object' },
  ];

  onMount(async () => {
    const data = await api.get('/igate/config');
    if (data) {
      form = {
        enabled: data.enabled ?? false,
        server: data.server ?? 'rotate.aprs2.net',
        port: String(data.port ?? 14580),
        callsign: data.callsign ?? '',
        passcode: data.passcode ?? '',
        filter: data.filter ?? '',
      };
    }
    filters = await api.get('/igate/filters') || [];
  });

  // Config handlers
  function validateConfig() {
    if (form.enabled && !form.callsign.trim()) return false;
    return true;
  }

  async function handleSave(e) {
    e.preventDefault();
    if (!validateConfig()) {
      toasts.error('Callsign is required when iGate is enabled');
      return;
    }
    loading = true;
    try {
      await api.put('/igate/config', { ...form, port: parseInt(form.port) });
      toasts.success('iGate config saved');
    } catch (err) {
      toasts.error(err.message);
    } finally {
      loading = false;
    }
  }

  // Filter handlers
  function openCreate() {
    editing = null;
    filterForm = { name: '', type: 'range', pattern: '', enabled: true };
    errors = {};
    modalOpen = true;
  }

  function openEdit(row) {
    editing = row;
    filterForm = { ...row };
    errors = {};
    modalOpen = true;
  }

  function validateFilter() {
    const e = {};
    if (!filterForm.name.trim()) e.name = 'Required';
    if (!filterForm.pattern.trim()) e.pattern = 'Required';
    errors = e;
    return Object.keys(e).length === 0;
  }

  async function handleFilterSave() {
    if (!validateFilter()) return;
    try {
      if (editing) {
        await api.put(`/igate/filters/${editing.id}`, filterForm);
        toasts.success('Filter updated');
      } else {
        await api.post('/igate/filters', filterForm);
        toasts.success('Filter created');
      }
      modalOpen = false;
      filters = await api.get('/igate/filters') || [];
    } catch (err) {
      toasts.error(err.message);
    }
  }

  async function handleDelete(row) {
    if (!confirm(`Delete filter "${row.name}"?`)) return;
    await api.delete(`/igate/filters/${row.id}`);
    toasts.success('Deleted');
    filters = await api.get('/igate/filters') || [];
  }
</script>

<PageHeader title="iGate" subtitle="Internet gateway configuration" />

<div class="tabs">
  <button class="tab" class:active={activeTab === 'config'} onclick={() => activeTab = 'config'}>RF → APRS-IS</button>
  <button class="tab" class:active={activeTab === 'filters'} onclick={() => activeTab = 'filters'}>APRS-IS → RF</button>
</div>

<div class="tab-panel" class:hidden={activeTab !== 'config'}>
  <p class="tab-doc">
    Connection settings for the APRS-IS network. When enabled, graywolf logs in to an
    APRS-IS server with your callsign and passcode, receives packets matching your
    server-side filter, and gates eligible RF-heard traffic up to the internet.
  </p>
  <Box>
    <form onsubmit={handleSave}>
      <Toggle bind:checked={form.enabled} label="Enable iGate" />
      <div style="margin-top: 16px;">
        <FormField label="APRS-IS Server" id="ig-server">
          <Input id="ig-server" bind:value={form.server} placeholder="rotate.aprs2.net" />
        </FormField>
        <FormField label="Port" id="ig-port">
          <Input id="ig-port" bind:value={form.port} type="number" placeholder="14580" />
        </FormField>
        <FormField label="Callsign" id="ig-call">
          <Input id="ig-call" bind:value={form.callsign} placeholder="N0CALL-10" />
        </FormField>
        <FormField label="Passcode" id="ig-pass">
          <Input id="ig-pass" bind:value={form.passcode} type="password" placeholder="12345" />
        </FormField>
        <FormField label="APRS-IS Server Filter" id="ig-filter" hint="Filter string sent to APRS-IS at login to control what the server forwards to you (server-side, IS → you).">
          <Input id="ig-filter" bind:value={form.filter} placeholder="r/35.0/-106.0/100" />
        </FormField>
      </div>
      <div class="form-actions">
        <Button variant="primary" type="submit" disabled={loading}>
          {loading ? 'Saving...' : 'Save'}
        </Button>
      </div>
    </form>
  </Box>
</div>

<div class="tab-panel" class:hidden={activeTab !== 'filters'}>
  <p class="tab-doc">
    Rules that decide which packets received from APRS-IS are allowed to be transmitted
    on RF. Each incoming IS packet is checked against these rules; only matching packets
    are gated to RF (subject to the usual third-party, recent-heard, and beacon checks).
    If no rules are defined, nothing is gated from IS to RF.
  </p>
  <div class="filters-header">
    <Button variant="primary" onclick={openCreate}>+ Add Filter</Button>
  </div>
  <DataTable {columns} rows={filters} onEdit={openEdit} onDelete={handleDelete} />
</div>

<Modal bind:open={modalOpen} title={editing ? 'Edit Filter' : 'New Filter'}>
    <FormField label="Name" error={errors.name} id="flt-name">
      <Input id="flt-name" bind:value={filterForm.name} placeholder="Local area" />
    </FormField>
    <FormField label="Type" id="flt-type">
      <Select id="flt-type" bind:value={filterForm.type} options={typeOptions} />
    </FormField>
    <FormField label="Pattern" error={errors.pattern} id="flt-pattern">
      <Input id="flt-pattern" bind:value={filterForm.pattern} placeholder="r/35.0/-106.0/50" />
    </FormField>
    <Toggle bind:checked={filterForm.enabled} label="Enabled" />
    <div class="modal-actions">
      <Button onclick={() => modalOpen = false}>Cancel</Button>
      <Button variant="primary" onclick={handleFilterSave}>{editing ? 'Save' : 'Create'}</Button>
    </div>
</Modal>

<style>
  .tabs {
    display: flex;
    gap: 0;
    margin-bottom: 16px;
    border-bottom: 1px solid var(--border-color);
  }
  .tab {
    padding: 8px 20px;
    background: none;
    border: none;
    border-bottom: 2px solid transparent;
    color: var(--text-secondary);
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    transition: color 0.15s, border-color 0.15s;
  }
  .tab:hover {
    color: var(--text-primary);
  }
  .tab.active {
    color: var(--accent);
    border-bottom-color: var(--accent);
  }
  .filters-header {
    display: flex;
    justify-content: flex-end;
    margin-bottom: 12px;
  }
  .tab-panel.hidden { display: none; }
  .tab-doc {
    font-size: 13px;
    color: var(--text-secondary);
    line-height: 1.5;
    margin: 0 0 16px;
    max-width: 720px;
  }
  .form-actions { display: flex; justify-content: flex-end; margin-top: 16px; }
  .modal-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 16px; }
</style>
