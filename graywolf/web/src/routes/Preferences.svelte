<script>
  import { Toggle, Box } from '@chrissnell/chonky-ui';
  import { unitsState } from '../lib/settings/units-store.svelte.js';
  import PageHeader from '../components/PageHeader.svelte';

  let metric = $state(unitsState.isMetric);

  $effect(() => {
    unitsState.system = metric ? 'metric' : 'imperial';
  });
</script>

<PageHeader title="Preferences" subtitle="Display and formatting options" />

<Box title="Units">
  <Toggle bind:checked={metric} label="Use metric units" />
  <p class="unit-hint">
    {#if metric}
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
