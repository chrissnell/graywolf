<script>
  import { onMount } from 'svelte';
  import { Button, Input, Toggle, Box } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';
  import FormField from '../components/FormField.svelte';

  let form = $state({ listen_addr: '0.0.0.0:8000', callsigns: 'N0CALL', enabled: false });
  let loading = $state(false);

  onMount(async () => {
    const data = await api.get('/agw');
    if (data) form = {
      listen_addr: data.listen_addr || '0.0.0.0:8000',
      callsigns: data.callsigns || 'N0CALL',
      enabled: data.enabled,
    };
  });

  async function handleSave(e) {
    e.preventDefault();
    loading = true;
    try {
      await api.put('/agw', {
        listen_addr: form.listen_addr,
        callsigns: form.callsigns,
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
      <FormField label="Listen Address" id="agw-addr">
        <Input id="agw-addr" bind:value={form.listen_addr} placeholder="0.0.0.0:8000" />
      </FormField>
      <FormField label="Callsigns" id="agw-calls">
        <Input id="agw-calls" bind:value={form.callsigns} placeholder="N0CALL" />
      </FormField>
    </div>
    <p class="hint">Comma-separated callsigns, one per AGW port.</p>
    <div class="form-actions">
      <Button variant="primary" type="submit" disabled={loading}>
        {loading ? 'Saving...' : 'Save'}
      </Button>
    </div>
  </form>
</Box>

<style>
  .form-actions { display: flex; justify-content: flex-end; margin-top: 16px; }
  .hint { font-size: 12px; color: var(--text-muted); margin: 4px 0 0; }
</style>
