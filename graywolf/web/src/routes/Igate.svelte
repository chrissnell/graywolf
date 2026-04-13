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
    callsign: '', passcode: '', server_filter: '',
  });
  let loading = $state(false);

  // Filters state
  let filters = $state([]);
  let modalOpen = $state(false);
  let editing = $state(null);
  let filterForm = $state({ type: 'prefix', pattern: '', action: 'allow', priority: 100, enabled: true });
  let errors = $state({});

  const columns = [
    { key: 'type', label: 'Type' },
    { key: 'pattern', label: 'Pattern' },
    { key: 'action', label: 'Action' },
    { key: 'priority', label: 'Priority' },
    { key: 'enabled', label: 'Enabled' },
  ];

  const typeOptions = [
    { value: 'callsign', label: 'Callsign' },
    { value: 'prefix', label: 'Prefix' },
    { value: 'message_dest', label: 'Message Dest' },
    { value: 'object', label: 'Object' },
  ];

  const actionOptions = [
    { value: 'allow', label: 'Allow' },
    { value: 'deny', label: 'Deny' },
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
        server_filter: data.server_filter ?? '',
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
    filterForm = { type: 'prefix', pattern: '', action: 'allow', priority: 100, enabled: true };
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
    APRS-IS server with your callsign and passcode and gates eligible RF-heard traffic
    up to the internet.
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
    Controls which APRS-IS packets reach graywolf and which are gated to RF.
    The server filter limits what the APRS-IS server forwards to you (if empty,
    no packets are received). The rules below decide which of those packets are
    transmitted on RF — if no rules are defined, nothing is gated.
  </p>
  <Box>
    <form onsubmit={handleSave}>
      <FormField label="APRS-IS Server Filter" id="ig-filter" hint="Filter string sent to APRS-IS at login (e.g. r/35.0/-106.0/100). If empty, no packets are received from APRS-IS.">
        <Input id="ig-filter" bind:value={form.server_filter} placeholder="r/35.0/-106.0/100" />
      </FormField>
      <div class="form-actions">
        <Button variant="primary" type="submit" disabled={loading}>
          {loading ? 'Saving...' : 'Save'}
        </Button>
      </div>
    </form>
  </Box>
  <h3 class="section-heading">IS → RF Gating Rules</h3>
  <div class="filters-header">
    <Button variant="primary" onclick={openCreate}>+ Add Rule</Button>
  </div>
  <DataTable {columns} rows={filters} onEdit={openEdit} onDelete={handleDelete} />
</div>

<Modal bind:open={modalOpen} title={editing ? 'Edit Rule' : 'New Rule'}>
    <FormField label="Type" id="flt-type">
      <Select id="flt-type" bind:value={filterForm.type} options={typeOptions} />
    </FormField>
    <FormField label="Pattern" error={errors.pattern} id="flt-pattern">
      <Input id="flt-pattern" bind:value={filterForm.pattern} placeholder="W5" />
    </FormField>
    <FormField label="Action" id="flt-action">
      <Select id="flt-action" bind:value={filterForm.action} options={actionOptions} />
    </FormField>
    <FormField label="Priority" id="flt-priority">
      <Input id="flt-priority" bind:value={filterForm.priority} type="number" placeholder="100" />
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
  .section-heading {
    font-size: 14px;
    font-weight: 600;
    color: var(--text-primary);
    margin: 20px 0 8px;
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
