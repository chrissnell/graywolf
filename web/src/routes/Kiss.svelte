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
  import Btn from '../components/Btn.svelte';

  let items = $state([]);
  let modalOpen = $state(false);
  let editing = $state(null);
  let form = $state({ type: 'tcp', tcp_port: '8001', serial_device: '', baud_rate: '9600' });

  const columns = [
    { key: 'type', label: 'Type' },
    { key: 'tcp_port', label: 'TCP Port' },
    { key: 'serial_device', label: 'Serial Device' },
    { key: 'baud_rate', label: 'Baud Rate' },
  ];

  const typeOptions = [
    { value: 'tcp', label: 'TCP' },
    { value: 'serial', label: 'Serial' },
  ];

  onMount(async () => { items = await api.get('/kiss') || []; });

  function openCreate() {
    editing = null;
    form = { type: 'tcp', tcp_port: '8001', serial_device: '', baud_rate: '9600' };
    modalOpen = true;
  }

  function openEdit(row) {
    editing = row;
    form = { ...row, tcp_port: String(row.tcp_port), baud_rate: String(row.baud_rate) };
    modalOpen = true;
  }

  async function handleSave(e) {
    e.preventDefault();
    const data = { ...form, tcp_port: parseInt(form.tcp_port), baud_rate: parseInt(form.baud_rate) };
    try {
      if (editing) {
        await api.put(`/kiss/${editing.id}`, data);
        toasts.success('KISS config updated');
      } else {
        await api.post('/kiss', data);
        toasts.success('KISS config created');
      }
      modalOpen = false;
      items = await api.get('/kiss') || [];
    } catch (err) {
      toasts.error(err.message);
    }
  }

  async function handleDelete(row) {
    if (!confirm('Delete KISS config?')) return;
    await api.delete(`/kiss/${row.id}`);
    toasts.success('Deleted');
    items = await api.get('/kiss') || [];
  }
</script>

<PageHeader title="KISS Interfaces" subtitle="KISS TNC interface configuration">
  <Btn variant="primary" onclick={openCreate}>+ Add KISS</Btn>
</PageHeader>

<DataTable {columns} rows={items} onEdit={openEdit} onDelete={handleDelete} />

<Modal bind:open={modalOpen} title={editing ? 'Edit KISS' : 'New KISS Interface'}>
  <form onsubmit={handleSave}>
    <FormField label="Type" id="kiss-type">
      <SelectInput id="kiss-type" bind:value={form.type} options={typeOptions} />
    </FormField>
    {#if form.type === 'tcp'}
      <FormField label="TCP Port" id="kiss-port">
        <TextInput id="kiss-port" bind:value={form.tcp_port} type="number" placeholder="8001" />
      </FormField>
    {:else}
      <FormField label="Serial Device" id="kiss-serial">
        <TextInput id="kiss-serial" bind:value={form.serial_device} placeholder="/dev/ttyUSB0" />
      </FormField>
      <FormField label="Baud Rate" id="kiss-baud">
        <TextInput id="kiss-baud" bind:value={form.baud_rate} type="number" placeholder="9600" />
      </FormField>
    {/if}
    <div class="modal-actions">
      <Btn variant="default" onclick={() => modalOpen = false}>Cancel</Btn>
      <Btn variant="primary" type="submit">{editing ? 'Save' : 'Create'}</Btn>
    </div>
  </form>
</Modal>

<style>
  .modal-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 16px; }
</style>
