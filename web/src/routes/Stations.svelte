<script>
  import { onMount } from 'svelte';
  import { Button, Input, Select, Box } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { online } from '../lib/stores/connection.js';
  import { toasts } from '../lib/stores.js';
  import { unitsState } from '../lib/settings/units-store.svelte.js';
  import PageHeader from '../components/PageHeader.svelte';
  import {
    COLS, ROWS,
    SPRITE_URLS, SPRITE_URLS_2X,
    cellOf, loadSymbols, describe,
  } from '../lib/aprsSymbols.js';

  // --- Constants -------------------------------------------------------

  const PAGE_SIZE_OPTIONS = [
    { value: 50,  label: '50 per page' },
    { value: 100, label: '100 per page' },
    { value: 200, label: '200 per page' },
  ];

  const TIMERANGE_OPTIONS = [
    { value: 900,    label: '15 minutes' },
    { value: 1800,   label: '30 minutes' },
    { value: 3600,   label: '1 hour' },
    { value: 7200,   label: '2 hours' },
    { value: 14400,  label: '4 hours' },
    { value: 28800,  label: '8 hours' },
    { value: 43200,  label: '12 hours' },
    { value: 86400,  label: '1 day' },
    { value: 172800, label: '2 days' },
    { value: 345600, label: '4 days' },
    { value: 604800, label: '7 days' },
  ];

  const DIRECTION_OPTIONS = [
    { value: 'RX', label: 'RX', cls: 'b-rx' },
    { value: 'TX', label: 'TX', cls: 'b-tx' },
    { value: 'IS', label: 'IS', cls: 'b-is' },
  ];

  const RETINA = typeof window !== 'undefined' && window.devicePixelRatio > 1.5;
  const SHEETS = RETINA ? SPRITE_URLS_2X : SPRITE_URLS;
  const ICON_PX = 20;

  // --- State -----------------------------------------------------------

  let stations       = $state([]);
  let aliases        = $state({});
  let myPosition     = $state(null);
  let posLogEnabled  = $state(false);
  let loading        = $state(true);
  let symbols        = $state(null);
  let autoRefresh    = $state(true);
  let timerange      = $state(86400);

  // Filters
  let filterCallsign = $state('');
  let filterAlias    = $state('');
  let hasAliasOnly   = $state(false);
  let todayOnly      = $state(false);
  let filterComment  = $state('');
  let selectedIcons  = $state(new Set());
  let iconDropOpen   = $state(false);
  let iconSearch     = $state('');
  let iconDropEl     = $state(null); // bound to the wrapper div for outside-click detection

  let filterDirections = $state(new Set());
  let dirDropOpen      = $state(false);
  let dirDropEl        = $state(null);

  // Sort
  let sortCol = $state('last_heard');
  let sortDir = $state('desc');

  // Pagination
  let pageSize    = $state(50);
  let currentPage = $state(1);

  // Inline alias editing: callsign currently being edited -> draft value
  let editingAlias   = $state(null);
  let editDraft      = $state('');

  let offline = $derived(!$online);

  // --- Helpers ---------------------------------------------------------

  /** Haversine distance between two points, in km. */
  function haversineKm(lat1, lon1, lat2, lon2) {
    const R = 6371;
    const dLat = (lat2 - lat1) * Math.PI / 180;
    const dLon = (lon2 - lon1) * Math.PI / 180;
    const a = Math.sin(dLat / 2) ** 2 +
              Math.cos(lat1 * Math.PI / 180) * Math.cos(lat2 * Math.PI / 180) *
              Math.sin(dLon / 2) ** 2;
    return R * 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));
  }

  /** Bearing from point 1 → point 2, 0–359 degrees true. */
  function bearingDeg(lat1, lon1, lat2, lon2) {
    const φ1 = lat1 * Math.PI / 180;
    const φ2 = lat2 * Math.PI / 180;
    const Δλ = (lon2 - lon1) * Math.PI / 180;
    const y = Math.sin(Δλ) * Math.cos(φ2);
    const x = Math.cos(φ1) * Math.sin(φ2) - Math.sin(φ1) * Math.cos(φ2) * Math.cos(Δλ);
    return ((Math.atan2(y, x) * 180 / Math.PI) + 360) % 360;
  }

  /** Format bearing as compass direction + degrees, e.g. "NNE 22°". */
  function formatBearing(deg) {
    const dirs = ['N','NNE','NE','ENE','E','ESE','SE','SSE','S','SSW','SW','WSW','W','WNW','NW','NNW'];
    const idx = Math.round(deg / 22.5) % 16;
    return `${dirs[idx]} ${Math.round(deg)}°`;
  }

  /** Format distance in km or miles based on units preference. */
  function formatDist(km) {
    if (unitsState.isMetric) return `${km < 10 ? km.toFixed(1) : Math.round(km)} km`;
    const mi = km * 0.621371;
    return `${mi < 10 ? mi.toFixed(1) : Math.round(mi)} mi`;
  }

  /** CSS background-position string for an APRS symbol sprite at ICON_PX. */
  function iconBgStyle(table, code) {
    if (!code) return '';
    const sheet = table === '/' ? SHEETS['/'] : SHEETS['\\'];
    const [col, row] = cellOf(code);
    const bgSize = `${COLS * ICON_PX}px ${ROWS * ICON_PX}px`;
    const bgPos  = `-${col * ICON_PX}px -${row * ICON_PX}px`;
    return `background-image:url(${sheet});background-size:${bgSize};background-position:${bgPos};background-repeat:no-repeat;image-rendering:${RETINA ? 'auto' : 'pixelated'}`;
  }

  /** Human-readable label for an APRS symbol. */
  function iconLabel(table, code) {
    return describe(symbols, table, code) || `${table}${code}`;
  }

  /** Composite key for a symbol used in selectedIcons set and dropdown. */
  function iconKey(table, code) { return `${table}${code}`; }

  /** "time ago" relative label. */
  function timeAgo(ts) {
    const diff = (Date.now() - new Date(ts).getTime()) / 1000;
    if (diff < 60)   return `${Math.round(diff)}s ago`;
    if (diff < 3600) return `${Math.round(diff / 60)}m ago`;
    if (diff < 86400) return `${Math.round(diff / 3600)}h ago`;
    return `${Math.round(diff / 86400)}d ago`;
  }

  // --- Derived: enriched station rows ----------------------------------

  let enriched = $derived.by(() => {
    const myLat = myPosition?.valid ? myPosition.lat : null;
    const myLon = myPosition?.valid ? myPosition.lon : null;
    return stations.map(s => {
      const pos = s.positions?.[0];
      const lat = pos?.lat ?? null;
      const lon = pos?.lon ?? null;
      let distKm = null, bearing = null;
      if (myLat !== null && lat !== null) {
        distKm  = haversineKm(myLat, myLon, lat, lon);
        bearing = bearingDeg(myLat, myLon, lat, lon);
      }
      return { ...s, alias: aliases[s.callsign] || '', distKm, bearing, lat, lon };
    });
  });

  // Unique icons across all current stations for the dropdown.
  let uniqueIcons = $derived.by(() => {
    const seen = new Map();
    for (const s of enriched) {
      const key = iconKey(s.symbol_table, s.symbol_code);
      if (!seen.has(key)) {
        seen.set(key, { key, table: s.symbol_table, code: s.symbol_code,
                        label: iconLabel(s.symbol_table, s.symbol_code) });
      }
    }
    return [...seen.values()].sort((a, b) => a.label.localeCompare(b.label));
  });

  // Filtered icon list for the dropdown search input.
  let filteredIcons = $derived.by(() => {
    const q = iconSearch.trim().toLowerCase();
    if (!q) return uniqueIcons;
    return uniqueIcons.filter(ic => ic.label.toLowerCase().includes(q));
  });

  // Midnight of today in local time (ms since epoch), for the "Today" filter.
  let todayStartMs = $derived((() => {
    const d = new Date();
    d.setHours(0, 0, 0, 0);
    return d.getTime();
  })());

  // Reset page to 1 whenever a filter changes so the user is never stranded.
  $effect(() => {
    // Access all filter state so this effect tracks them.
    filterCallsign; filterAlias; hasAliasOnly; todayOnly;
    filterComment; selectedIcons; filterDirections; sortCol; sortDir; pageSize;
    currentPage = 1;
  });

  let filteredSorted = $derived.by(() => {
    let list = enriched;

    // Callsign LIKE filter
    if (filterCallsign.trim()) {
      const q = filterCallsign.trim().toUpperCase();
      list = list.filter(s => s.callsign.toUpperCase().includes(q));
    }

    // Alias LIKE filter (only applies when alias column is visible)
    if (posLogEnabled && filterAlias.trim()) {
      const q = filterAlias.trim().toUpperCase();
      list = list.filter(s => s.alias.toUpperCase().includes(q));
    }

    // "Has alias" toggle
    if (posLogEnabled && hasAliasOnly) {
      list = list.filter(s => s.alias !== '');
    }

    // "Today" toggle
    if (todayOnly) {
      list = list.filter(s => new Date(s.last_heard).getTime() >= todayStartMs);
    }

    // Icon multiselect
    if (selectedIcons.size > 0) {
      list = list.filter(s => selectedIcons.has(iconKey(s.symbol_table, s.symbol_code)));
    }

    // Direction multiselect
    if (filterDirections.size > 0) {
      list = list.filter(s => filterDirections.has(s.direction));
    }

    // Comment LIKE filter
    if (filterComment.trim()) {
      const q = filterComment.trim().toLowerCase();
      list = list.filter(s => (s.comment || '').toLowerCase().includes(q));
    }

    // Sort
    list = [...list].sort((a, b) => {
      let va, vb;
      switch (sortCol) {
        case 'callsign':   va = a.callsign;  vb = b.callsign;  break;
        case 'alias':      va = a.alias;     vb = b.alias;     break;
        case 'last_heard': va = new Date(a.last_heard).getTime(); vb = new Date(b.last_heard).getTime(); break;
        case 'icon':       va = iconLabel(a.symbol_table, a.symbol_code); vb = iconLabel(b.symbol_table, b.symbol_code); break;
        case 'distance':   va = a.distKm  ?? Infinity; vb = b.distKm  ?? Infinity; break;
        case 'bearing':    va = a.bearing ?? Infinity; vb = b.bearing ?? Infinity; break;
        case 'comment':    va = a.comment || ''; vb = b.comment || ''; break;
        case 'lat':        va = a.lat ?? -Infinity; vb = b.lat ?? -Infinity; break;
        case 'lon':        va = a.lon ?? -Infinity; vb = b.lon ?? -Infinity; break;
        default:           va = 0; vb = 0;
      }
      if (va < vb) return sortDir === 'asc' ? -1 : 1;
      if (va > vb) return sortDir === 'asc' ?  1 : -1;
      return 0;
    });

    return list;
  });

  let totalPages  = $derived(Math.max(1, Math.ceil(filteredSorted.length / pageSize)));
  let safePage    = $derived(Math.min(currentPage, totalPages));
  let pagedRows   = $derived(filteredSorted.slice((safePage - 1) * pageSize, safePage * pageSize));

  // --- Data fetching ---------------------------------------------------

  async function refresh() {
    try {
      const [sData, aData, pos, plData] = await Promise.all([
        api.get(`/stations?bbox=-90,-180,90,180&timerange=${timerange}`),
        api.get('/stations/aliases'),
        api.get('/position').catch(() => null),
        api.get('/position-log').catch(() => null),
      ]);
      stations      = Array.isArray(sData) ? sData : [];
      aliases       = aData || {};
      myPosition    = pos || null;
      posLogEnabled = plData?.enabled ?? false;
    } catch (_) {}
    loading = false;
  }

  onMount(async () => {
    symbols = await loadSymbols().catch(() => null);
    await refresh();
  });

  // Auto-refresh polling — clears when toggled off or component is destroyed.
  $effect(() => {
    if (!autoRefresh) return;
    const id = setInterval(refresh, 30_000);
    return () => clearInterval(id);
  });

  // Re-fetch when timerange changes.
  $effect(() => {
    timerange; // track
    if (!loading) refresh();
  });

  // --- Sort toggle -----------------------------------------------------

  function setSort(col) {
    if (sortCol === col) {
      sortDir = sortDir === 'asc' ? 'desc' : 'asc';
    } else {
      sortCol = col;
      sortDir = col === 'last_heard' ? 'desc' : 'asc';
    }
  }

  function sortIndicator(col) {
    if (sortCol !== col) return '';
    return sortDir === 'asc' ? ' ▲' : ' ▼';
  }

  // --- Alias editing ---------------------------------------------------

  function startEditAlias(callsign, current) {
    editingAlias = callsign;
    editDraft    = current;
  }

  async function commitAlias(callsign) {
    editingAlias = null;
    const trimmed = editDraft.trim();
    try {
      if (trimmed === '') {
        await api.delete(`/stations/aliases/${encodeURIComponent(callsign)}`);
        const next = { ...aliases };
        delete next[callsign];
        aliases = next;
      } else {
        await api.put(`/stations/aliases/${encodeURIComponent(callsign)}`, { alias: trimmed });
        aliases = { ...aliases, [callsign]: trimmed };
      }
      toasts.success('Alias saved');
    } catch (_) {
      toasts.error('Failed to save alias');
    }
  }

  function onAliasKeydown(e, callsign) {
    if (e.key === 'Enter') { e.target.blur(); commitAlias(callsign); }
    if (e.key === 'Escape') { editingAlias = null; }
  }

  // --- Map navigation --------------------------------------------------

  function goToMap(s) {
    const pos = s.positions?.[0];
    if (!pos) return;
    window.location.hash =
      `#/map?focus=${encodeURIComponent(s.callsign)}&lat=${pos.lat}&lon=${pos.lon}`;
  }

  // --- Icon dropdown ---------------------------------------------------

  function toggleIcon(key) {
    const next = new Set(selectedIcons);
    if (next.has(key)) next.delete(key); else next.add(key);
    selectedIcons = next;
  }

  function clearIconFilter() { selectedIcons = new Set(); }

  function toggleDirection(val) {
    const next = new Set(filterDirections);
    if (next.has(val)) next.delete(val); else next.add(val);
    filterDirections = next;
  }

  // Close icon dropdown on outside click; also clear the search.
  function onIconDropKey(e) {
    if (e.key === 'Escape') { iconDropOpen = false; iconSearch = ''; }
  }

  // Close the icon dropdown on Escape or a click outside the wrapper.
  $effect(() => {
    if (!iconDropOpen) return;
    const dismiss = () => { iconDropOpen = false; iconSearch = ''; };
    const onKey   = (e) => { if (e.key === 'Escape') dismiss(); };
    const onDown  = (e) => { if (iconDropEl && !iconDropEl.contains(e.target)) dismiss(); };
    document.addEventListener('keydown', onKey);
    document.addEventListener('pointerdown', onDown);
    return () => {
      document.removeEventListener('keydown', onKey);
      document.removeEventListener('pointerdown', onDown);
    };
  });

  $effect(() => {
    if (!dirDropOpen) return;
    const dismiss = () => { dirDropOpen = false; };
    const onKey   = (e) => { if (e.key === 'Escape') dismiss(); };
    const onDown  = (e) => { if (dirDropEl && !dirDropEl.contains(e.target)) dismiss(); };
    document.addEventListener('keydown', onKey);
    document.addEventListener('pointerdown', onDown);
    return () => {
      document.removeEventListener('keydown', onKey);
      document.removeEventListener('pointerdown', onDown);
    };
  });

  // Focus an input element on mount (avoids the a11y_autofocus lint warning
  // that fires on the static `autofocus` attribute).
  function focusOnMount(node) {
    node.focus();
  }
