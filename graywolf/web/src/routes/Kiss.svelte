<script>
  import { onMount } from 'svelte';
  import { Button, Input, Select } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import DataTable from '../components/DataTable.svelte';
  import Modal from '../components/Modal.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import FormField from '../components/FormField.svelte';

  let items = $state([]);
  let channels = $state([]);
  let modalOpen = $state(false);
  let editing = $state(null);
  let form = $state({ type: 'tcp', tcp_port: '8001', serial_device: '', baud_rate: '9600', channel: '1' });

  // Delete-confirmation state (bound to ConfirmDialog)
  let confirmOpen = $state(false);
  let confirmMessage = $state('');
  let pendingDeleteId = $state(null);

  const columns = [
    { key: 'type', label: 'Type' },
    { key: 'tcp_port', label: 'TCP Port' },
    { key: 'serial_device', label: 'Serial Device' },
    { key: 'baud_rate', label: 'Baud Rate' },
    { key: 'channel', label: 'Channel' },
  ];

  const typeOptions = [
    { value: 'tcp', label: 'TCP' },
    { value: 'serial', label: 'Serial' },
  ];

  onMount(async () => {
    items = await api.get('/kiss') || [];
    channels = (await api.get('/channels') || []).map(c => ({ value: String(c.id), label: c.name || `Channel ${c.id}` }));
  });

  function openCreate() {
    editing = null;
    form = { type: 'tcp', tcp_port: '8001', serial_device: '', baud_rate: '9600', channel: '1' };
    modalOpen = true;
  }

  function openEdit(row) {
    editing = row;
    form = { ...row, tcp_port: String(row.tcp_port), baud_rate: String(row.baud_rate), channel: String(row.channel || 1) };
    modalOpen = true;
  }

  async function handleSave() {
    const data = { ...form, tcp_port: parseInt(form.tcp_port), baud_rate: parseInt(form.baud_rate), channel: parseInt(form.channel) };
    // Strip fields not in KissRequest DTO (backend rejects unknown fields)
    delete data.id;
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

  function describeRow(row) {
    if (row.type === 'tcp') return `TCP port ${row.tcp_port}`;
    if (row.type === 'serial') return `serial ${row.serial_device || ''}`.trim();
    return `#${row.id}`;
  }

  function handleDelete(row) {
    pendingDeleteId = row.id;
    confirmMessage = `Delete KISS interface (${describeRow(row)}) on channel ${row.channel}?`;
    confirmOpen = true;
  }

  async function confirmDelete() {
    const id = pendingDeleteId;
    pendingDeleteId = null;
    if (id == null) return;
    try {
      await api.delete(`/kiss/${id}`);
      toasts.success('Interface deleted');
      items = await api.get('/kiss') || [];
    } catch (err) {
      toasts.error(err.message);
    }
  }
</script>

<PageHeader title="KISS Interfaces" subtitle="KISS TNC interface configuration">
  <Button variant="primary" onclick={openCreate}>+ Add KISS</Button>
</PageHeader>

<DataTable {columns} rows={items} onEdit={openEdit} onDelete={handleDelete} />

<Modal bind:open={modalOpen} title={editing ? 'Edit KISS' : 'New KISS Interface'}>
    <FormField label="Type" id="kiss-type">
      <Select id="kiss-type" bind:value={form.type} options={typeOptions} />
    </FormField>
    {#if form.type === 'tcp'}
      <FormField label="TCP Port" id="kiss-port">
        <Input id="kiss-port" bind:value={form.tcp_port} type="number" placeholder="8001" />
      </FormField>
    {:else}
      <FormField label="Serial Device" id="kiss-serial">
        <Input id="kiss-serial" bind:value={form.serial_device} placeholder="/dev/ttyUSB0" />
      </FormField>
      <FormField label="Baud Rate" id="kiss-baud">
        <Input id="kiss-baud" bind:value={form.baud_rate} type="number" placeholder="9600" />
      </FormField>
    {/if}
    <FormField label="Channel" id="kiss-channel">
      {#if channels.length > 0}
        <Select id="kiss-channel" bind:value={form.channel} options={channels} />
      {:else}
        <Input id="kiss-channel" bind:value={form.channel} type="number" placeholder="1" />
      {/if}
    </FormField>
    <div class="modal-actions">
      <Button onclick={() => modalOpen = false}>Cancel</Button>
      <Button variant="primary" onclick={handleSave}>{editing ? 'Save' : 'Create'}</Button>
    </div>
</Modal>

<ConfirmDialog
  bind:open={confirmOpen}
  title="Delete Interface"
  message={confirmMessage}
  confirmLabel="Delete"
  onConfirm={confirmDelete}
/>

<style>
  .modal-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 16px; }
</style>
