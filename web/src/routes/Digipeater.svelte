<script>
  import { onMount } from 'svelte';
  import { Button, Input, Toggle, Box } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import DataTable from '../components/DataTable.svelte';
  import Modal from '../components/Modal.svelte';
  import FormField from '../components/FormField.svelte';

  let config = $state({ enabled: false, callsign: '' });
  let rules = $state([]);
  let modalOpen = $state(false);
  let editing = $state(null);
  let form = $state({ alias: '', substitute: true, preempt: false, enabled: true });
  let savingConfig = $state(false);

  const columns = [
    { key: 'alias', label: 'Alias' },
    { key: 'substitute', label: 'Substitute' },
    { key: 'preempt', label: 'Preempt' },
    { key: 'enabled', label: 'Enabled' },
  ];

  onMount(async () => {
    const data = await api.get('/digipeater');
    if (data) {
      config = { enabled: data.enabled, callsign: data.callsign };
      rules = data.rules || [];
    }
  });

  async function saveConfig(e) {
    e.preventDefault();
    savingConfig = true;
    try {
      await api.put('/digipeater', config);
      toasts.success('Digipeater config saved');
    } catch (err) {
      toasts.error(err.message);
    } finally {
      savingConfig = false;
    }
  }

  function openCreate() {
    editing = null;
    form = { alias: '', substitute: true, preempt: false, enabled: true };
    modalOpen = true;
  }

  function openEdit(row) {
    editing = row;
    form = { ...row };
    modalOpen = true;
  }

  async function handleSaveRule() {
    if (!form.alias.trim()) { toasts.error('Alias required'); return; }
    try {
      if (editing) {
        await api.put(`/digipeater/rules/${editing.id}`, form);
        toasts.success('Rule updated');
      } else {
        await api.post('/digipeater/rules', form);
        toasts.success('Rule created');
      }
      modalOpen = false;
      rules = await api.get('/digipeater/rules') || [];
    } catch (err) {
      toasts.error(err.message);
    }
  }

  async function handleDelete(row) {
    if (!confirm(`Delete rule "${row.alias}"?`)) return;
    await api.delete(`/digipeater/rules/${row.id}`);
    toasts.success('Deleted');
    rules = await api.get('/digipeater/rules') || [];
  }
</script>

<PageHeader title="Digipeater" subtitle="Digital repeater configuration and rules" />

<Box title="Settings">
  <form onsubmit={saveConfig}>
    <Toggle bind:checked={config.enabled} label="Enable Digipeater" />
    <div style="margin-top: 12px;">
      <FormField label="Callsign" id="digi-call">
        <Input id="digi-call" bind:value={config.callsign} placeholder="N0CALL-1" />
      </FormField>
    </div>
    <div class="form-actions">
      <Button variant="primary" type="submit" disabled={savingConfig}>Save Settings</Button>
    </div>
  </form>
</Box>

<div style="margin-top: 20px;">
  <PageHeader title="Digipeater Rules">
    <Button variant="primary" onclick={openCreate}>+ Add Rule</Button>
  </PageHeader>
  <DataTable {columns} rows={rules} onEdit={openEdit} onDelete={handleDelete} />
</div>

<Modal bind:open={modalOpen} title={editing ? 'Edit Rule' : 'New Rule'}>
    <FormField label="Alias" id="rule-alias">
      <Input id="rule-alias" bind:value={form.alias} placeholder="WIDE1-1" />
    </FormField>
    <div style="display: flex; gap: 20px; margin: 8px 0;">
      <Toggle bind:checked={form.substitute} label="Substitute" />
      <Toggle bind:checked={form.preempt} label="Preempt" />
      <Toggle bind:checked={form.enabled} label="Enabled" />
    </div>
    <div class="modal-actions">
      <Button onclick={() => modalOpen = false}>Cancel</Button>
      <Button variant="primary" onclick={handleSaveRule}>{editing ? 'Save' : 'Create'}</Button>
    </div>
</Modal>

<style>
  .form-actions { display: flex; justify-content: flex-end; margin-top: 16px; }
  .modal-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 16px; }
</style>
