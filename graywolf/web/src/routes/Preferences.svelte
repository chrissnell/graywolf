<script>
  import { Toggle, Box } from '@chrissnell/chonky-ui';
  import { unitsState } from '../lib/settings/units-store.svelte.js';
  import PageHeader from '../components/PageHeader.svelte';

  function toggleUnits(e) {
    unitsState.system = e.target.checked ? 'metric' : 'imperial';
  }
</script>

<PageHeader title="Preferences" subtitle="Display and formatting options" />

<Box title="Units">
  <Toggle checked={unitsState.isMetric} onchange={toggleUnits} label="Use metric units" />
  <p class="unit-hint">
    {#if unitsState.isMetric}
      Altitude in meters, distance in m/km, speed in km/h.
    {:else}
      Altitude in feet, distance in ft/mi, speed in mph.
    {/if}
  </p>
</Box>

<style>
  .unit-hint {
    margin-top: 12px;
    font-size: 13px;
    color: var(--text-muted);
  }
</style>
