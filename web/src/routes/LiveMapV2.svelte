<script>
  // LiveMapV2: MapLibre-based replacement for LiveMap.svelte (Leaflet).
  // Mounts the 5 map layers (stations, trails, weather, hover-path,
  // my-position), drives the data store, and renders the operator chrome:
  // the FAB + InfoPanel (layer toggles + time-range), plus the bottom
  // coord display and status bar. Cutover at task 29.

  import { onDestroy, onMount } from 'svelte';
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
  // Auto-fit on first successful poll: planet view → bounds of stations
  // heard in the active timerange (default 1h). One-shot, so panning/zooming
  // afterward sticks.
  let didAutoFit = false;
  let stationsLayer = null;
  let trailsLayer = null;
  let weatherLayer = null;
  let hoverPathLayer = null;
  let myPositionLayer = null;
  let mapRef = null;
  let activePopup = null;

  // Operator chrome state.
  // On desktop the layer card is a perma-card (top-left); on mobile we
  // use a FAB + bottom-sheet drawer like the legacy Leaflet UI.
  let isMobile = $state(false);
  let panelOpen = $state(false);
  let layerCardCollapsed = $state(false);
  let layerToggles = $state({
    stations: true,
    trails: true,
    weather: true,
    myPosition: true,
  });
  let timerangeSec = $state(Math.floor(dataStore.timerangeMs / 1000));
  let coordText = $state('');
  let zoomLevel = $state(null);

  // Tick once a second so "5s ago" stays accurate without hammering the
  // status-bar derived state from elsewhere.
  let tickNow = $state(Date.now());
  let tickTimer = null;
  if (typeof window !== 'undefined') {
    tickTimer = window.setInterval(() => {
      tickNow = Date.now();
    }, 1000);
  }

  let mqUnsub = null;
  onMount(() => {
    if (typeof window === 'undefined') return;
    const mq = window.matchMedia('(max-width: 768px)');
    isMobile = mq.matches;
    const handler = (e) => (isMobile = e.matches);
    mq.addEventListener('change', handler);
    mqUnsub = () => mq.removeEventListener('change', handler);
  });

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

  function focusStation(callsign) {
    const target = dataStore.stations.get(callsign);
    if (!target) return;
    const tpos = target.positions && target.positions[0];
    if (!tpos) return;
    mapRef?.panTo([tpos.lon, tpos.lat]);
    if (mapRef) openStationPopup(mapRef, target);
  }

  function onMapReady(map) {
    mapRef = map;
    // Trails first so the line sits beneath the (DOM) station markers
    // and below the weather labels in symbol-layer order.
    trailsLayer = mountTrailsLayer(map, () => dataStore.stations, {
      hasStation: (callsign) => dataStore.stations.has(callsign),
      focusStation,
    });
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

    zoomLevel = map.getZoom();
    map.on('zoom', () => (zoomLevel = map.getZoom()));

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

  // Push the layer toggles into the layer modules. We MUST read the
  // reactive value before the optional-chain so Svelte 5 tracks it as a
  // dependency on the initial run. With `layer?.setVisible(toggle)`, if
  // `layer` is null on first run (mount before onMapReady), the RHS is
  // short-circuited and `toggle` is never read — the effect ends up with
  // zero deps and never re-fires when the operator clicks a checkbox.
  // Reading `toggle` into a const first guarantees the dep is registered.
  $effect(() => {
    const v = layerToggles.stations;
    stationsLayer?.setVisible(v);
  });
  $effect(() => {
    const v = layerToggles.trails;
    trailsLayer?.setVisible(v);
  });
  $effect(() => {
    const v = layerToggles.weather;
    weatherLayer?.setVisible(v);
  });
  $effect(() => {
    const v = layerToggles.myPosition;
    myPositionLayer?.setVisible(v);
  });

  // Push the timerange into the data store.
  $effect(() => {
    dataStore.setTimerange(timerangeSec * 1000);
  });

  // One-shot auto-fit: after the first poll completes, frame all stations
  // currently in the data store. Empty result → leave at planet view.
  $effect(() => {
    const t = dataStore.lastFetchAt;
    if (!t || !mapRef || didAutoFit) return;
    didAutoFit = true;
    fitToStations();
  });

  function fitToStations() {
    if (!mapRef) return;
    const coords = [];
    for (const s of dataStore.stations.values()) {
      const p = s.positions && s.positions[0];
      if (p && Number.isFinite(p.lat) && Number.isFinite(p.lon)) {
        coords.push([p.lon, p.lat]);
      }
    }
    if (coords.length === 0) return;
    if (coords.length === 1) {
      mapRef.easeTo({ center: coords[0], zoom: 9, duration: 600 });
      return;
    }
    const bounds = new maplibregl.LngLatBounds(coords[0], coords[0]);
    for (let i = 1; i < coords.length; i++) bounds.extend(coords[i]);
    mapRef.fitBounds(bounds, { padding: 60, maxZoom: 12, duration: 600 });
  }

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
    mqUnsub?.();
    mqUnsub = null;
  });
