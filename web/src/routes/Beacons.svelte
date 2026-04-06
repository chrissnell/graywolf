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

  let beacons = $state([]);
  let smartBeacon = $state({
    enabled: false, fast_speed: '60', fast_rate: '60', slow_speed: '5', slow_rate: '1800',
    min_turn_angle: '28', turn_slope: '26', min_turn_time: '30',
  });
  let modalOpen = $state(false);
  let editing = $state(null);
  let form = $state({ callsign: '', destination: 'APGW00', path: 'WIDE1-1,WIDE2-1', comment: '', interval: '600', enabled: true });
  let savingSB = $state(false);

  const columns = [
    { key: 'callsign', label: 'Callsign' },
    { key: 'destination', label: 'Destination' },
    { key: 'path', label: 'Path' },
    { key: 'interval', label: 'Interval (s)' },
    { key: 'enabled', label: 'Enabled' },
  ];

  onMount(async () => {
    beacons = await api.get('/beacons') || [];
    const sb = await api.get('/smart-beacon');
    if (sb) smartBeacon = {
      enabled: sb.enabled,
      fast_speed: String(sb.fast_speed), fast_rate: String(sb.fast_rate),
      slow_speed: String(sb.slow_speed), slow_rate: String(sb.slow_rate),
      min_turn_angle: String(sb.min_turn_angle), turn_slope: String(sb.turn_slope),
      min_turn_time: String(sb.min_turn_time),
    };
  });

  function openCreate() {
    editing = null;
    form = { callsign: '', destination: 'APGW00', path: 'WIDE1-1,WIDE2-1', comment: '', interval: '600', enabled: true };
    modalOpen = true;
  }

  function openEdit(row) {
    editing = row;
    form = { ...row, interval: String(row.interval) };
    modalOpen = true;
  }

  async function handleSave(e) {
    e.preventDefault();
    if (!form.callsign.trim()) { toasts.error('Callsign required'); return; }
    const data = { ...form, interval: parseInt(form.interval) };
    try {
      if (editing) {
        await api.put(`/beacons/${editing.id}`, data);
        toasts.success('Beacon updated');
      } else {
        await api.post('/beacons', data);
        toasts.success('Beacon created');
      }
      modalOpen = false;
      beacons = await api.get('/beacons') || [];
    } catch (err) {
      toasts.error(err.message);
    }
  }

  async function handleDelete(row) {
    if (!confirm(`Delete beacon for ${row.callsign}?`)) return;
    await api.delete(`/beacons/${row.id}`);
    toasts.success('Deleted');
    beacons = await api.get('/beacons') || [];
  }

  async function saveSmartBeacon(e) {
    e.preventDefault();
    savingSB = true;
    try {
      await api.put('/smart-beacon', {
        enabled: smartBeacon.enabled,
        fast_speed: parseInt(smartBeacon.fast_speed),
        fast_rate: parseInt(smartBeacon.fast_rate),
        slow_speed: parseInt(smartBeacon.slow_speed),
        slow_rate: parseInt(smartBeacon.slow_rate),
        min_turn_angle: parseInt(smartBeacon.min_turn_angle),
        turn_slope: parseInt(smartBeacon.turn_slope),
        min_turn_time: parseInt(smartBeacon.min_turn_time),
      });
      toasts.success('SmartBeaconing saved');
    } catch (err) {
      toasts.error(err.message);
    } finally {
      savingSB = false;
    }
  }
</script>

<PageHeader title="Beacons" subtitle="APRS beacon configuration">
  <Btn variant="primary" onclick={openCreate}>+ Add Beacon</Btn>
</PageHeader>

<DataTable {columns} rows={beacons} onEdit={openEdit} onDelete={handleDelete} />

<div style="margin-top: 24px;">
  <Card title="SmartBeaconing">
    <form onsubmit={saveSmartBeacon}>
      <ToggleSwitch bind:checked={smartBeacon.enabled} label="Enable SmartBeaconing" id="sb-on" />
      <div class="sb-grid">
        <FormField label="Fast Speed (mph)" id="sb-fspd">
          <TextInput id="sb-fspd" bind:value={smartBeacon.fast_speed} type="number" />
        </FormField>
        <FormField label="Fast Rate (s)" id="sb-frate">
          <TextInput id="sb-frate" bind:value={smartBeacon.fast_rate} type="number" />
        </FormField>
        <FormField label="Slow Speed (mph)" id="sb-sspd">
          <TextInput id="sb-sspd" bind:value={smartBeacon.slow_speed} type="number" />
        </FormField>
        <FormField label="Slow Rate (s)" id="sb-srate">
          <TextInput id="sb-srate" bind:value={smartBeacon.slow_rate} type="number" />
        </FormField>
        <FormField label="Min Turn Angle" id="sb-angle">
          <TextInput id="sb-angle" bind:value={smartBeacon.min_turn_angle} type="number" />
        </FormField>
        <FormField label="Turn Slope" id="sb-slope">
          <TextInput id="sb-slope" bind:value={smartBeacon.turn_slope} type="number" />
        </FormField>
        <FormField label="Min Turn Time (s)" id="sb-ttime">
          <TextInput id="sb-ttime" bind:value={smartBeacon.min_turn_time} type="number" />
        </FormField>
      </div>
      <div class="form-actions">
        <Btn variant="primary" type="submit" disabled={savingSB}>Save SmartBeaconing</Btn>
      </div>
    </form>
  </Card>
</div>

<Modal bind:open={modalOpen} title={editing ? 'Edit Beacon' : 'New Beacon'}>
  <form onsubmit={handleSave}>
    <FormField label="Callsign" id="bcn-call">
      <TextInput id="bcn-call" bind:value={form.callsign} placeholder="N0CALL-9" />
    </FormField>
    <FormField label="Destination" id="bcn-dest">
      <TextInput id="bcn-dest" bind:value={form.destination} placeholder="APGW00" />
    </FormField>
    <FormField label="Path" id="bcn-path">
      <TextInput id="bcn-path" bind:value={form.path} placeholder="WIDE1-1,WIDE2-1" />
    </FormField>
    <FormField label="Comment" id="bcn-comment">
      <TextInput id="bcn-comment" bind:value={form.comment} placeholder="graywolf" />
    </FormField>
    <FormField label="Interval (seconds)" id="bcn-interval">
      <TextInput id="bcn-interval" bind:value={form.interval} type="number" placeholder="600" />
    </FormField>
    <ToggleSwitch bind:checked={form.enabled} label="Enabled" id="bcn-on" />
    <div class="modal-actions">
      <Btn variant="default" onclick={() => modalOpen = false}>Cancel</Btn>
      <Btn variant="primary" type="submit">{editing ? 'Save' : 'Create'}</Btn>
    </div>
  </form>
</Modal>

<style>
  .sb-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
    gap: 0 16px;
    margin-top: 12px;
  }
  .form-actions { display: flex; justify-content: flex-end; margin-top: 16px; }
  .modal-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 16px; }
</style>
