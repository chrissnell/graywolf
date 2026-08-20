<script>
  import { onMount } from 'svelte';
  import { Toggle } from '@chrissnell/chonky-ui';
  import PageHeader from '../components/PageHeader.svelte';
  import { getWeatherCounties, putCountyPrefs } from '../api/weather.js';
  import { toasts } from '../lib/stores.js';

  // --- Constants -------------------------------------------------------

  const PAGE_SIZE_OPTIONS = [
    { value: 50,  label: '50 per page' },
    { value: 100, label: '100 per page' },
    { value: 200, label: '200 per page' },
  ];

  const STATUS_LABELS = {
    clear:   'Clear',
    warning: 'Warning',
  };

  // --- State -----------------------------------------------------------

  let counties    = $state([]);
  let loading     = $state(true);
  let autoRefresh = $state(true);

  // Filters
  let filterName     = $state('');
  let filterAllowTX  = $state('all'); // 'all' | 'enabled' | 'disabled'
  let filterStates   = $state(new Set());
  let filterCWAs     = $state(new Set());
  let filterStatuses = $state(new Set());
  let filterTypes    = $state(new Set());

  // Multiselect dropdown open flags
  let allowTXDropOpen = $state(false);
  let stateDropOpen  = $state(false);
  let cwaDropOpen    = $state(false);
  let statusDropOpen = $state(false);
  let typeDropOpen   = $state(false);

  // Search inputs for each multiselect
  let stateSearch  = $state('');
  let cwaSearch    = $state('');
  let typeSearch   = $state('');

  // Trigger wrapper refs (for outside-click detection)
  let allowTXDropEl = $state(null);
  let stateDropEl  = $state(null);
  let cwaDropEl    = $state(null);
  let statusDropEl = $state(null);
  let typeDropEl   = $state(null);

  // Floating panel refs (outside table — needed for outside-click)
  let allowTXPanel = $state(null);
  let statePanel  = $state(null);
  let cwaPanel    = $state(null);
  let statusPanel = $state(null);
  let typePanel   = $state(null);

  // Anchor rects captured on button click to position fixed panels
  let allowTXAnchorRect = $state(null);
  let stateAnchorRect  = $state(null);
  let cwaAnchorRect    = $state(null);
  let statusAnchorRect = $state(null);
  let typeAnchorRect   = $state(null);

  const ALLOW_TX_LABELS = { all: 'All', enabled: 'Enabled', disabled: 'Disabled' };

  // Compute inline style for a fixed-position dropdown panel.
  function panelStyle(rect, minW = 180) {
    if (!rect) return '';
    return `position:fixed;top:${rect.bottom + 4}px;left:${rect.left}px;min-width:${Math.max(minW, rect.width)}px;`;
  }

  // Sort
  let sortCol = $state('distance_mi');
  let sortDir = $state('asc');

  // Pagination
  let pageSize    = $state(50);
  let currentPage = $state(1);

  // --- Derived option lists -------------------------------------------

  let uniqueStates  = $derived([...new Set(counties.map(c => c.state))].sort());
  let uniqueCWAs    = $derived([...new Set(counties.map(c => c.cwa).filter(Boolean))].sort());
  let uniqueStatuses = ['clear', 'warning'];
  let uniqueTypes   = $derived([...new Set(counties.filter(c => c.alert_type).map(c => c.alert_type))].sort());

  // Filtered option lists driven by each search input.
  let filteredStates = $derived.by(() => {
    const q = stateSearch.trim().toLowerCase();
    return q ? uniqueStates.filter(s => s.toLowerCase().includes(q)) : uniqueStates;
  });
  let filteredCWAs = $derived.by(() => {
    const q = cwaSearch.trim().toLowerCase();
    return q ? uniqueCWAs.filter(c => c.toLowerCase().includes(q)) : uniqueCWAs;
  });
  let filteredTypes = $derived.by(() => {
    const q = typeSearch.trim().toLowerCase();
    return q ? uniqueTypes.filter(t => t.toLowerCase().includes(q)) : uniqueTypes;
  });

  // --- Filtering & sorting -------------------------------------------

  function matchesFilter(c) {
    if (filterName && !c.county_name.toLowerCase().includes(filterName.toLowerCase())) return false;
    if (filterAllowTX === 'enabled' && !c.allow_tx) return false;
    if (filterAllowTX === 'disabled' && c.allow_tx) return false;
    if (filterStates.size > 0 && !filterStates.has(c.state)) return false;
    if (filterCWAs.size > 0 && !filterCWAs.has(c.cwa)) return false;
    if (filterStatuses.size > 0 && !filterStatuses.has(c.alert_status)) return false;
    if (filterTypes.size > 0 && !filterTypes.has(c.alert_type)) return false;
    return true;
  }

  function compareRows(a, b) {
    const dir = sortDir === 'asc' ? 1 : -1;
    let av = a[sortCol], bv = b[sortCol];
    if (sortCol === 'allow_tx') { av = av ? 1 : 0; bv = bv ? 1 : 0; }
    if (sortCol === 'distance_mi') {
      av = av < 0 ? Infinity : av;
      bv = bv < 0 ? Infinity : bv;
    }
    if (av === bv) return 0;
    if (av == null) return dir;
    if (bv == null) return -dir;
    return av < bv ? -dir : dir;
  }

  let filteredSorted = $derived(counties.filter(matchesFilter).toSorted(compareRows));

  let totalPages  = $derived(Math.max(1, Math.ceil(filteredSorted.length / pageSize)));
  let safePage    = $derived(Math.min(currentPage, totalPages));
  let pagedRows   = $derived(filteredSorted.slice((safePage - 1) * pageSize, safePage * pageSize));

  // Reset to page 1 on filter/sort change.
  $effect(() => {
    filterName; filterAllowTX; filterStates.size; filterCWAs.size; filterStatuses.size;
    filterTypes.size; sortCol; sortDir; pageSize;
    currentPage = 1;
  });

  // --- Sort helpers ---------------------------------------------------

  function setSort(col) {
    if (sortCol === col) {
      sortDir = sortDir === 'asc' ? 'desc' : 'asc';
    } else {
      sortCol = col;
      sortDir = col === 'distance_mi' ? 'asc' : 'asc';
    }
  }

  function sortIndicator(col) {
    if (sortCol !== col) return '';
    return sortDir === 'asc' ? ' ▲' : ' ▼';
  }

  // --- Data loading ---------------------------------------------------

  async function refresh() {
    loading = true;
    try {
      const data = await getWeatherCounties();
      counties = data || [];
    } catch (e) {
      toasts.error(e?.message || 'Failed to load weather counties');
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    refresh();
  });

  $effect(() => {
    if (!autoRefresh) return;
    const id = setInterval(refresh, 30_000);
    return () => clearInterval(id);
  });

  // --- Per-row TX toggle ----------------------------------------------

  async function handleAllowTX(county, value) {
    const fips = county.fips;
    // Optimistic update: replace the array so Svelte 5 recomputes all derived values.
    counties = counties.map(c => c.fips === fips ? { ...c, allow_tx: value } : c);
    try {
      await putCountyPrefs(fips, value);
    } catch (e) {
      counties = counties.map(c => c.fips === fips ? { ...c, allow_tx: !value } : c);
      toasts.error(e?.message || 'Failed to save county preference');
    }
  }

  // --- Multiselect helpers --------------------------------------------

  function toggleSet(set, value) {
    const next = new Set(set);
    if (next.has(value)) next.delete(value);
    else next.add(value);
    return next;
  }

  function formatDist(mi) {
    if (mi < 0) return '—';
    return mi < 10 ? `${mi.toFixed(1)} mi` : `${Math.round(mi)} mi`;
  }

  function formatLastHeard(ts) {
    if (!ts) return '—';
    return new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  }

  // Outside-click + Escape + scroll dismissal. Each effect checks both the
  // trigger wrapper AND the floating panel so a click inside the panel
  // (which lives outside the wrapper in the DOM) doesn't close the dropdown.
  $effect(() => {
    if (!allowTXDropOpen) return;
    const dismiss = () => { allowTXDropOpen = false; };
    const onKey  = (e) => { if (e.key === 'Escape') dismiss(); };
    const onDown = (e) => {
      if (allowTXDropEl && allowTXDropEl.contains(e.target)) return;
      if (allowTXPanel && allowTXPanel.contains(e.target)) return;
      dismiss();
    };
    document.addEventListener('keydown', onKey);
    document.addEventListener('pointerdown', onDown);
    window.addEventListener('scroll', dismiss, { passive: true, capture: true });
    return () => {
      document.removeEventListener('keydown', onKey);
      document.removeEventListener('pointerdown', onDown);
      window.removeEventListener('scroll', dismiss, { capture: true });
    };
  });
  $effect(() => {
    if (!stateDropOpen) return;
    const dismiss = () => { stateDropOpen = false; stateSearch = ''; };
    const onKey  = (e) => { if (e.key === 'Escape') dismiss(); };
    const onDown = (e) => {
      if (stateDropEl && stateDropEl.contains(e.target)) return;
      if (statePanel && statePanel.contains(e.target)) return;
      dismiss();
    };
    document.addEventListener('keydown', onKey);
    document.addEventListener('pointerdown', onDown);
    window.addEventListener('scroll', dismiss, { passive: true, capture: true });
    return () => {
      document.removeEventListener('keydown', onKey);
      document.removeEventListener('pointerdown', onDown);
      window.removeEventListener('scroll', dismiss, { capture: true });
    };
  });
  $effect(() => {
    if (!cwaDropOpen) return;
    const dismiss = () => { cwaDropOpen = false; cwaSearch = ''; };
    const onKey  = (e) => { if (e.key === 'Escape') dismiss(); };
    const onDown = (e) => {
      if (cwaDropEl && cwaDropEl.contains(e.target)) return;
      if (cwaPanel && cwaPanel.contains(e.target)) return;
      dismiss();
    };
    document.addEventListener('keydown', onKey);
    document.addEventListener('pointerdown', onDown);
    window.addEventListener('scroll', dismiss, { passive: true, capture: true });
    return () => {
      document.removeEventListener('keydown', onKey);
      document.removeEventListener('pointerdown', onDown);
      window.removeEventListener('scroll', dismiss, { capture: true });
    };
  });
  $effect(() => {
    if (!statusDropOpen) return;
    const dismiss = () => { statusDropOpen = false; };
    const onKey  = (e) => { if (e.key === 'Escape') dismiss(); };
    const onDown = (e) => {
      if (statusDropEl && statusDropEl.contains(e.target)) return;
      if (statusPanel && statusPanel.contains(e.target)) return;
      dismiss();
    };
    document.addEventListener('keydown', onKey);
    document.addEventListener('pointerdown', onDown);
    window.addEventListener('scroll', dismiss, { passive: true, capture: true });
    return () => {
      document.removeEventListener('keydown', onKey);
      document.removeEventListener('pointerdown', onDown);
      window.removeEventListener('scroll', dismiss, { capture: true });
    };
  });
  $effect(() => {
    if (!typeDropOpen) return;
    const dismiss = () => { typeDropOpen = false; typeSearch = ''; };
    const onKey  = (e) => { if (e.key === 'Escape') dismiss(); };
    const onDown = (e) => {
      if (typeDropEl && typeDropEl.contains(e.target)) return;
      if (typePanel && typePanel.contains(e.target)) return;
      dismiss();
    };
    document.addEventListener('keydown', onKey);
    document.addEventListener('pointerdown', onDown);
    window.addEventListener('scroll', dismiss, { passive: true, capture: true });
    return () => {
      document.removeEventListener('keydown', onKey);
      document.removeEventListener('pointerdown', onDown);
      window.removeEventListener('scroll', dismiss, { capture: true });
    };
  });
