<script>
  import { onMount } from 'svelte';
  import { Box, Toggle } from '@chrissnell/chonky-ui';
  import { mapsState } from '../lib/settings/maps-store.svelte.js';
  import PageHeader from '../components/PageHeader.svelte';

  let consented = $state(false);

  onMount(() => mapsState.fetchConfig());
</script>

<PageHeader title="Maps" subtitle="Choose your basemap source" />

{#if !mapsState.registered}
  <Box title="About Graywolf private maps">
    <p class="prose">
      Graywolf can use a private, prettier basemap hosted by the project author,
      <strong>Chris Snell (NW5W)</strong>. Chris pays for the hosting and bandwidth
      personally, and provides this map to the amateur radio community at no cost.
    </p>
    <p class="prose">
      To prevent abuse from non-amateur clients, the map server requires a one-time
      registration per device.
    </p>
    <h3 class="prose-heading">What is sent during registration</h3>
    <ul class="prose-list">
      <li>Your callsign (uppercase, without -SSID).</li>
      <li>Your IP address, captured by the server.</li>
    </ul>
    <p class="prose">
      Nothing else. No email, no name, no metadata. Each install registers
      independently -- your laptop and your tablet each get their own token.
    </p>
    <Toggle
      checked={consented}
      onCheckedChange={(v) => (consented = v)}
      label="I understand and agree."
    />
  </Box>
{/if}

<style>
  @import '../lib/maps/styles.css';
</style>
