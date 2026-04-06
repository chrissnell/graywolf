<script>
  import { onMount } from 'svelte';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import DataTable from '../components/DataTable.svelte';
  import Modal from '../components/Modal.svelte';
  import FormField from '../components/FormField.svelte';
  import TextInput from '../components/TextInput.svelte';
  import SelectInput from '../components/SelectInput.svelte';
  import ToggleSwitch from '../components/ToggleSwitch.svelte';
  import Btn from '../components/Btn.svelte';

  let filters = $state([]);
  let modalOpen = $state(false);
  let editing = $state(null);
  let form = $state({ name: '', type: 'range', pattern: '', enabled: true });
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

  onMount(async () => { filters = await api.get('/igate/filters') || []; });

  function openCreate() {
    editing = null;
    form = { name: '', type: 'range', pattern: '', enabled: true };
    errors = {};
    modalOpen = true;
  }

  function openEdit(row) {
    editing = row;
    form = { ...row };
    errors = {};
    modalOpen = true;
  }

  function validate() {
    const e = {};
    if (!form.name.trim()) e.name = 'Required';
    if (!form.pattern.trim()) e.pattern = 'Required';
    errors = e;
    return Object.keys(e).length === 0;
  }

  async function handleSave(e) {
    e.preventDefault();
    if (!validate()) return;
    try {
      if (editing) {
        await api.put(`/igate/filters/${editing.id}`, form);
        toasts.success('Filter updated');
      } else {
        await api.post('/igate/filters', form);
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

<PageHeader title="iGate Filters" subtitle="IS-to-RF filter rules">
  <Btn variant="primary" onclick={openCreate}>+ Add Filter</Btn>
</PageHeader>

<DataTable {columns} rows={filters} onEdit={openEdit} onDelete={handleDelete} />

<Modal bind:open={modalOpen} title={editing ? 'Edit Filter' : 'New Filter'}>
  <form onsubmit={handleSave}>
    <FormField label="Name" error={errors.name} id="flt-name">
      <TextInput id="flt-name" bind:value={form.name} placeholder="Local area" />
    </FormField>
    <FormField label="Type" id="flt-type">
      <SelectInput id="flt-type" bind:value={form.type} options={typeOptions} />
    </FormField>
    <FormField label="Pattern" error={errors.pattern} id="flt-pattern">
      <TextInput id="flt-pattern" bind:value={form.pattern} placeholder="r/35.0/-106.0/50" />
    </FormField>
    <ToggleSwitch bind:checked={form.enabled} label="Enabled" id="flt-enabled" />
    <div class="modal-actions">
      <Btn variant="default" onclick={() => modalOpen = false}>Cancel</Btn>
      <Btn variant="primary" type="submit">{editing ? 'Save' : 'Create'}</Btn>
    </div>
  </form>
</Modal>

<style>
  .modal-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 16px; }
</style>
