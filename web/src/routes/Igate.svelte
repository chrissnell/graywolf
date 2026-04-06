<script>
  import { onMount } from 'svelte';
  import { Button, Input, Toggle, Box } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import FormField from '../components/FormField.svelte';

  let form = $state({
    enabled: true, server: 'rotate.aprs2.net', port: '14580',
    callsign: '', passcode: '', filter: '',
  });
  let loading = $state(false);

  onMount(async () => {
    const data = await api.get('/igate/config');
    if (data) form = { ...data, port: String(data.port) };
  });

  function validate() {
    if (form.enabled && !form.callsign.trim()) return false;
    return true;
  }

  async function handleSave(e) {
    e.preventDefault();
    if (!validate()) {
      toasts.error('Callsign is required when iGate is enabled');
      return;
    }
    loading = true;
    try {
      await api.put('/igate/config', { ...form, port: parseInt(form.port) });
      toasts.success('iGate config saved');
    } catch (err) {
      toasts.error(err.message);
    } finally {
      loading = false;
    }
  }
</script>

<PageHeader title="iGate" subtitle="Internet gateway configuration" />

<Box>
  <form onsubmit={handleSave}>
    <Toggle bind:checked={form.enabled} label="Enable iGate" />
    <div style="margin-top: 16px;">
      <FormField label="APRS-IS Server" id="ig-server">
        <Input id="ig-server" bind:value={form.server} placeholder="rotate.aprs2.net" />
      </FormField>
      <FormField label="Port" id="ig-port">
        <Input id="ig-port" bind:value={form.port} type="number" placeholder="14580" />
      </FormField>
      <FormField label="Callsign" id="ig-call">
        <Input id="ig-call" bind:value={form.callsign} placeholder="N0CALL-10" />
      </FormField>
      <FormField label="Passcode" id="ig-pass">
        <Input id="ig-pass" bind:value={form.passcode} type="password" placeholder="12345" />
      </FormField>
      <FormField label="Server Filter" id="ig-filter">
        <Input id="ig-filter" bind:value={form.filter} placeholder="r/35.0/-106.0/100" />
      </FormField>
    </div>
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