</script>

<PageHeader title="Stations" subtitle="All stations heard on RF and APRS-IS">
  <span class="conn-status" class:error={offline} aria-live="polite">
    <span class="conn-dot"></span>{offline ? 'error' : 'live'}
  </span>
</PageHeader>

<!-- Toolbar -->
<Box>
  <div class="toolbar">
    <div class="toolbar-left">
      <label class="toggle-label">
        <input type="checkbox" bind:checked={autoRefresh} />
        Auto-refresh
      </label>
      <Button onclick={refresh} disabled={loading}>Refresh</Button>
      <Select
        bind:value={timerange}
        options={TIMERANGE_OPTIONS}
      />
    </div>
    <div class="toolbar-right">
      <span class="station-count">
        {filteredSorted.length} of {stations.length} station{stations.length !== 1 ? 's' : ''}
      </span>
      <Select
        bind:value={pageSize}
        options={PAGE_SIZE_OPTIONS}
      />
    </div>
  </div>
</Box>

<!-- Global quick-filters: toggles that apply to the whole result set -->
<Box>
  <div class="global-filters">
    <label class="toggle-label">
      <input type="checkbox" bind:checked={todayOnly} />
      Today only
    </label>
    {#if posLogEnabled}
      <label class="toggle-label">
        <input type="checkbox" bind:checked={hasAliasOnly} />
        Show stations with aliases
      </label>
    {/if}
  </div>
</Box>

<div class="table-wrap" style="margin-top: 12px;">
  {#if offline}
    <Box><div class="empty">No stations — connection to the Graywolf server lost.</div></Box>
  {:else if loading}
    <Box><div class="empty">Loading…</div></Box>
  {:else if stations.length === 0}
    <Box><div class="empty">No stations heard yet in the selected time range.</div></Box>
  {:else}
    <div class="table-scroll height-fix">
      <table class="stations-table">
        <thead>
          <!-- Header row: sortable column titles -->
          <tr class="header-row">
            <th class="th-sortable" onclick={() => setSort('callsign')}>Station{sortIndicator('callsign')}</th>
            {#if posLogEnabled}
              <th class="th-sortable" onclick={() => setSort('alias')}>Alias{sortIndicator('alias')}</th>
            {/if}
            <th class="th-sortable" onclick={() => setSort('last_heard')}>Last Heard{sortIndicator('last_heard')}</th>
            <th class="th-sortable" onclick={() => setSort('icon')}>Icon{sortIndicator('icon')}</th>
            <th class="th-sortable" onclick={() => setSort('distance')}>Distance{sortIndicator('distance')}</th>
            <th class="th-sortable" onclick={() => setSort('bearing')}>Bearing{sortIndicator('bearing')}</th>
            <th class="th-sortable" onclick={() => setSort('comment')}>Comment{sortIndicator('comment')}</th>
            <th class="th-sortable" onclick={() => setSort('lat')}>Lat{sortIndicator('lat')}</th>
            <th class="th-sortable" onclick={() => setSort('lon')}>Lon{sortIndicator('lon')}</th>
          </tr>
          <!-- Filter row -->
          <tr class="filter-row">
            <td>
              <Input bind:value={filterCallsign} placeholder="Search…" />
            </td>
            {#if posLogEnabled}
              <td class="filter-alias-cell">
                <Input bind:value={filterAlias} placeholder="Search…" />
              </td>
            {/if}
            <td class="filter-heard-cell">
              <div class="icon-drop-wrap" bind:this={dirDropEl}>
                <button
                  type="button"
                  class="icon-drop-btn"
                  onclick={() => { dirDropOpen = !dirDropOpen; }}
                  aria-expanded={dirDropOpen}
                  aria-haspopup="listbox"
                >
                  {filterDirections.size > 0 ? `${filterDirections.size} selected` : 'All'}
                  <span class="icon-drop-caret">▾</span>
                </button>
                {#if dirDropOpen}
                  <div class="icon-drop-panel" role="listbox" aria-multiselectable="true">
                    <button
                      type="button"
                      class="icon-drop-clear"
                      onclick={() => { filterDirections = new Set(); }}
                    >Clear selection</button>
                    <div class="icon-drop-list">
                      {#each DIRECTION_OPTIONS as opt}
                        <label class="icon-drop-item">
                          <input
                            type="checkbox"
                            checked={filterDirections.has(opt.value)}
                            onchange={() => toggleDirection(opt.value)}
                          />
                          <span class="badge {opt.cls}">{opt.label}</span>
                        </label>
                      {/each}
                    </div>
                  </div>
                {/if}
              </div>
            </td>
            <td class="filter-icon-cell">
              <!-- Custom multiselect dropdown for icon filter -->
              <div class="icon-drop-wrap" bind:this={iconDropEl}>
                <button
                  type="button"
                  class="icon-drop-btn"
                  onclick={() => { iconDropOpen = !iconDropOpen; }}
                  aria-expanded={iconDropOpen}
                  aria-haspopup="listbox"
                >
                  {selectedIcons.size > 0 ? `${selectedIcons.size} selected` : 'All icons'}
                  <span class="icon-drop-caret">▾</span>
                </button>
                {#if iconDropOpen}
                  <div class="icon-drop-panel" role="listbox" aria-multiselectable="true">
                    <div class="icon-drop-search">
                      <input
                        class="icon-search-input"
                        type="text"
                        bind:value={iconSearch}
                        placeholder="Search icons…"
                        aria-label="Search icon names"
                      />
                    </div>
                    <button
                      type="button"
                      class="icon-drop-clear"
                      onclick={clearIconFilter}
                    >Clear selection</button>
                    <div class="icon-drop-list">
                      {#each filteredIcons as ic (ic.key)}
                        <label class="icon-drop-item">
                          <input
                            type="checkbox"
                            checked={selectedIcons.has(ic.key)}
                            onchange={() => toggleIcon(ic.key)}
                          />
                          <span class="aprs-icon" style={iconBgStyle(ic.table, ic.code)}></span>
                          <span class="icon-drop-label">{ic.label}</span>
                        </label>
                      {/each}
                    </div>
                  </div>
                {/if}
              </div>
            </td>
            <td><!-- Distance: no filter --></td>
            <td><!-- Bearing: no filter --></td>
            <td>
              <Input bind:value={filterComment} placeholder="Search…" />
            </td>
            <td><!-- Lat: no filter --></td>
            <td><!-- Lon: no filter --></td>
          </tr>
        </thead>
        <tbody>
          {#if pagedRows.length === 0}
            <tr>
              <td colspan={posLogEnabled ? 9 : 8} class="empty">No stations match the current filters.</td>
            </tr>
          {:else}
            {#each pagedRows as s (s.callsign)}
              {@const pos = s.positions?.[0]}
              {@const dirCls = s.direction === 'RX' ? 'b-rx' : s.direction === 'TX' ? 'b-tx' : 'b-is'}
              <tr class="station-row">
                <!-- Station (callsign, click → map) -->
                <td class="td-callsign">
                  <button
                    type="button"
                    class="callsign-link"
                    onclick={() => goToMap(s)}
                    title="Open on Live Map"
                  >{s.callsign}</button>
                </td>

                <!-- Alias (inline edit, only when position log enabled) -->
                {#if posLogEnabled}
                  <td class="td-alias">
                    {#if editingAlias === s.callsign}
                      <input
                        class="alias-input"
                        bind:value={editDraft}
                        onblur={() => commitAlias(s.callsign)}
                        onkeydown={(e) => onAliasKeydown(e, s.callsign)}
                        placeholder="Add alias…"
                        maxlength="64"
                        use:focusOnMount
                      />
                    {:else}
                      <div class="alias-display">
                        <span class="alias-text">{s.alias || ''}</span>
                        <button
                          type="button"
                          class="alias-edit-btn"
                          onclick={() => startEditAlias(s.callsign, s.alias)}
                          title="Edit alias"
                          aria-label="Edit alias for {s.callsign}"
                        >✎</button>
                      </div>
                    {/if}
                  </td>
                {/if}

                <!-- Last Heard + direction badge -->
                <td class="td-heard">
                  <div class="cell-flex">
                    <span>{timeAgo(s.last_heard)}</span>
                    <span class="badge {dirCls}">{s.direction === 'IS' ? 'IS' : s.direction}</span>
                  </div>
                </td>

                <!-- Icon + name -->
                <td class="td-icon">
                  <div class="cell-flex">
                    <span class="aprs-icon" style={iconBgStyle(s.symbol_table, s.symbol_code)}></span>
                    <span class="icon-name">{iconLabel(s.symbol_table, s.symbol_code)}</span>
                  </div>
                </td>

                <!-- Distance -->
                <td class="td-num">
                  {s.distKm !== null ? formatDist(s.distKm) : '—'}
                </td>

                <!-- Bearing -->
                <td class="td-num">
                  {s.bearing !== null ? formatBearing(s.bearing) : '—'}
                </td>

                <!-- Comment -->
                <td class="td-comment">{s.comment || ''}</td>

                <!-- Lat -->
                <td class="td-num">{s.lat !== null ? s.lat.toFixed(5) : '—'}</td>

                <!-- Lon -->
                <td class="td-num">{s.lon !== null ? s.lon.toFixed(5) : '—'}</td>
              </tr>
            {/each}
          {/if}
        </tbody>
      </table>
    </div>

    <!-- Pagination controls -->
    <div class="pagination">
      <div class="pagination-info">
        {#if filteredSorted.length > 0}
          Showing {(safePage - 1) * pageSize + 1}–{Math.min(safePage * pageSize, filteredSorted.length)}
          of {filteredSorted.length}
        {/if}
      </div>
      <div class="pagination-nav">
        <button
          type="button"
          class="page-btn"
          disabled={safePage <= 1}
          onclick={() => { currentPage = safePage - 1; }}
        >← Prev</button>
        <span class="page-label">Page {safePage} of {totalPages}</span>
        <button
          type="button"
          class="page-btn"
          disabled={safePage >= totalPages}
          onclick={() => { currentPage = safePage + 1; }}
        >Next →</button>
      </div>
    </div>
  {/if}
</div>

<style>
  /* Connection status (mirrors Logs.svelte) */
  .conn-status {
    display: inline-flex; align-items: center; gap: 6px;
    font-size: var(--text-xs); font-weight: 600;
    text-transform: uppercase; letter-spacing: 0.04em;
    color: var(--color-success);
  }
  .conn-status.error { color: var(--color-danger); }
  .conn-dot {
    width: 9px; height: 9px; border-radius: 50%;
    background: var(--color-success);
  }
  .conn-status.error .conn-dot {
    background: var(--color-danger);
    box-shadow: 0 0 8px var(--color-danger);
  }

  /* Toolbar */
  .toolbar {
    display: flex; align-items: center; gap: 10px; flex-wrap: wrap;
    justify-content: space-between;
  }
  .toolbar-left, .toolbar-right {
    display: flex; align-items: center; gap: 10px; flex-wrap: wrap;
  }
  .station-count {
    font-size: var(--text-sm); color: var(--color-text-dim);
  }

  /* Toggle label */
  .toggle-label {
    display: inline-flex; align-items: center; gap: 6px;
    font-size: var(--text-sm); cursor: pointer; user-select: none;
    color: var(--color-text);
  }
  .toggle-small { font-size: var(--text-xs); }

  /* Table wrapper */
  .table-wrap { }
  .table-scroll { overflow-x: auto; }
  .empty {
    color: var(--color-text-dim); text-align: center; padding: 24px;
  }

  .height-fix {
    min-height: 60vh;
  }

  /* Table */
  .stations-table {
    width: 100%; border-collapse: collapse;
    font-size: var(--text-sm);
  }

  .header-row th {
    background: var(--bg-secondary);
    border-bottom: 2px solid var(--border-color);
    padding: 8px 10px;
    text-align: left;
    white-space: nowrap;
    font-weight: 600;
    font-size: var(--text-xs);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-secondary);
  }

  .th-sortable {
    cursor: pointer;
    user-select: none;
  }
  .th-sortable:hover { color: var(--color-primary); }

  /* Global quick-filter bar (between toolbar and table) */
  .global-filters {
    display: flex; align-items: center; gap: 20px; flex-wrap: wrap;
    padding: 2px 0;
  }

  /* Filter row */
  .filter-row td {
    background: var(--bg-secondary);
    border-bottom: 1px solid var(--border-color);
    padding: 4px 6px;
    vertical-align: middle;
  }
  .filter-alias-cell { min-width: 120px; }
  .filter-heard-cell { position: relative; min-width: 110px; vertical-align: top !important; }
  .filter-icon-cell { position: relative; min-width: 120px; vertical-align: top !important; }

  /* Body rows */
  .station-row td {
    padding: 7px 10px;
    border-bottom: 1px solid var(--border-color);
    vertical-align: middle;
  }
  .station-row:hover td { background: var(--color-surface-hover, rgba(255,255,255,0.04)); }

  /* Callsign link */
  .callsign-link {
    background: none; border: none; padding: 0;
    color: var(--color-primary); font-family: var(--font-mono);
    font-size: var(--text-sm); font-weight: 600;
    cursor: pointer; text-decoration: none;
  }
  .callsign-link:hover { text-decoration: underline; }

  /* Alias cell */
  .alias-display {
    display: flex; align-items: center; gap: 6px;
  }
  .alias-text { flex: 1; color: var(--color-text); font-size: var(--text-sm); }
  .alias-edit-btn {
    background: none; border: none; padding: 2px 4px;
    color: var(--color-text-dim); cursor: pointer; font-size: 13px;
    opacity: 0; transition: opacity 0.1s;
  }
  .station-row:hover .alias-edit-btn { opacity: 1; }
  .alias-input {
    width: 100%; background: var(--bg-primary);
    border: 1px solid var(--color-primary);
    border-radius: 4px; padding: 2px 6px;
    color: var(--color-text); font-size: var(--text-sm);
    outline: none;
  }

  /* Last Heard */
  .td-heard { white-space: nowrap; }

  /* Direction badges (mirrors LiveMapV2.svelte globals) */
  .badge {
    display: inline-block; padding: 1px 5px; border-radius: 3px;
    font-size: 10px; font-weight: 700; line-height: 1.4;
    text-transform: uppercase; letter-spacing: 0.04em;
  }
  .b-rx  { background: var(--color-success, #2a7a2a); color: #fff; }
  .b-tx  { background: var(--color-warning, #8a6a00); color: #fff; }
  .b-is  { background: var(--color-info,    #1a5f8a); color: #fff; }

  /* Icon cell */
  /* Inner flex wrapper — keeps display:flex off the <td> itself to avoid
     overriding display:table-cell (breaks layout in Firefox). */
  .cell-flex { display: flex; align-items: center; gap: 6px; }

  .td-icon { white-space: nowrap; }
  .aprs-icon {
    display: inline-block;
    width: 20px; height: 20px;
    flex-shrink: 0;
  }
  .icon-name { font-size: var(--text-xs); color: var(--color-text-dim); }

  /* Numeric cells */
  .td-num {
    font-variant-numeric: tabular-nums;
    font-size: var(--text-sm); white-space: nowrap;
    color: var(--color-text-dim);
  }
  .td-callsign { white-space: nowrap; }
  .td-comment { max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--color-text-dim); }

  /* Icon dropdown */
  .icon-drop-wrap { position: relative; }
  .icon-drop-btn {
    width: 100%; background: var(--bg-primary);
    border: 1px solid var(--border-color); border-radius: 4px;
    padding: 0.5rem; color: var(--color-text);
    font-size: var(--text-base); cursor: pointer;
    display: flex; align-items: center; justify-content: space-between;
    box-sizing: border-box;
    gap: 4px;
  }
  .icon-drop-btn:hover { border-color: var(--color-primary); }
  .icon-drop-caret { font-size: 10px; opacity: 0.6; }
  .icon-drop-panel {
    position: absolute; top: calc(100% + 4px); left: 0;
    z-index: 40; min-width: 200px; max-width: 260px;
    background: var(--bg-secondary); border: 1px solid var(--border-color);
    border-radius: 6px; box-shadow: var(--shadow-md, 0 4px 16px rgba(0,0,0,0.3));
    padding: 6px 0;
  }
  .icon-drop-clear {
    width: 100%; background: none; border: none;
    padding: 4px 12px; text-align: left;
    font-size: var(--text-xs); color: var(--color-text-dim);
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
    font-size: var(--text-sm); outline: none;
  }
  .icon-search-input:focus { border-color: var(--color-primary); }
  .icon-drop-list { max-height: 220px; overflow-y: auto; padding: 0 4px; }
  .icon-drop-item {
    display: flex; align-items: center; gap: 8px;
    padding: 4px 8px; cursor: pointer;
    border-radius: 4px; font-size: var(--text-sm);
    user-select: none;
  }
  .icon-drop-item:hover { background: var(--color-surface-hover, rgba(255,255,255,0.06)); }
  .icon-drop-label { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  /* Pagination */
  .pagination {
    display: flex; align-items: center; justify-content: space-between;
    padding: 8px 12px; border-top: 1px solid var(--border-color);
    font-size: var(--text-sm); flex-wrap: wrap; gap: 8px;
  }
  .pagination-info { color: var(--color-text-dim); }
  .pagination-nav { display: flex; align-items: center; gap: 10px; }
  .page-label { color: var(--color-text-dim); }
  .page-btn {
    background: var(--bg-secondary); border: 1px solid var(--border-color);
    border-radius: 4px; padding: 4px 12px;
    color: var(--color-text); font-size: var(--text-sm); cursor: pointer;
  }
  .page-btn:hover:not(:disabled) { border-color: var(--color-primary); color: var(--color-primary); }
  .page-btn:disabled { opacity: 0.4; cursor: not-allowed; }
</style>
