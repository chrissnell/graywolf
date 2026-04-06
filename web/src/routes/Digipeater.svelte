<script>
  import { onMount } from 'svelte';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import Card from '../components/Card.svelte';
  import DataTable from '../components/DataTable.svelte';
  import Modal from '../components/Modal.svelte';
  import FormField from '../components/FormField.svelte';
  import TextInput from '../components/TextInput.svelte';
  import ToggleSwitch from '../components/ToggleSwitch.svelte';
  import Btn from '../components/Btn.svelte';

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

  async function handleSaveRule(e) {
    e.preventDefault();
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

<Card title="Settings">
  <form onsubmit={saveConfig}>
    <ToggleSwitch bind:checked={config.enabled} label="Enable Digipeater" id="digi-enabled" />
    <div style="margin-top: 12px;">
      <FormField label="Callsign" id="digi-call">
        <TextInput id="digi-call" bind:value={config.callsign} placeholder="N0CALL-1" />
      </FormField>
    </div>
    <div class="form-actions">
      <Btn variant="primary" type="submit" disabled={savingConfig}>Save Settings</Btn>
    </div>
  </form>
</Card>

<div style="margin-top: 20px;">
  <PageHeader title="Digipeater Rules">
    <Btn variant="primary" onclick={openCreate}>+ Add Rule</Btn>
  </PageHeader>
  <DataTable {columns} rows={rules} onEdit={openEdit} onDelete={handleDelete} />
</div>

<Modal bind:open={modalOpen} title={editing ? 'Edit Rule' : 'New Rule'}>
  <form onsubmit={handleSaveRule}>
    <FormField label="Alias" id="rule-alias">
      <TextInput id="rule-alias" bind:value={form.alias} placeholder="WIDE1-1" />
    </FormField>
    <div style="display: flex; gap: 20px; margin: 8px 0;">
      <ToggleSwitch bind:checked={form.substitute} label="Substitute" id="rule-sub" />
      <ToggleSwitch bind:checked={form.preempt} label="Preempt" id="rule-pre" />
      <ToggleSwitch bind:checked={form.enabled} label="Enabled" id="rule-on" />
    </div>
    <div class="modal-actions">
      <Btn variant="default" onclick={() => modalOpen = false}>Cancel</Btn>
      <Btn variant="primary" type="submit">{editing ? 'Save' : 'Create'}</Btn>
    </div>
  </form>
</Modal>

<style>
  .form-actions { display: flex; justify-content: flex-end; margin-top: 16px; }
  .modal-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 16px; }
</style>
