<script>
  import { onMount } from 'svelte';
  import { Button, Input, Select } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import DataTable from '../components/DataTable.svelte';
  import Modal from '../components/Modal.svelte';
  import FormField from '../components/FormField.svelte';

  let channels = $state([]);
  let modalOpen = $state(false);
  let editing = $state(null);
  let form = $state({
    name: '', audio_device_id: '1', audio_channel: '0',
    modem_type: 'afsk', bit_rate: '1200', mark_freq: '1200', space_freq: '2200',
  });
  let errors = $state({});

  const columns = [
    { key: 'name', label: 'Name' },
    { key: 'modem_type', label: 'Modem' },
    { key: 'bit_rate', label: 'Bit Rate' },
    { key: 'audio_device_id', label: 'Device ID' },
    { key: 'audio_channel', label: 'Audio Ch' },
  ];

  const modemOptions = [
    { value: 'afsk', label: 'AFSK' },
    { value: 'gfsk', label: 'GFSK' },
    { value: 'psk', label: 'PSK' },
    { value: 'fsk', label: 'FSK' },
  ];

  onMount(loadChannels);

  async function loadChannels() {
    channels = await api.get('/channels') || [];
  }

  function openCreate() {
    editing = null;
    form = {
      name: '', audio_device_id: '1', audio_channel: '0',
      modem_type: 'afsk', bit_rate: '1200', mark_freq: '1200', space_freq: '2200',
    };
    errors = {};
    modalOpen = true;
  }

  function openEdit(row) {
    editing = row;
    form = {
      ...row,
      audio_device_id: String(row.audio_device_id),
      audio_channel: String(row.audio_channel),
      bit_rate: String(row.bit_rate),
      mark_freq: String(row.mark_freq),
      space_freq: String(row.space_freq),
    };
    errors = {};
    modalOpen = true;
  }

  function validate() {
    const e = {};
    if (!form.name.trim()) e.name = 'Required';
    errors = e;
    return Object.keys(e).length === 0;
  }

  async function handleSave(e) {
    e.preventDefault();
    if (!validate()) return;
    const data = {
      ...form,
      audio_device_id: parseInt(form.audio_device_id, 10),
      audio_channel: parseInt(form.audio_channel, 10),
      bit_rate: parseInt(form.bit_rate, 10),
      mark_freq: parseInt(form.mark_freq, 10),
      space_freq: parseInt(form.space_freq, 10),
    };
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
  <Button variant="primary" onclick={openCreate}>+ Add Channel</Button>
</PageHeader>

<DataTable {columns} rows={channels} onEdit={openEdit} onDelete={handleDelete} />

<Modal bind:open={modalOpen} title={editing ? 'Edit Channel' : 'New Channel'}>
  <form onsubmit={handleSave}>
    <FormField label="Name" error={errors.name} id="ch-name">
      <Input id="ch-name" bind:value={form.name} placeholder="VHF APRS" />
    </FormField>
    <FormField label="Audio Device ID" id="ch-dev">
      <Input id="ch-dev" bind:value={form.audio_device_id} type="number" placeholder="1" />
    </FormField>
    <FormField label="Audio Channel" id="ch-ach">
      <Select id="ch-ach" bind:value={form.audio_channel} options={[
        { value: '0', label: '0 (Left/Mono)' },
        { value: '1', label: '1 (Right)' },
      ]} />
    </FormField>
    <FormField label="Modem Type" id="ch-modem">
      <Select id="ch-modem" bind:value={form.modem_type} options={modemOptions} />
    </FormField>
    <FormField label="Bit Rate" id="ch-baud">
      <Input id="ch-baud" bind:value={form.bit_rate} type="number" placeholder="1200" />
    </FormField>
    <FormField label="Mark Freq (Hz)" id="ch-mark">
      <Input id="ch-mark" bind:value={form.mark_freq} type="number" placeholder="1200" />
    </FormField>
    <FormField label="Space Freq (Hz)" id="ch-space">
      <Input id="ch-space" bind:value={form.space_freq} type="number" placeholder="2200" />
    </FormField>
    <div class="modal-actions">
      <Button onclick={() => modalOpen = false}>Cancel</Button>
      <Button variant="primary" type="submit">{editing ? 'Save' : 'Create'}</Button>
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
