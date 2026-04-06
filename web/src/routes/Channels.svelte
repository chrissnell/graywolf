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
  let audioDevices = $state([]);
  let modalOpen = $state(false);
  let editing = $state(null);
  let form = $state({
    name: '', input_device_id: '0', input_channel: '0',
    output_device_id: '0', output_channel: '0',
    modem_type: 'afsk', bit_rate: '1200', mark_freq: '1200', space_freq: '2200',
  });
  let errors = $state({});

  let inputDevices = $derived(audioDevices.filter(d => d.direction === 'input'));
  let outputDevices = $derived(audioDevices.filter(d => d.direction === 'output'));
  let inputDeviceOptions = $derived(inputDevices.map(d => ({ value: String(d.id), label: d.name })));
  let outputDeviceOptions = $derived([
    { value: '0', label: 'None (RX only)' },
    ...outputDevices.map(d => ({ value: String(d.id), label: d.name })),
  ]);

  const columns = [
    { key: 'name', label: 'Name' },
    { key: 'modem_type', label: 'Modem' },
    { key: 'bit_rate', label: 'Bit Rate' },
    { key: 'input_device_id', label: 'Input Device', format: (v) => {
      const d = audioDevices.find(d => d.id === v);
      return d ? `${d.name} (${v})` : String(v);
    }},
    { key: 'output_device_id', label: 'Output Device', format: (v) => {
      if (v === 0) return 'None';
      const d = audioDevices.find(d => d.id === v);
      return d ? `${d.name} (${v})` : String(v);
    }},
  ];

  const modemOptions = [
    { value: 'afsk', label: 'AFSK' },
    { value: 'gfsk', label: 'GFSK' },
    { value: 'psk', label: 'PSK' },
    { value: 'fsk', label: 'FSK' },
  ];

  const channelOptions = [
    { value: '0', label: '0 (Left/Mono)' },
    { value: '1', label: '1 (Right)' },
  ];

  onMount(async () => {
    await Promise.all([loadChannels(), loadDevices()]);
  });

  async function loadChannels() {
    channels = await api.get('/channels') || [];
  }

  async function loadDevices() {
    audioDevices = await api.get('/audio-devices') || [];
  }

  function openCreate() {
    editing = null;
    const defaultInput = inputDevices.length > 0 ? String(inputDevices[0].id) : '0';
    form = {
      name: '', input_device_id: defaultInput, input_channel: '0',
      output_device_id: '0', output_channel: '0',
      modem_type: 'afsk', bit_rate: '1200', mark_freq: '1200', space_freq: '2200',
    };
    errors = {};
    modalOpen = true;
  }

  function openEdit(row) {
    editing = row;
    form = {
      ...row,
      input_device_id: String(row.input_device_id),
      input_channel: String(row.input_channel),
      output_device_id: String(row.output_device_id),
      output_channel: String(row.output_channel),
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
    if (!form.input_device_id || form.input_device_id === '0') e.input_device_id = 'Required';
    errors = e;
    return Object.keys(e).length === 0;
  }

  async function handleSave() {
    if (!validate()) return;
    const data = {
      ...form,
      input_device_id: parseInt(form.input_device_id, 10),
      input_channel: parseInt(form.input_channel, 10),
      output_device_id: parseInt(form.output_device_id, 10),
      output_channel: parseInt(form.output_channel, 10),
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
  <FormField label="Name" error={errors.name} id="ch-name">
    <Input id="ch-name" bind:value={form.name} placeholder="VHF APRS" />
  </FormField>
  <FormField label="Input Device" error={errors.input_device_id} id="ch-indev">
    <Select id="ch-indev" bind:value={form.input_device_id} options={inputDeviceOptions} />
  </FormField>
  <FormField label="Input Channel" id="ch-inch">
    <Select id="ch-inch" bind:value={form.input_channel} options={channelOptions} />
  </FormField>
  <FormField label="Output Device" id="ch-outdev">
    <Select id="ch-outdev" bind:value={form.output_device_id} options={outputDeviceOptions} />
  </FormField>
  {#if form.output_device_id !== '0'}
    <FormField label="Output Channel" id="ch-outch">
      <Select id="ch-outch" bind:value={form.output_channel} options={channelOptions} />
    </FormField>
  {/if}
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
    <Button variant="primary" onclick={handleSave}>{editing ? 'Save' : 'Create'}</Button>
  </div>
</Modal>

<style>
  .modal-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    margin-top: 16px;
  }
</style>
