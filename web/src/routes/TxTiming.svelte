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
  let form = $state({ channel: '1', tx_delay_ms: '300', tx_tail_ms: '50', slot_ms: '100', persist: '63', full_dup: false });
  let errors = $state({});

  const columns = [
    { key: 'channel', label: 'Channel' },
    { key: 'tx_delay_ms', label: 'TXDelay (ms)' },
    { key: 'tx_tail_ms', label: 'TXTail (ms)' },
    { key: 'slot_ms', label: 'Slot Time (ms)' },
    { key: 'persist', label: 'Persist' },
    { key: 'full_dup', label: 'Duplex' },
  ];

  onMount(async () => { items = await api.get('/tx-timing') || []; });

  function openCreate() {
    editing = null;
    form = { channel: '1', tx_delay_ms: '300', tx_tail_ms: '50', slot_ms: '100', persist: '63', full_dup: false };
    errors = {};
    modalOpen = true;
  }

  function openEdit(row) {
    editing = row;
    form = {
      channel: String(row.channel), tx_delay_ms: String(row.tx_delay_ms),
      tx_tail_ms: String(row.tx_tail_ms), slot_ms: String(row.slot_ms),
      persist: String(row.persist), full_dup: row.full_dup,
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

  async function handleSave() {
    if (!validate()) return;
    const data = {
      channel: parseInt(form.channel),
      tx_delay_ms: parseInt(form.tx_delay_ms), tx_tail_ms: parseInt(form.tx_tail_ms),
      slot_ms: parseInt(form.slot_ms), persist: parseInt(form.persist),
      full_dup: form.full_dup,
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
    <FormField label="Channel" id="tx-ch">
      <Input id="tx-ch" bind:value={form.channel} type="number" />
    </FormField>
    <FormField label="TXDelay (ms)" id="tx-delay">
      <Input id="tx-delay" bind:value={form.tx_delay_ms} type="number" placeholder="300" />
    </FormField>
    <FormField label="TXTail (ms)" id="tx-tail">
      <Input id="tx-tail" bind:value={form.tx_tail_ms} type="number" placeholder="50" />
    </FormField>
    <FormField label="Slot Time (ms)" id="tx-slot">
      <Input id="tx-slot" bind:value={form.slot_ms} type="number" placeholder="100" />
    </FormField>
    <FormField label="Persist (0-255)" error={errors.persist} id="tx-persist">
      <Input id="tx-persist" bind:value={form.persist} type="number" placeholder="63" />
    </FormField>
    <Toggle bind:checked={form.full_dup} label="Duplex" />
    <div class="modal-actions">
      <Button onclick={() => modalOpen = false}>Cancel</Button>
      <Button variant="primary" onclick={handleSave}>{editing ? 'Save' : 'Create'}</Button>
    </div>
</Modal>

<style>
  .modal-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 16px; }
</style>
