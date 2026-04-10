<script>
  import { onMount } from 'svelte';
  import { Button, Toggle, Box } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import PageHeader from '../components/PageHeader.svelte';

  let enabled = $state(false);

  onMount(async () => {
    const data = await api.get('/igate/config');
    if (data) {
      enabled = data.simulation_mode || false;
    }
  });

  async function toggle() {
    try {
      await api.post('/igate/simulation', { enabled });
      toasts.success(enabled ? 'Simulation enabled' : 'Simulation disabled');
    } catch (err) {
      toasts.error(err.message);
    }
  }


</script>

<PageHeader title="Simulation" subtitle="Simulation mode for testing without RF" />

<Box>
  <div class="sim-toggle">
    <Toggle bind:checked={enabled} label="Enable simulation mode" />
    <Button onclick={toggle}>Apply</Button>
  </div>
  <p class="sim-note">
    When enabled, packets are processed without actual RF transmission/reception.
    Useful for testing configurations.
  </p>
</Box>

<style>
  .sim-toggle {
    display: flex;
    align-items: center;
    gap: 16px;
  }
  .sim-note {
    margin-top: 12px;
    font-size: 13px;
    color: var(--text-muted);
  }
</style>
