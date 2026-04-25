<script>
  // LiveMapV2: MapLibre-based replacement for LiveMap.svelte (Leaflet).
  // Mounts the 5 map layers (stations, trails, weather, hover-path,
  // my-position), drives the data store, and renders the operator chrome:
  // the FAB + InfoPanel (layer toggles + time-range), plus the bottom
  // coord display and status bar. Cutover at task 29.

  import { onDestroy } from 'svelte';
  import maplibregl from 'maplibre-gl';
  import MaplibreMap from '../lib/map/maplibre-map.svelte';
  import InfoPanel from '../lib/map/info-panel.svelte';
  import { createDataStore } from '../lib/map/data-store.svelte.js';
  import { mountStationsLayer } from '../lib/map/layers/stations.js';
  import { mountTrailsLayer } from '../lib/map/layers/trails.js';
  import { mountWeatherLayer } from '../lib/map/layers/weather.js';
  import { mountHoverPathLayer } from '../lib/map/layers/hover-path.js';
  import { mountMyPositionLayer } from '../lib/map/layers/my-position.js';
  import { renderStationPopupHTML } from '../lib/map/popup.js';
  import { unitsState } from '../lib/settings/units-store.svelte.js';
  import { toMaidenhead } from '../lib/map/maidenhead.js';
  import { fmtLat, fmtLon, timeAgo } from '../lib/map/popup-helpers.js';

  // 9-entry list. Values are seconds (data store wants ms; multiplied at
  // dispatch). Extends the legacy 6-option dropdown with 2/4/7-day entries.
  const TIMERANGES_S = [
    { value: 3600, label: '1 hour' },
    { value: 7200, label: '2 hours' },
    { value: 14400, label: '4 hours' },
    { value: 28800, label: '8 hours' },
    { value: 43200, label: '12 hours' },
    { value: 86400, label: '1 day' },
    { value: 172800, label: '2 days' },
    { value: 345600, label: '4 days' },
    { value: 604800, label: '7 days' },
  ];

  const dataStore = createDataStore();
  let stationsLayer = null;
  let trailsLayer = null;
  let weatherLayer = null;
  let hoverPathLayer = null;
  let myPositionLayer = null;
  let mapRef = null;
  let activePopup = null;

  // Operator chrome state.
  let panelOpen = $state(false);
  let layerToggles = $state({
    stations: true,
    trails: true,
    weather: true,
    myPosition: true,
  });
  let timerangeSec = $state(Math.floor(dataStore.timerangeMs / 1000));
  let coordText = $state('');

  // Tick once a second so "5s ago" stays accurate without hammering the
  // status-bar derived state from elsewhere.
  let tickNow = $state(Date.now());
  let tickTimer = null;
  if (typeof window !== 'undefined') {
    tickTimer = window.setInterval(() => {
      tickNow = Date.now();
    }, 1000);
  }

  function closePopup() {
    if (activePopup) {
      activePopup.remove();
      activePopup = null;
    }
  }

  function openStationPopup(map, station) {
    const pos = station && station.positions && station.positions[0];
    if (!pos) return;
    closePopup();

    const html = renderStationPopupHTML(station, {
      hasStation: (callsign) => dataStore.stations.has(callsign),
    });

    activePopup = new maplibregl.Popup({
      offset: 18,
      maxWidth: '320px',
      className: 'gw-station-popup',
      closeButton: true,
      closeOnClick: true,
    })
      .setLngLat([pos.lon, pos.lat])
      .setHTML(html)
      .addTo(map);

    // Keep the hover path pinned while the popup is open; clear it on close.
    hoverPathLayer?.show(station);

    activePopup.on('close', () => {
      activePopup = null;
      hoverPathLayer?.clear();
    });

    // Wire path-link clicks: pan + reopen popup for the clicked digipeater.
    const el = activePopup.getElement();
    if (el) {
      el.addEventListener('click', (ev) => {
        const link = ev.target && ev.target.closest && ev.target.closest('.path-link');
        if (!link) return;
        ev.preventDefault();
        const callsign = link.dataset.callsign;
        if (!callsign) return;
        const target = dataStore.stations.get(callsign);
        if (!target) return;
        const tpos = target.positions && target.positions[0];
        if (!tpos) return;
        map.panTo([tpos.lon, tpos.lat]);
        openStationPopup(map, target);
      });
    }
  }

  function updateCoordText(lngLat) {
    if (!lngLat) {
      coordText = '';
      return;
    }
    const lat = lngLat.lat;
    const lon = lngLat.lng;
    coordText = `${fmtLat(lat)} ${fmtLon(lon)} · ${toMaidenhead(lat, lon)}`;
  }

  function onMapReady(map) {
    mapRef = map;
    // Trails first so the line sits beneath the (DOM) station markers
    // and below the weather labels in symbol-layer order.
    trailsLayer = mountTrailsLayer(map, () => dataStore.stations);
    weatherLayer = mountWeatherLayer(map, () => dataStore.stations);
    hoverPathLayer = mountHoverPathLayer(map, () => {
      const my = dataStore.myPosition;
      return my ? { lat: my.lat, lon: my.lon } : null;
    });
    stationsLayer = mountStationsLayer(map, () => dataStore.stations, {
      onMarkerEnter: (s) => {
        // Don't override an open popup with a hover.
        if (activePopup) return;
        hoverPathLayer?.show(s);
      },
      onMarkerLeave: () => {
        if (activePopup) return;
        hoverPathLayer?.clear();
      },
      onMarkerClick: (s) => openStationPopup(map, s),
    });
    myPositionLayer = mountMyPositionLayer(map, () => dataStore.myPosition, {
      onMarkerEnter: () => {
        if (activePopup) return;
        const my = dataStore.myPosition;
        if (!my?.callsign) return;
        const myStation = dataStore.stations.get(my.callsign);
        if (myStation) hoverPathLayer?.show(myStation);
      },
      onMarkerLeave: () => {
        if (activePopup) return;
        hoverPathLayer?.clear();
      },
    });

    function updateBounds() {
      const b = map.getBounds();
      dataStore.setBounds({
        swLat: b.getSouth(),
        swLon: b.getWest(),
        neLat: b.getNorth(),
        neLon: b.getEast(),
      });
    }
    map.on('moveend', updateBounds);
    updateBounds();

    // Coord display: cheap (single $state assignment per move). MapLibre
    // already throttles mousemove to once per animation frame, so this
    // is fine without an explicit rAF gate.
    map.on('mousemove', (e) => updateCoordText(e.lngLat));
    map.on('mouseout', () => (coordText = ''));

    dataStore.start();
  }

  // Drive layer refresh from data-store reactivity. Touching .size
  // ensures Svelte tracks Map mutations even if the proxy short-circuits
  // a reassignment. unitsState.isMetric is read so the weather layer
  // re-renders when the operator toggles metric/imperial.
  $effect(() => {
    const _size = dataStore.stations.size;
    const _isMetric = unitsState.isMetric;
    const _myPos = dataStore.myPosition; // track
    if (stationsLayer) stationsLayer.refresh();
    if (trailsLayer) trailsLayer.refresh();
    if (weatherLayer) weatherLayer.refresh();
    if (myPositionLayer) myPositionLayer.refresh();
  });

  // Push the layer toggles into the layer modules. Each module no-ops
  // safely before the layer is actually mounted (effect re-fires after
  // onMapReady).
  $effect(() => {
    stationsLayer?.setVisible(layerToggles.stations);
  });
  $effect(() => {
    trailsLayer?.setVisible(layerToggles.trails);
  });
  $effect(() => {
    weatherLayer?.setVisible(layerToggles.weather);
  });
  $effect(() => {
    myPositionLayer?.setVisible(layerToggles.myPosition);
  });

  // Push the timerange into the data store.
  $effect(() => {
    dataStore.setTimerange(timerangeSec * 1000);
  });

  // ---- Status bar derivations ----
  let stationCount = $derived(dataStore.stations.size);
  let timerangeLabel = $derived(
    TIMERANGES_S.find((o) => o.value === timerangeSec)?.label || '',
  );
  let lastFetchAgo = $derived.by(() => {
    const t = dataStore.lastFetchAt;
    if (!t) return '';
    // Touching tickNow keeps this re-derived once a second.
    const _ = tickNow;
    return timeAgo(t.toISOString());
  });
  let pollDotClass = $derived(
    dataStore.pollingState === 'error'
      ? 'error'
      : dataStore.pollingState === 'polling'
        ? 'polling'
        : '',
  );
  let pollLabel = $derived(
    dataStore.pollingState === 'error'
      ? 'error'
      : dataStore.pollingState === 'polling'
        ? 'live'
        : 'idle',
  );

  onDestroy(() => {
    dataStore.stop();
    closePopup();
    stationsLayer?.destroy();
    trailsLayer?.destroy();
    weatherLayer?.destroy();
    hoverPathLayer?.destroy();
    myPositionLayer?.destroy();
    stationsLayer = null;
    trailsLayer = null;
    weatherLayer = null;
    hoverPathLayer = null;
    myPositionLayer = null;
    mapRef = null;
    if (tickTimer) {
      clearInterval(tickTimer);
      tickTimer = null;
    }
  });
