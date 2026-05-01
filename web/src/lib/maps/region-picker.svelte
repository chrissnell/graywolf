<script>
  import { onMount } from 'svelte';
  import { Drawer, Button, Input } from '@chrissnell/chonky-ui';
  import { catalogStore } from './catalog-store.svelte.js';
  import { buildCountryTree } from './catalog-tree.js';
  import { downloadsState } from './downloads-store.svelte.js';
  import { formatBytes } from './format-bytes.js';

  let { open = $bindable(false) } = $props();
  let query = $state('');
  let expanded = $state(new Set()); // Set<iso2>

  onMount(() => { catalogStore.load(); });

  let tree = $derived.by(() => {
    if (!catalogStore.catalog) return [];
    return buildCountryTree(catalogStore.catalog);
  });

  // Filter: when there is a query, expand any country with a matching
  // child (or whose own name matches) and only display matching leaves.
  let filteredTree = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return tree;
    const out = [];
    for (const country of tree) {
      const countryHit = country.name.toLowerCase().includes(q);
      const childHits = country.children.filter(
        (c) => c.name.toLowerCase().includes(q) || c.slug.includes(q),
      );
      if (countryHit || childHits.length > 0) {
        out.push({ ...country, children: countryHit ? country.children : childHits });
      }
    }
    return out;
  });

  function statusOf(slug) { return downloadsState.items.get(slug); }
  function toggle(iso2) {
    const s = new Set(expanded);
    if (s.has(iso2)) s.delete(iso2); else s.add(iso2);
    expanded = s;
  }
  function isExpanded(iso2) {
    if (query.trim()) return true; // expand all on search
    return expanded.has(iso2);
  }
</script>

<Drawer bind:open anchor="bottom">
  <Drawer.Header>
    <h2 class="picker-title">Offline maps</h2>
    <Drawer.Close aria-label="Close">×</Drawer.Close>
  </Drawer.Header>
  <Drawer.Body>
    <div class="picker-search">
      <Input
        type="text"
        placeholder="Search countries, states, provinces..."
        bind:value={query}
        autocomplete="off"
        spellcheck={false}
      />
    </div>

    {#if catalogStore.loading && !catalogStore.catalog}
      <p class="form-hint">Loading catalog...</p>
    {:else if catalogStore.error}
      <p class="form-hint form-hint-error">{catalogStore.error}</p>
    {:else}
      <ul class="region-list" role="list">
        {#each filteredTree as country (country.iso2)}
          {@const cItem = statusOf(country.slug)}
          {@const cStatus = cItem?.state ?? 'absent'}
          <li class="region-country">
            <div class="region-row" data-level="country">
              <button
                class="region-toggle"
                type="button"
                onclick={() => toggle(country.iso2)}
                disabled={country.synthetic && country.children.length === 0}
                aria-expanded={isExpanded(country.iso2)}
              >
                <span class="region-caret">{isExpanded(country.iso2) ? '▾' : '▸'}</span>
                <span class="region-name">{country.name}</span>
              </button>
              <div class="region-meta">
                {#if !country.synthetic && country.sizeBytes > 0}
                  <span class="region-size">{formatBytes(country.sizeBytes)}</span>
                {/if}
                {#if cStatus === 'downloading'}
                  <span class="region-status">{Math.round((cItem.bytes_downloaded / Math.max(1, cItem.bytes_total)) * 100)}%</span>
                {:else if cStatus === 'complete'}
                  <span class="region-status complete">Downloaded</span>
                {/if}
              </div>
              <div class="region-actions">
                {#if !country.synthetic}
                  {#if cStatus === 'absent' || cStatus === 'error'}
                    <Button onclick={() => downloadsState.start(country.slug)}>Download</Button>
                  {:else if cStatus === 'complete'}
                    <Button variant="danger" onclick={() => downloadsState.remove(country.slug)}>Delete</Button>
                  {:else}
                    <Button variant="default" disabled>Downloading...</Button>
                  {/if}
                {/if}
              </div>
            </div>

            {#if isExpanded(country.iso2)}
              <ul class="region-children" role="list">
                {#each country.children as child (child.slug)}
                  {@const item = statusOf(child.slug)}
                  {@const status = item?.state ?? 'absent'}
                  <li class="region-row" data-level="child">
                    <span class="region-name">{child.name}</span>
                    <div class="region-meta">
                      <span class="region-size">{formatBytes(child.sizeBytes)}</span>
                      {#if status === 'downloading'}
                        <span class="region-status">{Math.round((item.bytes_downloaded / Math.max(1, item.bytes_total)) * 100)}%</span>
                      {:else if status === 'complete'}
                        <span class="region-status complete">Downloaded</span>
                      {/if}
                    </div>
                    <div class="region-actions">
                      {#if status === 'absent' || status === 'error'}
                        <Button onclick={() => downloadsState.start(child.slug)}>Download</Button>
                      {:else if status === 'complete'}
                        <Button variant="danger" onclick={() => downloadsState.remove(child.slug)}>Delete</Button>
                      {:else}
                        <Button variant="default" disabled>Downloading...</Button>
                      {/if}
                    </div>
                  </li>
                {/each}
              </ul>
            {/if}
          </li>
        {:else}
          <li class="region-empty">No regions match "{query}"</li>
        {/each}
      </ul>
    {/if}
  </Drawer.Body>
</Drawer>

<style>
  .picker-title { margin: 0; font-size: 14px; font-weight: 700; text-transform: uppercase; letter-spacing: 1px; }
  .picker-search { margin-bottom: 12px; }
  .picker-search :global(input) { font-size: 16px; }

  .region-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 4px; }
  .region-children { list-style: none; margin: 4px 0 0; padding: 0 0 0 24px; display: flex; flex-direction: column; gap: 4px; }
  .region-country { display: flex; flex-direction: column; }

  .region-row {
    display: grid;
    grid-template-columns: 1fr auto auto;
    align-items: center;
    gap: 12px;
    padding: 10px 12px;
    min-height: 56px;
    border: 1px solid var(--border-color);
    border-radius: 6px;
    background: var(--bg-secondary);
  }
  .region-row[data-level="country"] { background: var(--bg-tertiary, var(--bg-secondary)); font-weight: 600; }

  .region-toggle {
    display: flex; align-items: center; gap: 8px;
    background: none; border: 0; padding: 0; color: inherit; font: inherit;
    cursor: pointer; text-align: left; min-width: 0;
  }
  .region-toggle:disabled { cursor: default; }
  .region-caret { width: 1ch; text-align: center; opacity: 0.7; }

  .region-name { font-size: 14px; color: var(--text-primary); }
  .region-meta { display: flex; flex-direction: column; align-items: flex-end; gap: 2px; }
  .region-size { font-size: 12px; color: var(--text-muted); }
  .region-status { font-size: 12px; color: var(--text-muted); }
  .region-status.complete { color: var(--color-success); }
  .region-actions { display: flex; gap: 4px; }
  .region-empty { text-align: center; padding: 24px; color: var(--text-muted); font-size: 13px; }
</style>
