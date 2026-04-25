<script>
  import { onMount } from 'svelte';
  import { mapsState } from '../lib/settings/maps-store.svelte.js';
  import PageHeader from '../components/PageHeader.svelte';

  onMount(() => mapsState.fetchConfig());
</script>

<PageHeader title="Maps" subtitle="Choose your basemap source and register for private maps" />

<p>Maps Settings — under construction.</p>