</script>

<div class="livemap-shell">
  <MaplibreMap oncreate={onMapReady} />

  <!-- Floating Action Button (top-right, sits below MapLibre's NavigationControl
       which already lives at top-right; we offset right enough to avoid overlap
       on desktop, and the NavigationControl is hidden on mobile via gesture). -->
  <button
    type="button"
    class="map-fab"
    onclick={() => (panelOpen = !panelOpen)}
    aria-label="Map controls"
    aria-expanded={panelOpen}
  >
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <polygon points="12 2 2 7 12 12 22 7 12 2" />
      <polyline points="2 17 12 22 22 17" />
      <polyline points="2 12 12 17 22 12" />
    </svg>
  </button>

  <InfoPanel title="Map Layers" bind:open={panelOpen}>
    <div class="layer-toggles">
      <label class="toggle-row">
        <input
          type="checkbox"
          checked={layerToggles.stations}
          onchange={(e) => (layerToggles = { ...layerToggles, stations: e.currentTarget.checked })}
        />
        <span>Stations</span>
      </label>
      <label class="toggle-row">
        <input
          type="checkbox"
          checked={layerToggles.trails}
          onchange={(e) => (layerToggles = { ...layerToggles, trails: e.currentTarget.checked })}
        />
        <span>Trails</span>
      </label>
      <label class="toggle-row">
        <input
          type="checkbox"
          checked={layerToggles.weather}
          onchange={(e) => (layerToggles = { ...layerToggles, weather: e.currentTarget.checked })}
        />
        <span>Weather</span>
      </label>
      <label class="toggle-row">
        <input
          type="checkbox"
          checked={layerToggles.myPosition}
          onchange={(e) => (layerToggles = { ...layerToggles, myPosition: e.currentTarget.checked })}
        />
        <span>My Position</span>
      </label>
    </div>

    <label class="timerange-label" for="map-timerange-select">Time range</label>
    <select
      id="map-timerange-select"
      class="map-timerange-select"
      bind:value={timerangeSec}
    >
      {#each TIMERANGES_S as opt}
        <option value={opt.value}>{opt.label}</option>
      {/each}
    </select>
  </InfoPanel>

  <!-- Coord display (bottom-right) -->
  {#if coordText}
    <div class="map-coord-display">{coordText}</div>
  {/if}

  <!-- Status bar (bottom-left) -->
  <div class="map-status-bar" aria-live="polite">
    <span class="status-dot {pollDotClass}" aria-hidden="true"></span>
    <span>{pollLabel}</span>
    <span class="status-sep">&middot;</span>
    <span>{stationCount} station{stationCount !== 1 ? 's' : ''}</span>
    <span class="status-sep">&middot;</span>
    <span>{timerangeLabel}</span>
    {#if lastFetchAgo}
      <span class="status-sep">&middot;</span>
      <span>{lastFetchAgo}</span>
    {/if}
  </div>
</div>

<style>
  .livemap-shell {
    position: absolute;
    inset: 0;
    overflow: hidden;
  }

  /* FAB. Sits to the LEFT of MapLibre's NavigationControl (which is
     already top-right at the default 10px inset). We use top: 12px and
     position via right offset large enough to clear the 30px-wide nav
     stack plus its margin. */
  .map-fab {
    position: absolute;
    top: 12px;
    right: 60px;
    width: 44px;
    height: 44px;
    border-radius: 22px;
    background: var(--map-overlay-bg);
    color: var(--map-overlay-fg);
    border: 1px solid var(--map-overlay-border);
    box-shadow: var(--map-overlay-shadow);
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 60;
  }
  .map-fab:hover {
    color: var(--color-text);
  }

  .layer-toggles {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .toggle-row {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    cursor: pointer;
    color: var(--map-overlay-fg);
  }
  .toggle-row input[type='checkbox'] {
    width: 16px;
    height: 16px;
    accent-color: var(--color-accent);
    cursor: pointer;
  }
  .timerange-label {
    display: block;
    margin-top: 14px;
    margin-bottom: 4px;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 1px;
    color: var(--color-text-muted);
  }
  .map-timerange-select {
    width: 100%;
    background: var(--color-surface);
    color: var(--color-text);
    border: 1px solid var(--color-border);
    border-radius: 4px;
    font-family: var(--font-mono);
    font-size: 13px;
    padding: 6px 8px;
    cursor: pointer;
  }
  .map-timerange-select option {
    background: var(--color-surface);
    color: var(--color-text);
  }

  /* Coord display (bottom-right; sits above MapLibre's attribution). */
  .map-coord-display {
    position: absolute;
    bottom: 28px;
    right: 12px;
    padding: 4px 10px;
    background: var(--map-overlay-bg);
    color: var(--map-overlay-fg);
    border: 1px solid var(--map-overlay-border);
    border-radius: 4px;
    font-family: var(--font-mono);
    font-size: 12px;
    pointer-events: none;
    z-index: 40;
  }

  /* Status bar (bottom-left; sits above MapLibre's ScaleControl which
     also lives at bottom-left -- ScaleControl is short and the offset
     keeps them stacked cleanly). */
  .map-status-bar {
    position: absolute;
    bottom: 28px;
    left: 12px;
    padding: 4px 10px;
    background: var(--map-overlay-bg);
    color: var(--map-overlay-fg);
    border: 1px solid var(--map-overlay-border);
    border-radius: 4px;
    font-family: var(--font-mono);
    font-size: 12px;
    display: flex;
    gap: 6px;
    align-items: center;
    z-index: 40;
    pointer-events: none;
    white-space: nowrap;
  }
  .map-status-bar .status-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--color-success);
  }
  .map-status-bar .status-dot.error {
    background: var(--color-danger);
  }
  .map-status-bar .status-dot.polling {
    background: var(--color-success);
  }
  .map-status-bar .status-sep {
    opacity: 0.5;
  }

  /* Mobile: shrink coord/status text. Don't go below 11px to keep
     numbers readable on a small phone. Cap status-bar width and
     ellipsize so it can't push past the right edge into the coord
     display area on narrow phones. */
  @media (max-width: 480px) {
    .map-coord-display,
    .map-status-bar {
      font-size: 11px;
      padding: 4px 8px;
    }
    .map-status-bar {
      max-width: calc(100% - 24px);
      overflow: hidden;
      text-overflow: ellipsis;
    }
  }

  /* iOS Safari auto-zooms <select> with font-size <16px on focus.
     The select lives inside the InfoPanel bottom-sheet on mobile,
     so bump it to 16px there to suppress the zoom. */
  @media (max-width: 768px) {
    .map-timerange-select {
      font-size: 16px;
    }
  }

  /* The stations layer attaches .gw-station-marker / .gw-station-label
     elements outside this component's scope (MapLibre owns the DOM), so
     these have to be :global. */
  :global(.gw-station-marker) {
    display: flex;
    flex-direction: column;
    align-items: center;
    cursor: pointer;
    pointer-events: auto;
    user-select: none;
  }
  :global(.gw-station-label) {
    margin-top: 2px;
    padding: 1px 4px;
    font-size: 11px;
    font-weight: 600;
    color: var(--map-overlay-fg);
    background: var(--map-overlay-bg);
    border-radius: 2px;
    white-space: nowrap;
    max-width: 120px;
    overflow: hidden;
    text-overflow: ellipsis;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.2);
  }

  /* Station popup: theme-aware container + tip + close button. */
  :global(.gw-station-popup .maplibregl-popup-content) {
    background: var(--map-overlay-bg);
    color: var(--map-overlay-fg);
    border: 1px solid var(--map-overlay-border);
    border-radius: 8px;
    box-shadow: var(--map-overlay-shadow);
    padding: 12px;
    font-size: 13px;
  }
  :global(.gw-station-popup.maplibregl-popup-anchor-top .maplibregl-popup-tip) {
    border-bottom-color: var(--map-overlay-bg) !important;
  }
  :global(.gw-station-popup.maplibregl-popup-anchor-bottom .maplibregl-popup-tip) {
    border-top-color: var(--map-overlay-bg) !important;
  }
  :global(.gw-station-popup.maplibregl-popup-anchor-left .maplibregl-popup-tip) {
    border-right-color: var(--map-overlay-bg) !important;
  }
  :global(.gw-station-popup.maplibregl-popup-anchor-right .maplibregl-popup-tip) {
    border-left-color: var(--map-overlay-bg) !important;
  }
  :global(.gw-station-popup .maplibregl-popup-close-button) {
    color: var(--map-overlay-fg);
    font-size: 22px;
    width: 36px;
    height: 36px;
  }

  /* Popup interior structure. The HTML is generated by lib/map/popup.js,
     which lives outside this component, so these rules must be :global. */
  :global(.stn-popup) { font-family: var(--font-mono); }
  :global(.stn-hdr) { display: flex; align-items: center; gap: 8px; }
  :global(.stn-call) { color: #d4a040; font-size: 13px; font-weight: 700; }
  :global(.stn-sub) { color: var(--color-text-dim); font-size: 11px; margin-top: 2px; }
  :global(.stn-sep) { border-top: 1px solid var(--color-border-subtle); margin: 6px 0; }
  :global(.stn-coords) { font-size: 12px; }
  :global(.stn-meta) { color: var(--color-text-muted); font-size: 12px; }
  :global(.stn-via) { font-size: 12px; margin-top: 2px; }
  :global(.via-rf) { color: var(--color-success); }
  :global(.via-rf-hops) { color: var(--color-warning); }
  :global(.via-is) { color: #c39bff; }
  :global(.stn-path) { color: var(--color-text-dim); font-size: 11px; }
  :global(.stn-path .path-link) { color: #6eb5ff; text-decoration: none; cursor: pointer; }
  :global(.stn-path .path-link:hover) { text-decoration: underline; }
  :global(.stn-comment) { color: var(--color-text-dim); font-style: italic; font-size: 12px; }

  :global(.stn-popup .badge) {
    display: inline-block;
    font-weight: 700;
    font-size: 10px;
    padding: 2px 6px;
    border-radius: 3px;
    white-space: nowrap;
  }
  :global(.stn-popup .b-rx) { background: rgba(63, 185, 80, 0.15); color: var(--color-success); }
  :global(.stn-popup .b-tx) { background: rgba(210, 153, 34, 0.15); color: var(--color-warning); }
  :global(.stn-popup .b-is) { background: rgba(195, 155, 255, 0.15); color: #c39bff; }

  /* Own position marker. The MapLibre marker DOM is outside this
     component's scope, so these have to be :global. */
  :global(.own-position-marker) {
    background: none !important;
    border: none !important;
  }
  :global(.own-position) {
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: var(--color-accent);
    border: 2px solid var(--color-text);
    box-shadow: 0 0 0 3px rgba(88, 166, 255, 0.3);
  }
</style>
