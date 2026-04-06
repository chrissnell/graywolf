<script>
  import { onMount } from 'svelte';
  import { Button, Input, Toggle } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import DataTable from '../components/DataTable.svelte';
  import Modal from '../components/Modal.svelte';
  import FormField from '../components/FormField.svelte';

  let items = $state([]);
  let modalOpen = $state(false);
  let editing = $state(null);
  let form = $state({ channel_id: '1', txdelay: '300', txtail: '50', slottime: '100', persist: '63', duplex: false });
  let errors = $state({});

  const columns = [
    { key: 'channel_id', label: 'Channel' },
    { key: 'txdelay', label: 'TXDelay (ms)' },
    { key: 'txtail', label: 'TXTail (ms)' },
    { key: 'slottime', label: 'Slot Time (ms)' },
    { key: 'persist', label: 'Persist' },
    { key: 'duplex', label: 'Duplex' },
  ];

  onMount(async () => { items = await api.get('/tx-timing') || []; });

  function openCreate() {
    editing = null;
    form = { channel_id: '1', txdelay: '300', txtail: '50', slottime: '100', persist: '63', duplex: false };
    errors = {};
    modalOpen = true;
  }

  function openEdit(row) {
    editing = row;
    form = {
      channel_id: String(row.channel_id), txdelay: String(row.txdelay),
      txtail: String(row.txtail), slottime: String(row.slottime),
      persist: String(row.persist), duplex: row.duplex,
    };
    errors = {};
    modalOpen = true;
  }

  function validate() {
    const e = {};
    const p = parseInt(form.persist);
    if (p < 0 || p > 255) e.persist = 'Must be 0-255';
    errors = e;
    return Object.keys(e).length === 0;
  }

  async function handleSave(e) {
    e.preventDefault();
    if (!validate()) return;
    const data = {
      channel_id: parseInt(form.channel_id),
      txdelay: parseInt(form.txdelay), txtail: parseInt(form.txtail),
      slottime: parseInt(form.slottime), persist: parseInt(form.persist),
      duplex: form.duplex,
    };
    try {
      if (editing) {
        await api.put(`/tx-timing/${editing.id}`, data);
        toasts.success('TX timing updated');
      } else {
        await api.post('/tx-timing', data);
        toasts.success('TX timing created');
      }
      modalOpen = false;
      items = await api.get('/tx-timing') || [];
    } catch (err) {
      toasts.error(err.message);
    }
  }

  async function handleDelete(row) {
    if (!confirm('Delete TX timing config?')) return;
    await api.delete(`/tx-timing/${row.id}`);
    toasts.success('Deleted');
    items = await api.get('/tx-timing') || [];
  }
</script>

<PageHeader title="TX Timing" subtitle="Transmit timing parameters per channel">
  <Button variant="primary" onclick={openCreate}>+ Add TX Timing</Button>
</PageHeader>

<DataTable {columns} rows={items} onEdit={openEdit} onDelete={handleDelete} />

<Modal bind:open={modalOpen} title={editing ? 'Edit TX Timing' : 'New TX Timing'}>
  <form onsubmit={handleSave}>
    <FormField label="Channel ID" id="tx-ch">
      <Input id="tx-ch" bind:value={form.channel_id} type="number" />
    </FormField>
    <FormField label="TXDelay (ms)" id="tx-delay">
      <Input id="tx-delay" bind:value={form.txdelay} type="number" placeholder="300" />
    </FormField>
    <FormField label="TXTail (ms)" id="tx-tail">
      <Input id="tx-tail" bind:value={form.txtail} type="number" placeholder="50" />
    </FormField>
    <FormField label="Slot Time (ms)" id="tx-slot">
      <Input id="tx-slot" bind:value={form.slottime} type="number" placeholder="100" />
    </FormField>
    <FormField label="Persist (0-255)" error={errors.persist} id="tx-persist">
      <Input id="tx-persist" bind:value={form.persist} type="number" placeholder="63" />
    </FormField>
    <Toggle bind:checked={form.duplex} label="Duplex" />
    <div class="modal-actions">
      <Button onclick={() => modalOpen = false}>Cancel</Button>
      <Button variant="primary" type="submit">{editing ? 'Save' : 'Create'}</Button>
    </div>
  </form>
</Modal>

<style>
  .modal-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 16px; }
</style>
