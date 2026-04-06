<script>
  import { onMount } from 'svelte';
  import { Button, Input, Select } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import DataTable from '../components/DataTable.svelte';
  import Modal from '../components/Modal.svelte';
  import FormField from '../components/FormField.svelte';

  let items = $state([]);
  let modalOpen = $state(false);
  let editing = $state(null);
  let form = $state({ channel_id: '1', method: 'none', device_path: '', gpio_pin: '0' });
  let errors = $state({});

  const columns = [
    { key: 'channel_id', label: 'Channel' },
    { key: 'method', label: 'Method' },
    { key: 'device_path', label: 'Device' },
    { key: 'gpio_pin', label: 'GPIO Pin' },
  ];

  const methodOptions = [
    { value: 'none', label: 'None' },
    { value: 'serial_rts', label: 'Serial RTS' },
    { value: 'serial_dtr', label: 'Serial DTR' },
    { value: 'gpio', label: 'GPIO' },
    { value: 'cm108', label: 'CM108' },
  ];

  onMount(async () => { items = await api.get('/ptt') || []; });

  function openCreate() {
    editing = null;
    form = { channel_id: '1', method: 'none', device_path: '', gpio_pin: '0' };
    errors = {};
    modalOpen = true;
  }

  function openEdit(row) {
    editing = row;
    form = { ...row, channel_id: String(row.channel_id), gpio_pin: String(row.gpio_pin) };
    errors = {};
    modalOpen = true;
  }

  function validate() {
    const e = {};
    if (form.method !== 'none' && !form.device_path.trim()) e.device_path = 'Device path required for this method';
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
      items = await api.get('/ptt') || [];
    } catch (err) {
      toasts.error(err.message);
    }
  }

  async function handleDelete(row) {
    if (!confirm('Delete PTT config?')) return;
    await api.delete(`/ptt/${row.id}`);
    toasts.success('Deleted');
    items = await api.get('/ptt') || [];
  }
</script>

<PageHeader title="PTT Configuration" subtitle="Push-to-talk settings per channel">
  <Button variant="primary" onclick={openCreate}>+ Add PTT</Button>
</PageHeader>

<DataTable {columns} rows={items} onEdit={openEdit} onDelete={handleDelete} />

<Modal bind:open={modalOpen} title={editing ? 'Edit PTT' : 'New PTT Config'}>
  <form onsubmit={handleSave}>
    <FormField label="Channel ID" id="ptt-ch">
      <Input id="ptt-ch" bind:value={form.channel_id} type="number" />
    </FormField>
    <FormField label="Method" id="ptt-method">
      <Select id="ptt-method" bind:value={form.method} options={methodOptions} />
    </FormField>
    {#if form.method !== 'none'}
      <FormField label="Device Path" error={errors.device_path} id="ptt-dev">
        <Input id="ptt-dev" bind:value={form.device_path} placeholder="/dev/ttyUSB0" />
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

<style>
  .modal-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 16px; }
</style>
