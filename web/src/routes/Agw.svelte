<script>
  import { onMount } from 'svelte';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import Card from '../components/Card.svelte';
  import FormField from '../components/FormField.svelte';
  import TextInput from '../components/TextInput.svelte';
  import ToggleSwitch from '../components/ToggleSwitch.svelte';
  import Btn from '../components/Btn.svelte';

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

<Card>
  <form onsubmit={handleSave}>
    <ToggleSwitch bind:checked={form.enabled} label="Enable AGW interface" id="agw-enabled" />
    <div style="margin-top: 16px;">
      <FormField label="TCP Port" id="agw-port">
        <TextInput id="agw-port" bind:value={form.tcp_port} type="number" placeholder="8000" />
      </FormField>
      <FormField label="Monitor Port" id="agw-mon">
        <TextInput id="agw-mon" bind:value={form.monitor_port} type="number" placeholder="8002" />
      </FormField>
    </div>
    <div class="form-actions">
      <Btn variant="primary" type="submit" disabled={loading}>
        {loading ? 'Saving...' : 'Save'}
      </Btn>
    </div>
  </form>
</Card>

<style>
  .form-actions { display: flex; justify-content: flex-end; margin-top: 16px; }
</style>
