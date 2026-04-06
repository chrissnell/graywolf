<script>
  import { onMount } from 'svelte';
  import { Button, Input, Toggle, Box } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import FormField from '../components/FormField.svelte';

  let form = $state({ tcp_port: '8000', monitor_port: '8002', enabled: true });
  let loading = $state(false);

  onMount(async () => {
    const data = await api.get('/agw');
    if (data) form = { tcp_port: String(data.tcp_port), monitor_port: String(data.monitor_port), enabled: data.enabled };
  });

  async function handleSave(e) {
    e.preventDefault();
    loading = true;
    try {
      await api.put('/agw', {
        tcp_port: parseInt(form.tcp_port),
        monitor_port: parseInt(form.monitor_port),
        enabled: form.enabled,
      });
      toasts.success('AGW config saved');
    } catch (err) {
      toasts.error(err.message);
    } finally {
      loading = false;
    }
  }
</script>

<PageHeader title="AGW Interface" subtitle="AGWPE-compatible interface configuration" />

<Box>
  <form onsubmit={handleSave}>
    <Toggle bind:checked={form.enabled} label="Enable AGW interface" />
    <div style="margin-top: 16px;">
      <FormField label="TCP Port" id="agw-port">
        <Input id="agw-port" bind:value={form.tcp_port} type="number" placeholder="8000" />
      </FormField>
      <FormField label="Monitor Port" id="agw-mon">
        <Input id="agw-mon" bind:value={form.monitor_port} type="number" placeholder="8002" />
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
