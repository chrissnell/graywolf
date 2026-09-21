<script>
  import { Box, Select } from '@chrissnell/chonky-ui';
  import { navProviderState, NAV_PROVIDERS } from '../lib/settings/navigation-store.svelte.js';
  import PageHeader from '../components/PageHeader.svelte';

  const options = NAV_PROVIDERS.map((p) => ({ value: p.value, label: p.label }));
</script>

<PageHeader title="Navigation" subtitle="Choose the map app used by the live map's Navigate action" />

<Box title="Navigation app">
  <Select
    value={navProviderState.provider}
    onValueChange={(v) => (navProviderState.provider = v)}
    options={options}
  />
  <p class="nav-hint">
    Used by the "Navigate" action on station and object popups on the live
    map. Organic Maps supports offline maps; Google Maps and Apple Maps are
    common alternatives. Defaults to the app suited to this device until
    changed here.
  </p>
</Box>

<style>
  .nav-hint {
    margin-top: 12px;
    font-size: 13px;
    color: var(--text-muted);
  }
</style>
