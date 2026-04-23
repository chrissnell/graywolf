<script>
  import { onMount } from 'svelte';
  import { Toggle, Box } from '@chrissnell/chonky-ui';
  import { unitsState } from '../lib/settings/units-store.svelte.js';
  import { updates } from '../lib/updatesStore.svelte.js';
  import PageHeader from '../components/PageHeader.svelte';

  onMount(() => {
    updates.fetchConfig();
    unitsState.fetchConfig();
  });
</script>

<PageHeader title="Preferences" subtitle="Display and formatting options" />

<Box title="Units">
  <Toggle
    checked={unitsState.isMetric}
    onCheckedChange={(v) => unitsState.setSystem(v ? 'metric' : 'imperial')}
    label="Use metric units"
  />
  <p class="unit-hint">
    {#if unitsState.isMetric}
      Altitude in meters, distance in m/km, speed in km/h.
    {:else}
      Altitude in feet, distance in ft/mi, speed in mph.
    {/if}
  </p>
</Box>

<Box title="Updates">
  <Toggle
    checked={updates.enabled}
    onCheckedChange={(v) => updates.setEnabled(v)}
    label="Check for updates from GitHub"
  />
  <p class="update-hint">
    Contacts github.com once a day. Turn off for offline stations
    or to avoid sharing your IP.
  </p>
</Box>

<style>
  .unit-hint {
    margin-top: 12px;
    font-size: 13px;
    color: var(--text-muted);
  }
  .update-hint {
    margin-top: 12px;
    font-size: 13px;
    color: var(--text-muted);
  }
</style>
