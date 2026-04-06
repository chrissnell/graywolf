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

  let channels = $state([]);
  let modalOpen = $state(false);
  let editing = $state(null);
  let form = $state({ name: '', frequency: '', modem_type: 'afsk1200', baud_rate: '1200', device: '', enabled: true });
  let errors = $state({});

  const columns = [
    { key: 'name', label: 'Name' },
    { key: 'frequency', label: 'Frequency' },
    { key: 'modem_type', label: 'Modem' },
    { key: 'baud_rate', label: 'Baud Rate' },
    { key: 'device', label: 'Device' },
    { key: 'enabled', label: 'Enabled' },
  ];

  const modemOptions = [
    { value: 'afsk1200', label: 'AFSK 1200' },
    { value: 'gfsk9600', label: 'GFSK 9600' },
    { value: 'psk31', label: 'PSK31' },
    { value: 'fsk300', label: 'FSK 300' },
  ];

  onMount(loadChannels);

  async function loadChannels() {
    channels = await api.get('/channels') || [];
  }

  function openCreate() {
    editing = null;
    form = { name: '', frequency: '', modem_type: 'afsk1200', baud_rate: '1200', device: '', enabled: true };
    errors = {};
    modalOpen = true;
  }

  function openEdit(row) {
    editing = row;
    form = { ...row, baud_rate: String(row.baud_rate) };
    errors = {};
    modalOpen = true;
  }

  function validate() {
    const e = {};
    if (!form.name.trim()) e.name = 'Required';
    if (!form.frequency.trim()) e.frequency = 'Required';
    if (!form.device.trim()) e.device = 'Required';
    errors = e;
    return Object.keys(e).length === 0;
  }

  async function handleSave(e) {
    e.preventDefault();
    if (!validate()) return;
    const data = { ...form, baud_rate: parseInt(form.baud_rate, 10) };
    try {
      if (editing) {
        await api.put(`/channels/${editing.id}`, data);
        toasts.success('Channel updated');
      } else {
        await api.post('/channels', data);
        toasts.success('Channel created');
      }
      modalOpen = false;
      await loadChannels();
    } catch (err) {
      toasts.error(err.message);
    }
  }

  async function handleDelete(row) {
    if (!confirm(`Delete channel "${row.name}"?`)) return;
    try {
      await api.delete(`/channels/${row.id}`);
      toasts.success('Channel deleted');
      await loadChannels();
    } catch (err) {
      toasts.error(err.message);
    }
  }
</script>

<PageHeader title="Channels" subtitle="Radio channel configuration">
  <Btn variant="primary" onclick={openCreate}>+ Add Channel</Btn>
</PageHeader>

<DataTable {columns} rows={channels} onEdit={openEdit} onDelete={handleDelete} />

<Modal bind:open={modalOpen} title={editing ? 'Edit Channel' : 'New Channel'}>
  <form onsubmit={handleSave}>
    <FormField label="Name" error={errors.name} id="ch-name">
      <TextInput id="ch-name" bind:value={form.name} placeholder="VHF APRS" />
    </FormField>
    <FormField label="Frequency" error={errors.frequency} id="ch-freq">
      <TextInput id="ch-freq" bind:value={form.frequency} placeholder="144.390" />
    </FormField>
    <FormField label="Modem Type" id="ch-modem">
      <SelectInput id="ch-modem" bind:value={form.modem_type} options={modemOptions} />
    </FormField>
    <FormField label="Baud Rate" id="ch-baud">
      <TextInput id="ch-baud" bind:value={form.baud_rate} type="number" placeholder="1200" />
    </FormField>
    <FormField label="Audio Device" error={errors.device} id="ch-dev">
      <TextInput id="ch-dev" bind:value={form.device} placeholder="hw:0" />
    </FormField>
    <ToggleSwitch bind:checked={form.enabled} label="Enabled" id="ch-enabled" />
    <div class="modal-actions">
      <Btn variant="default" onclick={() => modalOpen = false}>Cancel</Btn>
      <Btn variant="primary" type="submit">{editing ? 'Save' : 'Create'}</Btn>
    </div>
  </form>
</Modal>

<style>
  .modal-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    margin-top: 16px;
  }
</style>
