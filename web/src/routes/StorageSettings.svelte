<script>
  import { onMount } from 'svelte';
  import { Box } from '@chrissnell/chonky-ui';
  import { storageUsageState, colorForKey } from '../lib/settings/storage-usage-store.svelte.js';
  import { formatBytes } from '../lib/maps/format-bytes.js';
  import PageHeader from '../components/PageHeader.svelte';

  onMount(() => {
    storageUsageState.refresh();
  });

  // Bar segments as percentages of the total. When nothing is stored
  // yet the bar renders empty rather than dividing by zero.
  let segments = $derived.by(() => {
    const total = storageUsageState.totalBytes;
    return storageUsageState.locations.map((l) => ({
      ...l,
      color: colorForKey(l.key),
      pct: total > 0 ? (l.bytes / total) * 100 : 0,
    }));
  });
</script>

<PageHeader title="Storage" subtitle="Where Graywolf keeps offline maps and history" />

<Box title="Storage usage">
  {#if storageUsageState.error && !storageUsageState.loaded}
    <p class="usage-error">
      Couldn't read storage usage. <button class="linklike" onclick={() => storageUsageState.refresh()}>Retry</button>
    </p>
  {:else}
    <div class="usage-bar" aria-hidden="true">
      {#each segments as seg (seg.key)}
        {#if seg.pct > 0}
          <div class="usage-seg" style="width:{seg.pct}%; background:{seg.color};"></div>
        {/if}
      {/each}
    </div>

    <ul class="usage-list">
      {#each segments as seg (seg.key)}
        <li class="usage-row">
          <span class="usage-swatch" style="background:{seg.color};"></span>
          <span class="usage-name">{seg.label}</span>
          <span class="usage-bytes">{formatBytes(seg.bytes)}</span>
        </li>
      {/each}
    </ul>

    <div class="usage-total">
      <span>Total</span>
      <span class="usage-bytes">{formatBytes(storageUsageState.totalBytes)}</span>
    </div>

    {#if storageUsageState.error}
      <p class="usage-stale">
        Showing last known figures — couldn't refresh.
        <button class="linklike" onclick={() => storageUsageState.refresh()}>Retry</button>
      </p>
    {/if}
  {/if}
</Box>

<Box title="Data location">
  <p class="loc-hint">
    These are the paths Graywolf reads and writes on this device — useful
    if you want to find or back up your data.
  </p>
  <ul class="loc-list">
    {#each storageUsageState.locations as loc (loc.key)}
      <li class="loc-row">
        <span class="loc-name">{loc.label}</span>
        <code class="loc-path">{loc.path}</code>
      </li>
    {/each}
  </ul>
</Box>

<style>
  .usage-bar {
    display: flex;
    width: 100%;
    height: 16px;
    border-radius: 5px;
    overflow: hidden;
    background: var(--color-surface-raised);
    margin-bottom: 16px;
  }
  .usage-seg {
    height: 100%;
  }
  .usage-list {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .usage-row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 6px 0;
  }
  .usage-swatch {
    width: 12px;
    height: 12px;
    border-radius: 3px;
    flex: none;
  }
  .usage-name {
    flex: 1;
  }
  .usage-bytes {
    color: var(--color-text);
    font-variant-numeric: tabular-nums;
  }
  .usage-total {
    display: flex;
    justify-content: space-between;
    margin-top: 10px;
    padding-top: 10px;
    border-top: 1px solid var(--color-border);
    color: var(--color-text-muted);
  }
  .usage-total .usage-bytes {
    font-weight: bold;
  }
  .usage-error {
    color: var(--color-text-muted);
  }
  .usage-stale {
    color: var(--color-text-dim);
    font-size: 0.9em;
    margin-top: 12px;
    margin-bottom: 0;
  }
  .linklike {
    background: none;
    border: none;
    color: var(--color-primary);
    cursor: pointer;
    padding: 0;
    font: inherit;
  }
  .loc-hint {
    color: var(--color-text-muted);
    margin-top: 0;
  }
  .loc-list {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .loc-row {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 6px 0;
  }
  .loc-path {
    color: var(--color-text-dim);
    word-break: break-all;
  }
</style>