</script>

<div class="livemap-shell">
  <!-- Start at planet view; onMapReady fits to recent stations after first poll. -->
  <MaplibreMap initialCenter={[0, 20]} initialZoom={1} oncreate={onMapReady} />

  {#snippet panelBody()}
    <div class="layer-toggles">
      <label class="toggle-row">
        <input
          type="checkbox"
          checked={layerToggles.stations}
          onchange={(e) => (layerToggles.stations = e.currentTarget.checked)}
        />
        <span>Stations</span>
      </label>
      <label class="toggle-row">
        <input
          type="checkbox"
          checked={layerToggles.trails}
          onchange={(e) => (layerToggles.trails = e.currentTarget.checked)}
        />
        <span>Trails</span>
      </label>
      <label class="toggle-row">
        <input
          type="checkbox"
          checked={layerToggles.weather}
          onchange={(e) => (layerToggles.weather = e.currentTarget.checked)}
        />
        <span>Weather</span>
      </label>
      <label class="toggle-row">
        <input
          type="checkbox"
          checked={layerToggles.myPosition}
          onchange={(e) => (layerToggles.myPosition = e.currentTarget.checked)}
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
  {/snippet}

  {#if isMobile}
    <!-- Mobile: FAB at top-right opens a bottom-sheet drawer. -->
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
      {@render panelBody()}
    </InfoPanel>
  {:else}
    <!-- Desktop: perma-card at top-left, like the legacy Leaflet UI.
         A caret in the header collapses the body; the header stays
         visible so the operator can re-expand it. -->
    <aside
      class="layer-card"
      class:collapsed={layerCardCollapsed}
      aria-label="Map Layers"
    >
      <button
        type="button"
        class="layer-card-header"
        onclick={() => (layerCardCollapsed = !layerCardCollapsed)}
        aria-expanded={!layerCardCollapsed}
        aria-controls="layer-card-body"
      >
        <h2>Map Layers</h2>
        <svg
          class="layer-card-caret"
          width="12"
          height="12"
          viewBox="0 0 12 12"
          aria-hidden="true"
        >
          <polyline
            points="2,4 6,8 10,4"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
      </button>
      {#if !layerCardCollapsed}
        <div id="layer-card-body" class="layer-card-body">
          {@render panelBody()}
        </div>
      {/if}
    </aside>
  {/if}

  <!-- Coord + zoom display (bottom-right). Zoom is always visible;
       coords/grid only appear while the cursor is over the map. -->
  {#if zoomLevel !== null || coordText}
    <div class="map-coord-display">
      {#if coordText}{coordText} &middot; {/if}z {zoomLevel?.toFixed(1) ?? ''}
    </div>
  {/if}

  <!-- Status bar (bottom-center; legacy placement so it doesn't sit
       under the sidebar on narrow desktop windows). -->
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

  /* FAB (mobile only). Sits to the LEFT of MapLibre's NavigationControl
     (which is already top-right at the default 10px inset). NavigationControl
     itself is hidden on touch widths via maplibre-map.svelte's media query,
     so the FAB has the corner to itself. */
  .map-fab {
    position: absolute;
    top: 12px;
    right: 12px;
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

  /* Desktop layer card (top-left, perma-visible — matches the legacy
     Leaflet UI). */
  .layer-card {
    position: absolute;
    top: 12px;
    left: 12px;
    width: 200px;
    background: var(--map-overlay-bg);
    color: var(--map-overlay-fg);
    border: 1px solid var(--map-overlay-border);
    border-radius: 8px;
    box-shadow: var(--map-overlay-shadow);
    z-index: 50;
  }
  .layer-card-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    padding: 8px 12px;
    border: none;
    border-bottom: 1px solid var(--map-overlay-border);
    background: transparent;
    color: inherit;
    cursor: pointer;
    text-align: left;
    font: inherit;
  }
  .layer-card.collapsed .layer-card-header {
    border-bottom: none;
  }
  .layer-card-header h2 {
    margin: 0;
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 1px;
    color: var(--color-text-muted);
  }
  .layer-card-caret {
    color: var(--color-text-muted);
    transition: transform 120ms ease;
    flex-shrink: 0;
  }
  .layer-card.collapsed .layer-card-caret {
    transform: rotate(-90deg);
  }
  .layer-card-header:hover .layer-card-caret,
  .layer-card-header:hover h2 {
    color: var(--color-text);
  }
  .layer-card-body {
    padding: 10px 12px;
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

  /* Status bar (bottom-center; matches the legacy Leaflet placement so
     it doesn't sit under the sidebar on narrow desktop windows and stays
     visible whether the sidebar is wide or collapsed). */
  .map-status-bar {
    position: absolute;
    bottom: 20px;
    left: 50%;
    transform: translateX(-50%);
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
  /* Pull the status bar up on mobile so it clears the bottom safe area
     and the MapLibre attribution. */
  @media (max-width: 768px) {
    .map-status-bar {
      bottom: 14px;
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

  /* The stations layer attaches .gw-station-marker / .gw-station-label /
     .gw-station-icon elements outside this component's scope (MapLibre
     owns the DOM), so these have to be :global.

     Layout: the marker root keeps maplibregl's position:absolute (do NOT
     override with position:relative — that pulls the marker into document
     flow and the per-marker transform stacks all of them at the canvas
     origin). The 21x21 icon child is the visual anchor (anchor:'center'
     in stations.js puts the icon center on the lat/lon). The callsign
     label is absolutely positioned to the right of the icon, anchored
     within the maplibregl-applied positioning context, so its width
     doesn't shift the icon off-target. */
  :global(.gw-station-marker) {
    width: 21px;
    height: 21px;
    cursor: pointer;
    pointer-events: auto;
    user-select: none;
  }
  :global(.gw-station-icon) {
    width: 21px;
    height: 21px;
  }
  :global(.gw-station-label) {
    position: absolute;
    left: 100%;
    top: 50%;
    transform: translateY(-50%);
    margin-left: 4px;
    padding: 0 4px;
    line-height: 12px;
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 600;
    color: #ffffff;
    background: rgba(14, 14, 14, 0.78);
    border: 1px solid rgba(255, 255, 255, 0.6);
    border-radius: 2px;
    white-space: nowrap;
    max-width: 120px;
    overflow: hidden;
    text-overflow: ellipsis;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.35);
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

  /* Weather label chip -- ports the legacy Leaflet wx-label/wx-text
     styling. The marker root is a maplibregl.Marker (DOM-based) so
     these have to be :global. */
  :global(.wx-label) {
    background: none !important;
    border: none !important;
    pointer-events: none;
  }
  :global(.wx-text) {
    background: rgba(22, 27, 34, 0.85);
    color: var(--color-text-dim);
    font-family: var(--font-mono);
    font-size: 10px;
    padding: 1px 4px;
    border-radius: 3px;
    white-space: nowrap;
    text-align: center;
  }

  /* Trail hover tooltip: small dim chip with the callsign, tip-less and
     non-interactive. Distinct from station popups so it doesn't pull
     theme styling for the close button etc. */
  :global(.gw-trail-tooltip .maplibregl-popup-content) {
    background: rgba(22, 27, 34, 0.85);
    color: #e0e0e0;
    border: 1px solid var(--color-border-subtle);
    border-radius: 3px;
    padding: 1px 6px;
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 700;
    box-shadow: none;
    pointer-events: none;
  }
  :global(.gw-trail-tooltip .maplibregl-popup-tip) {
    display: none;
  }

  /* Own position marker -- iOS Maps "blue dot" look: white ring around a
     solid blue core, with a soft blue halo. The MapLibre marker DOM is
     outside this component's scope, so these have to be :global. */
  :global(.own-position-marker) {
    background: none !important;
    border: none !important;
  }
  :global(.own-position) {
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: #007aff;
    border: 2px solid #ffffff;
    box-shadow:
      0 0 0 6px rgba(0, 122, 255, 0.18),
      0 1px 4px rgba(0, 0, 0, 0.4);
  }
</style>
