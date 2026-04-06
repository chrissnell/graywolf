<script>
  import { onMount } from 'svelte';
  import { Button, Input, Select, Box } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import FormField from '../components/FormField.svelte';

  let form = $state({
    source: 'serial', serial_port: '/dev/ttyACM0', baud_rate: '9600',
    gpsd_host: 'localhost', gpsd_port: '2947',
  });
  let loading = $state(false);

  const sourceOptions = [
    { value: 'serial', label: 'Serial Port' },
    { value: 'gpsd', label: 'GPSD' },
    { value: 'none', label: 'None' },
  ];

  onMount(async () => {
    const data = await api.get('/gps');
    if (data) form = {
      source: data.source, serial_port: data.serial_port,
      baud_rate: String(data.baud_rate), gpsd_host: data.gpsd_host,
      gpsd_port: String(data.gpsd_port),
    };
  });

  async function handleSave(e) {
    e.preventDefault();
    loading = true;
    try {
      await api.put('/gps', {
        ...form, baud_rate: parseInt(form.baud_rate), gpsd_port: parseInt(form.gpsd_port),
      });
      toasts.success('GPS config saved');
    } catch (err) {
      toasts.error(err.message);
    } finally {
      loading = false;
    }
  }
</script>

<PageHeader title="GPS" subtitle="GPS source configuration" />

<Box>
  <form onsubmit={handleSave}>
    <FormField label="Source" id="gps-source">
      <Select id="gps-source" bind:value={form.source} options={sourceOptions} />
    </FormField>
    {#if form.source === 'serial'}
      <FormField label="Serial Port" id="gps-serial">
        <Input id="gps-serial" bind:value={form.serial_port} placeholder="/dev/ttyACM0" />
      </FormField>
      <FormField label="Baud Rate" id="gps-baud">
        <Select id="gps-baud" bind:value={form.baud_rate} options={[
          { value: '4800', label: '4800' },
          { value: '9600', label: '9600' },
          { value: '38400', label: '38400' },
          { value: '115200', label: '115200' },
        ]} />
      </FormField>
    {:else if form.source === 'gpsd'}
      <FormField label="GPSD Host" id="gps-host">
        <Input id="gps-host" bind:value={form.gpsd_host} placeholder="localhost" />
      </FormField>
      <FormField label="GPSD Port" id="gps-port">
        <Input id="gps-port" bind:value={form.gpsd_port} type="number" placeholder="2947" />
      </FormField>
    {/if}
    <div class="form-actions">
      <Button variant="primary" type="submit" disabled={loading}>
        {loading ? 'Saving...' : 'Save'}
      </Button>
    </div>
  </form>
</Box>

<style>
  .form-actions { display: flex; justify-content: flex-end; margin-top: 16px; }
</style>