</script>

<PageHeader title="Weather Alerts" subtitle="NWS county alert status and RF forwarding">
  <Toggle bind:checked={autoRefresh} label="Auto-refresh" />
</PageHeader>

<div class="table-toolbar">
  <span class="row-count">
    {filteredSorted.length} of {counties.length} counties
    {#if counties.filter(c => c.alert_status !== 'clear').length > 0}
      · <span class="active-alerts">{counties.filter(c => c.alert_status !== 'clear').length} active alerts</span>
    {/if}
  </span>
  <div class="page-size-wrap">
    <select bind:value={pageSize} aria-label="Rows per page">
      {#each PAGE_SIZE_OPTIONS as opt}
        <option value={opt.value}>{opt.label}</option>
      {/each}
    </select>
  </div>
</div>

<div class="table-wrap heigh-fix">
  <table class="county-table">
    <thead>
      <!-- Column header row -->
      <tr>
        <th class="col-tx" onclick={() => setSort('allow_tx')} role="button" tabindex="0">
          Allow TX{sortIndicator('allow_tx')}
        </th>
        <th class="col-name" onclick={() => setSort('county_name')} role="button" tabindex="0">
          County Name{sortIndicator('county_name')}
        </th>
        <th class="col-state" onclick={() => setSort('state')} role="button" tabindex="0">
          State{sortIndicator('state')}
        </th>
        <th class="col-dist" onclick={() => setSort('distance_mi')} role="button" tabindex="0">
          Distance{sortIndicator('distance_mi')}
        </th>
        <th class="col-cwa" onclick={() => setSort('cwa')} role="button" tabindex="0">
          CWA{sortIndicator('cwa')}
        </th>
        <th class="col-code" onclick={() => setSort('nws_code')} role="button" tabindex="0">
          County Code{sortIndicator('nws_code')}
        </th>
        <th class="col-status" onclick={() => setSort('alert_status')} role="button" tabindex="0">
          Alert{sortIndicator('alert_status')}
        </th>
        <th class="col-type" onclick={() => setSort('alert_type')} role="button" tabindex="0">
          Type{sortIndicator('alert_type')}
        </th>
        <th class="col-heard" onclick={() => setSort('last_updated')} role="button" tabindex="0">
          Last Heard{sortIndicator('last_updated')}
        </th>
      </tr>

      <!-- Filter row -->
      <tr class="filter-row">
        <!-- Allow TX: single-select dropdown matching multiselect style -->
        <td class="filter-drop-cell">
          <div class="icon-drop-wrap" bind:this={allowTXDropEl}>
            <button
              type="button"
              class="icon-drop-btn"
              class:active={filterAllowTX !== 'all'}
              onclick={(e) => { allowTXAnchorRect = e.currentTarget.getBoundingClientRect(); allowTXDropOpen = !allowTXDropOpen; stateDropOpen = false; cwaDropOpen = false; statusDropOpen = false; typeDropOpen = false; }}
              aria-expanded={allowTXDropOpen}
              aria-haspopup="listbox"
            >
              {ALLOW_TX_LABELS[filterAllowTX]}
              <span class="icon-drop-caret">&#9662;</span>
            </button>
          </div>
        </td>

        <!-- County Name: text filter -->
        <td>
          <input
            class="filter-input"
            type="text"
            placeholder="Search…"
            bind:value={filterName}
            aria-label="Filter by county name"
          />
        </td>

        <!-- State: multiselect trigger only — panel rendered outside table below -->
        <td class="filter-drop-cell">
          <div class="icon-drop-wrap" bind:this={stateDropEl}>
            <button
              type="button"
              class="icon-drop-btn"
              class:active={filterStates.size > 0}
              onclick={(e) => { stateAnchorRect = e.currentTarget.getBoundingClientRect(); stateDropOpen = !stateDropOpen; cwaDropOpen = false; statusDropOpen = false; typeDropOpen = false; }}
              aria-expanded={stateDropOpen}
              aria-haspopup="listbox"
            >
              {filterStates.size > 0 ? `${filterStates.size} selected` : 'All states'}
              <span class="icon-drop-caret">&#9662;</span>
            </button>
          </div>
        </td>

        <!-- Distance: no filter -->
        <td></td>

        <!-- CWA: multiselect trigger only -->
        <td class="filter-drop-cell">
          <div class="icon-drop-wrap" bind:this={cwaDropEl}>
            <button
              type="button"
              class="icon-drop-btn"
              class:active={filterCWAs.size > 0}
              onclick={(e) => { cwaAnchorRect = e.currentTarget.getBoundingClientRect(); cwaDropOpen = !cwaDropOpen; stateDropOpen = false; statusDropOpen = false; typeDropOpen = false; }}
              aria-expanded={cwaDropOpen}
              aria-haspopup="listbox"
            >
              {filterCWAs.size > 0 ? `${filterCWAs.size} selected` : 'All CWAs'}
              <span class="icon-drop-caret">&#9662;</span>
            </button>
          </div>
        </td>

        <!-- County Code: no filter -->
        <td></td>

        <!-- Alert status: multiselect trigger only -->
        <td class="filter-drop-cell">
          <div class="icon-drop-wrap" bind:this={statusDropEl}>
            <button
              type="button"
              class="icon-drop-btn"
              class:active={filterStatuses.size > 0}
              onclick={(e) => { statusAnchorRect = e.currentTarget.getBoundingClientRect(); statusDropOpen = !statusDropOpen; stateDropOpen = false; cwaDropOpen = false; typeDropOpen = false; }}
              aria-expanded={statusDropOpen}
              aria-haspopup="listbox"
            >
              {filterStatuses.size > 0 ? `${filterStatuses.size} selected` : 'All alerts'}
              <span class="icon-drop-caret">&#9662;</span>
            </button>
          </div>
        </td>

        <!-- Alert type: multiselect trigger only -->
        <td class="filter-drop-cell">
          <div class="icon-drop-wrap" bind:this={typeDropEl}>
            <button
              type="button"
              class="icon-drop-btn"
              class:active={filterTypes.size > 0}
              onclick={(e) => { typeAnchorRect = e.currentTarget.getBoundingClientRect(); typeDropOpen = !typeDropOpen; stateDropOpen = false; cwaDropOpen = false; statusDropOpen = false; }}
              aria-expanded={typeDropOpen}
              aria-haspopup="listbox"
            >
              {filterTypes.size > 0 ? `${filterTypes.size} selected` : 'All types'}
              <span class="icon-drop-caret">&#9662;</span>
            </button>
          </div>
        </td>

        <!-- Last Heard: no filter -->
        <td></td>
      </tr>
    </thead>

    <tbody>
      {#if loading && counties.length === 0}
        <tr><td colspan="9" class="loading-cell">Loading county data…</td></tr>
      {:else if pagedRows.length === 0}
        <tr><td colspan="9" class="empty-cell">No counties match the current filters.</td></tr>
      {:else}
        {#each pagedRows as county (county.fips)}
          <tr class="county-row" class:has-alert={county.alert_status !== 'clear'}>
            <td class="col-tx">
              <Toggle
                checked={county.allow_tx}
                onCheckedChange={(v) => handleAllowTX(county, v)}
                label=""
                aria-label={`Allow TX for ${county.county_name}`}
              />
            </td>
            <td class="col-name">{county.county_name}</td>
            <td class="col-state">{county.state}</td>
            <td class="col-dist mono">{formatDist(county.distance_mi)}</td>
            <td class="col-cwa mono">{county.cwa || '—'}</td>
            <td class="col-code mono">{county.nws_code}</td>
            <td class="col-status">
              <span class="badge badge-{county.alert_status}">
                {STATUS_LABELS[county.alert_status] ?? county.alert_status}
              </span>
            </td>
            <td class="col-type mono">
              {#if county.alert_type}
                {county.alert_type.replace(/_/g, ' ')}
              {:else}
                —
              {/if}
            </td>
            <td class="col-heard mono">{formatLastHeard(county.last_updated)}</td>
          </tr>
        {/each}
      {/if}
    </tbody>
  </table>
</div>

<!-- Floating dropdown panels — rendered outside the table to escape overflow clipping.
     position:fixed keeps them anchored to the viewport via the captured anchor rect. -->
{#if allowTXDropOpen && allowTXAnchorRect}
  <div bind:this={allowTXPanel} class="icon-drop-panel" style={panelStyle(allowTXAnchorRect, 120)} role="listbox">
    {#each Object.entries(ALLOW_TX_LABELS) as [val, label]}
      <button
        type="button"
        class="icon-drop-item single-select-item"
        class:selected={filterAllowTX === val}
        onclick={() => { filterAllowTX = val; allowTXDropOpen = false; }}
        role="option"
        aria-selected={filterAllowTX === val}
      >{label}</button>
    {/each}
  </div>
{/if}

{#if stateDropOpen && stateAnchorRect}
  <div bind:this={statePanel} class="icon-drop-panel" style={panelStyle(stateAnchorRect, 180)} role="listbox" aria-multiselectable="true">
    <button type="button" class="icon-drop-clear" onclick={() => { filterStates = new Set(); }}>Clear selection</button>
    <div class="icon-drop-search">
      <input class="icon-search-input" type="text" bind:value={stateSearch} placeholder="Search states…" aria-label="Search states" />
    </div>
    <div class="icon-drop-list">
      {#each filteredStates as st}
        <label class="icon-drop-item">
          <input type="checkbox" checked={filterStates.has(st)} onchange={() => filterStates = toggleSet(filterStates, st)} />
          <span class="icon-drop-label">{st}</span>
        </label>
      {/each}
    </div>
  </div>
{/if}

{#if cwaDropOpen && cwaAnchorRect}
  <div bind:this={cwaPanel} class="icon-drop-panel" style={panelStyle(cwaAnchorRect, 180)} role="listbox" aria-multiselectable="true">
    <button type="button" class="icon-drop-clear" onclick={() => { filterCWAs = new Set(); }}>Clear selection</button>
    <div class="icon-drop-search">
      <input class="icon-search-input" type="text" bind:value={cwaSearch} placeholder="Search CWAs…" aria-label="Search CWAs" />
    </div>
    <div class="icon-drop-list">
      {#each filteredCWAs as cwa}
        <label class="icon-drop-item">
          <input type="checkbox" checked={filterCWAs.has(cwa)} onchange={() => filterCWAs = toggleSet(filterCWAs, cwa)} />
          <span class="icon-drop-label">{cwa}</span>
        </label>
      {/each}
    </div>
  </div>
{/if}

{#if statusDropOpen && statusAnchorRect}
  <div bind:this={statusPanel} class="icon-drop-panel" style={panelStyle(statusAnchorRect, 150)} role="listbox" aria-multiselectable="true">
    <button type="button" class="icon-drop-clear" onclick={() => { filterStatuses = new Set(); }}>Clear selection</button>
    <div class="icon-drop-list">
      {#each uniqueStatuses as st}
        <label class="icon-drop-item">
          <input type="checkbox" checked={filterStatuses.has(st)} onchange={() => filterStatuses = toggleSet(filterStatuses, st)} />
          <span class="icon-drop-label">{STATUS_LABELS[st] ?? st}</span>
        </label>
      {/each}
    </div>
  </div>
{/if}

{#if typeDropOpen && typeAnchorRect}
  <div bind:this={typePanel} class="icon-drop-panel" style={panelStyle(typeAnchorRect, 200)} role="listbox" aria-multiselectable="true">
    <button type="button" class="icon-drop-clear" onclick={() => { filterTypes = new Set(); }}>Clear selection</button>
    <div class="icon-drop-search">
      <input class="icon-search-input" type="text" bind:value={typeSearch} placeholder="Search types…" aria-label="Search alert types" />
    </div>
    <div class="icon-drop-list">
      {#each filteredTypes as t}
        <label class="icon-drop-item">
          <input type="checkbox" checked={filterTypes.has(t)} onchange={() => filterTypes = toggleSet(filterTypes, t)} />
          <span class="icon-drop-label">{t.replace(/_/g, ' ')}</span>
        </label>
      {/each}
    </div>
  </div>
{/if}

<!-- Pagination -->
{#if totalPages > 1}
  <div class="pagination">
    <button onclick={() => currentPage = Math.max(1, safePage - 1)} disabled={safePage <= 1}>← Prev</button>
    <span>Page {safePage} of {totalPages}</span>
    <button onclick={() => currentPage = Math.min(totalPages, safePage + 1)} disabled={safePage >= totalPages}>Next →</button>
  </div>
{/if}

<style>
  .table-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 0;
    font-size: 13px;
    color: var(--text-muted);
  }

  .active-alerts {
    color: var(--color-danger, #c41010);
    font-weight: 600;
  }

  .page-size-wrap select {
    font-size: 13px;
    padding: 4px 8px;
    border: 1px solid var(--border-color);
    border-radius: 4px;
    background: var(--bg-secondary);
    color: var(--text-primary);
  }

  .table-wrap {
    overflow-x: auto;
  }

  .height-fix {
    min-height: 60vh;
  }

  .county-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
  }

  .county-table th {
    text-align: left;
    padding: 8px 10px;
    border-bottom: 2px solid var(--border-color);
    white-space: nowrap;
    cursor: pointer;
    user-select: none;
    font-weight: 600;
    color: var(--text-secondary);
  }

  .county-table th:hover {
    color: var(--text-primary);
  }

  .filter-row td {
    padding: 4px 4px 8px;
    vertical-align: top;
  }

  .filter-input {
    width: 100%;
    padding: 4px 8px;
    border: 1px solid var(--border-color);
    border-radius: 4px;
    background: var(--bg-secondary);
    color: var(--text-primary);
    font-size: 12px;
    box-sizing: border-box;
  }

  .filter-drop-cell {
    position: relative;
    min-width: 120px;
  }

  /* Multiselect dropdowns — mirrors Stations.svelte icon-drop pattern */
  .icon-drop-wrap { position: relative; }
  .icon-drop-btn {
    width: 100%; background: var(--bg-primary);
    border: 1px solid var(--border-color); border-radius: 4px;
    padding: 4px 8px; color: var(--color-text);
    font-size: 12px; cursor: pointer;
    display: flex; align-items: center; justify-content: space-between;
    gap: 4px;
  }
  .icon-drop-btn:hover { border-color: var(--color-primary); }
  .icon-drop-btn.active { border-color: var(--accent); color: var(--text-primary); }
  .icon-drop-caret { font-size: 10px; opacity: 0.6; }
  .icon-drop-panel {
    /* z-index must beat any sticky table header or modal overlay */
    z-index: 9999;
    background: var(--bg-secondary); border: 1px solid var(--border-color);
    border-radius: 6px; box-shadow: var(--shadow-md, 0 4px 16px rgba(0,0,0,0.3));
    padding: 6px 0;
  }
  .icon-drop-clear {
    width: 100%; background: none; border: none;
    padding: 4px 12px; text-align: left;
    font-size: 12px; color: var(--color-text-dim);
    cursor: pointer; border-bottom: 1px solid var(--border-color);
    margin-bottom: 4px;
  }
  .icon-drop-clear:hover { color: var(--color-primary); }
  .icon-drop-search {
    padding: 6px 8px 4px;
    border-bottom: 1px solid var(--border-color);
  }
  .icon-search-input {
    width: 100%; background: var(--bg-primary);
    border: 1px solid var(--border-color); border-radius: 4px;
    padding: 4px 8px; color: var(--color-text);
    font-size: 12px; outline: none; box-sizing: border-box;
  }
  .icon-search-input:focus { border-color: var(--color-primary); }
  .icon-drop-list { max-height: 200px; overflow-y: auto; padding: 0 4px; }
  .icon-drop-item {
    display: flex; align-items: center; gap: 8px;
    padding: 4px 8px; cursor: pointer;
    border-radius: 4px; font-size: 12px;
    user-select: none;
  }
  .icon-drop-item:hover { background: var(--color-surface-hover, rgba(255,255,255,0.06)); }
  .icon-drop-label { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  .single-select-item {
    width: 100%; background: none; border: none; text-align: left;
    cursor: pointer; padding: 6px 12px; font-size: 12px;
    color: var(--color-text);
    border-radius: 4px; display: block;
  }
  .single-select-item:hover { background: var(--color-surface-hover, rgba(255,255,255,0.06)); }
  .single-select-item.selected { color: var(--color-primary); font-weight: 600; }

  .county-row td {
    padding: 7px 10px;
    border-bottom: 1px solid var(--border-color);
    vertical-align: middle;
  }

  .county-row.has-alert {
    background: rgba(var(--color-warning-rgb, 212,167,44), 0.04);
  }

  .mono {
    font-family: var(--font-mono);
  }

  /* Alert status badges */
  .badge {
    display: inline-block;
    padding: 2px 8px;
    border-radius: 10px;
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .badge-clear {
    background: rgba(39,174,96,0.15);
    color: #27ae60;
  }

  .badge-watch {
    background: rgba(230,157,0,0.18);
    color: #b07800;
  }

  .badge-warning {
    background: rgba(196,16,16,0.15);
    color: #c41010;
  }

  .loading-cell, .empty-cell {
    padding: 24px;
    text-align: center;
    color: var(--text-muted);
  }

  .pagination {
    display: flex;
    align-items: center;
    gap: 12px;
    justify-content: center;
    padding: 16px 0;
    font-size: 13px;
    color: var(--text-muted);
  }

  .pagination button {
    padding: 5px 12px;
    border: 1px solid var(--border-color);
    border-radius: 4px;
    background: var(--bg-secondary);
    color: var(--text-primary);
    cursor: pointer;
  }

  .pagination button:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  /* Mobile: collapse less critical columns */
  @media (max-width: 768px) {
    .col-cwa, .col-code {
      display: none;
    }
  }
</style>
