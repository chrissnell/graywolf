<script>
  import { onMount } from 'svelte';
  import L from 'leaflet';
  import 'leaflet/dist/leaflet.css';
  import { mapState } from '../lib/map/map-store.svelte.js';
  import { StationLayer } from '../lib/map/station-layer.js';
  import { TrailLayer } from '../lib/map/trail-layer.js';
  import { WeatherLayer } from '../lib/map/weather-layer.js';

  // --- Reactive state ---
  let container;
  let map = $state(null);
  let stationCount = $state(0);
  let pollStatus = $state('connecting');
  let lastFetchAgo = $state('');
  let initialLoadDone = $state(false);
  let coordText = $state('');
  let layerControlOpen = $state(
    typeof window !== 'undefined' ? window.innerWidth > 768 : true,
  );

  // --- Non-reactive (must NOT be $state to avoid unwanted $effect deps) ---
  let stationLayer, trailLayer, weatherLayer;
  let ownMarker = null;
  let sinceCursor = null;
  let cachedBounds = null;
  let lastFetchTime = null;

  const TIMERANGE_OPTIONS = [
    { value: 3600, label: '1 hour' },
    { value: 7200, label: '2 hours' },
    { value: 14400, label: '4 hours', disabled: true },
    { value: 28800, label: '8 hours', disabled: true },
    { value: 43200, label: '12 hours', disabled: true },
    { value: 86400, label: '1 day', disabled: true },
  ];

  // --- Helpers ---

  function toMaidenhead(lat, lon) {
    lon += 180;
    lat += 90;
    return (
      String.fromCharCode(65 + Math.floor(lon / 20)) +
      String.fromCharCode(65 + Math.floor(lat / 10)) +
      Math.floor((lon % 20) / 2) +
      Math.floor(lat % 10) +
      String.fromCharCode(97 + Math.floor((lon % 2) * 12)) +
      String.fromCharCode(97 + Math.floor((lat % 1) * 24))
    );
  }

  function expandBounds(bounds) {
    const sw = bounds.getSouthWest();
    const ne = bounds.getNorthEast();
    const latD = (ne.lat - sw.lat) * 0.5;
    const lngD = (ne.lng - sw.lng) * 0.5;
    return L.latLngBounds(
      [sw.lat - latD, sw.lng - lngD],
      [ne.lat + latD, ne.lng + lngD],
    );
  }

  function shouldFullReload(currentBounds) {
    if (!cachedBounds) return true;
    return !cachedBounds.contains(currentBounds);
  }

  function updateCoordText(latlng) {
    if (!latlng) return;
    const la = Math.abs(latlng.lat).toFixed(4);
    const lo = Math.abs(latlng.lng).toFixed(4);
    const ld = latlng.lat >= 0 ? 'N' : 'S';
    const od = latlng.lng >= 0 ? 'E' : 'W';
    coordText = `${la}\u00B0${ld} ${lo}\u00B0${od} \u00B7 ${toMaidenhead(latlng.lat, latlng.lng)}`;
  }

  function updateAgoDisplay() {
    if (!lastFetchTime) { lastFetchAgo = ''; return; }
    const sec = Math.floor((Date.now() - lastFetchTime) / 1000);
    lastFetchAgo = sec < 60 ? `${sec}s ago` : `${Math.floor(sec / 60)}m ago`;
  }

  // --- Fetch with ETag support ---
  // Uses raw fetch (not api.js) because we need If-None-Match / 304 handling.

  async function fetchStations(mapInstance, timerange, etag) {
    const bounds = expandBounds(mapInstance.getBounds());
    const sw = bounds.getSouthWest();
    const ne = bounds.getNorthEast();
    const bbox = `${sw.lat.toFixed(5)},${sw.lng.toFixed(5)},${ne.lat.toFixed(5)},${ne.lng.toFixed(5)}`;

    // Always include weather so data is ready when the toggle turns on
    let url = `/api/stations?bbox=${bbox}&timerange=${timerange}&include=weather`;
    if (sinceCursor) url += `&since=${encodeURIComponent(sinceCursor)}`;

    const headers = {};
    if (etag) headers['If-None-Match'] = etag;

    const res = await fetch(url, { credentials: 'same-origin', headers });
    if (res.status === 304) return { status: 304, data: null, etag };
    if (res.status === 401) {
      window.location.hash = '#/login';
      throw new Error('Unauthorized');
    }
    if (!res.ok) throw new Error(`HTTP ${res.status}`);

    cachedBounds = bounds;
    return { status: 200, data: await res.json(), etag: res.headers.get('ETag') };
  }

  // --- Polling: setTimeout chain with backoff, dedup, ETag, visibility pause ---

  function startPolling(mapInstance, timerange) {
    let timer = null;
    let fetchInFlight = false;
    let backoff = 5000;
    let etag = null;
    const MAX_BACKOFF = 60000;

    async function poll() {
      if (fetchInFlight) return;
      fetchInFlight = true;

      // Check if viewport exceeds cached expanded bounds → full reload
      if (shouldFullReload(mapInstance.getBounds())) {
        sinceCursor = null;
        etag = null;
      }

      const isDelta = sinceCursor !== null;

      try {
        const resp = await fetchStations(mapInstance, timerange, etag);
        if (resp.status === 304) {
          backoff = 5000;
          pollStatus = 'live';
          lastFetchTime = Date.now();
          return;
        }

        const stations = resp.data;
        etag = resp.etag;
        backoff = 5000;
        pollStatus = 'live';
        lastFetchTime = Date.now();
        initialLoadDone = true;

        // Update markers — full reconciliation or delta merge
        stationLayer.update(stations, isDelta);
        stationLayer.pruneStale(timerange);
        stationLayer.applyVisibilityFilter(mapState.layerToggles);

        // Update trail and weather layers from station data
        if (mapState.layerToggles.trails) {
          trailLayer.update(stationLayer.getStations());
        }
        if (mapState.layerToggles.weather) {
          weatherLayer.update(stationLayer.getStations());
        }

        // Advance since cursor to max last_heard (stations sorted newest-first)
        if (stations.length > 0) {
          sinceCursor = stations[0].last_heard;
        }

        stationCount = stationLayer.markers.size;
      } catch (e) {
        console.error('Station poll error:', e);
        backoff = Math.min(backoff * 2, MAX_BACKOFF);
        pollStatus = backoff > 5000 ? 'error' : 'backoff';
      } finally {
        fetchInFlight = false;
      }
    }

    // Idempotent — always clears the previous timer before setting a new one
    function schedule() {
      clearTimeout(timer);
      timer = setTimeout(async () => {
        await poll();
        if (!document.hidden) schedule();
      }, backoff);
    }

    function onVisibility() {
      if (document.hidden) {
        clearTimeout(timer);
        timer = null;
      } else {
        poll();     // immediate catch-up
        schedule(); // safe — clearTimeout inside prevents double chain
      }
    }

    // Immediate re-fetch when user pans/zooms outside cached bounds
    function onMoveEnd() {
      if (shouldFullReload(mapInstance.getBounds())) {
        sinceCursor = null;
        etag = null;
        clearTimeout(timer);
        poll().then(() => { if (!document.hidden) schedule(); });
      }
    }

    document.addEventListener('visibilitychange', onVisibility);
    mapInstance.on('moveend', onMoveEnd);
    poll();
    schedule();

    return () => {
      clearTimeout(timer);
      document.removeEventListener('visibilitychange', onVisibility);
      mapInstance.off('moveend', onMoveEnd);
    };
  }

  // --- Own position marker ---

  async function fetchOwnPosition(mapInstance) {
    try {
      const res = await fetch('/api/position', { credentials: 'same-origin' });
      if (!res.ok) return;
      const pos = await res.json();
      if (!pos?.valid) return;
      ownMarker = L.marker([pos.lat, pos.lon], {
        icon: L.divIcon({
          className: 'own-position-marker',
          html: '<div class="own-position"></div>',
          iconSize: [14, 14],
          iconAnchor: [7, 7],
        }),
        zIndexOffset: 1000,
      }).addTo(mapInstance);
      ownMarker.bindTooltip('My Position', {
        permanent: false,
        direction: 'right',
        offset: [10, 0],
        className: 'callsign-label',
      });
    } catch (_) {}
  }

  // --- Map lifecycle ---

  onMount(() => {
    const m = L.map(container, {
      center: mapState.mapCenter,
      zoom: mapState.mapZoom,
      zoomControl: true,
      attributionControl: true,
    });

    L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
      attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>',
      maxZoom: 19,
    }).addTo(m);

    L.control.scale({ imperial: true, metric: true }).addTo(m);

    // Persist map center/zoom to localStorage via mapState
    m.on('moveend', () => {
      const c = m.getCenter();
      mapState.mapCenter = [c.lat, c.lng];
      mapState.mapZoom = m.getZoom();
    });

    // Coordinate display: mouse position on desktop, map center on mobile
    const isMobile = 'ontouchstart' in window;
    if (isMobile) {
      m.on('moveend', () => updateCoordText(m.getCenter()));
    } else {
      m.on('mousemove', (e) => updateCoordText(e.latlng));
      m.on('mouseout', () => updateCoordText(m.getCenter()));
    }
    updateCoordText(m.getCenter());

    // Escape key closes popups
    function onKeyDown(e) {
      if (e.key === 'Escape') m.closePopup();
    }
    document.addEventListener('keydown', onKeyDown);

    // Create layers
    stationLayer = new StationLayer(m);
    trailLayer = new TrailLayer(m);
    weatherLayer = new WeatherLayer(m);

    if (mapState.layerToggles.trails) trailLayer.show();
    if (mapState.layerToggles.weather) weatherLayer.show();

    // Handle container resizes (full-bleed CSS may apply after mount)
    const ro = new ResizeObserver(() => m.invalidateSize());
    ro.observe(container);

    // Set map state last — this triggers $effects
    map = m;

    fetchOwnPosition(m);

    const agoTimer = setInterval(updateAgoDisplay, 1000);

    return () => {
      clearInterval(agoTimer);
      ro.disconnect();
      document.removeEventListener('keydown', onKeyDown);
      if (ownMarker) ownMarker.remove();
      stationLayer.destroy();
      trailLayer.destroy();
      weatherLayer.destroy();
      m.remove();
      map = null;
    };
  });

  // Effect 1: Polling lifecycle — only restarts when server-affecting params change.
  // Layer toggles are client-only filters and do NOT belong here.
  $effect(() => {
    if (!map) return;
    const tr = mapState.timerange;
    sinceCursor = null;
    cachedBounds = null;
    return startPolling(map, tr);
  });

  // Effect 2: Layer visibility — client-side only, no re-fetch.
  $effect(() => {
    if (!map || !stationLayer) return;
    const toggles = mapState.layerToggles;

    stationLayer.applyVisibilityFilter(toggles);

    if (toggles.trails) {
      trailLayer.show();
      trailLayer.update(stationLayer.getStations());
    } else {
      trailLayer.hide();
    }

    if (toggles.weather) {
      weatherLayer.show();
      weatherLayer.update(stationLayer.getStations());
    } else {
      weatherLayer.hide();
    }
  });

  // --- Derived ---

  let timerangeLabel = $derived(
    TIMERANGE_OPTIONS.find((o) => o.value === mapState.timerange)?.label || '1 hour',
  );

  let statusDotColor = $derived(
    pollStatus === 'live' ? 'var(--color-success)' :
    pollStatus === 'backoff' ? 'var(--color-warning)' :
    pollStatus === 'error' ? 'var(--color-danger)' :
    'var(--color-text-dim)',
  );

  let statusLabel = $derived(
    pollStatus === 'live' ? 'live' :
    pollStatus === 'backoff' ? 'retrying' :
    pollStatus === 'error' ? 'error' : 'connecting',
  );

  // --- Handlers ---

  function handleTimerangeChange(e) {
    mapState.timerange = Number(e.target.value);
  }

  function toggleLayer(key) {
    mapState.layerToggles = { ...mapState.layerToggles, [key]: !mapState.layerToggles[key] };
  }
</script>

<div class="map-wrapper">
  <div class="map-container" bind:this={container}></div>

  {#if map}
    <!-- Layer control (top-left, below Leaflet zoom control) -->
    <div class="map-layer-control">
      <button
        class="layer-toggle-btn"
        onclick={() => layerControlOpen = !layerControlOpen}
        aria-label="Toggle layer controls"
      >
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <polygon points="12 2 2 7 12 12 22 7 12 2" />
          <polyline points="2 17 12 22 22 17" />
          <polyline points="2 12 12 17 22 12" />
        </svg>
      </button>
      {#if layerControlOpen}
        <div class="layer-panel">
          <label>
            <input type="checkbox" checked={mapState.layerToggles.stations} onchange={() => toggleLayer('stations')} />
            Stations
          </label>
          <label>
            <input type="checkbox" checked={mapState.layerToggles.aprsIs} onchange={() => toggleLayer('aprsIs')} />
            APRS-IS
          </label>
          <label>
            <input type="checkbox" checked={mapState.layerToggles.trails} onchange={() => toggleLayer('trails')} />
            Trails
          </label>
          <label>
            <input type="checkbox" checked={mapState.layerToggles.weather} onchange={() => toggleLayer('weather')} />
            Weather
          </label>
        </div>
      {/if}
    </div>

    <!-- Timerange dropdown (top-right) -->
    <div class="map-timerange">
      <select value={mapState.timerange} onchange={handleTimerangeChange}>
        {#each TIMERANGE_OPTIONS as opt}
          <option
            value={opt.value}
            disabled={opt.disabled}
            title={opt.disabled ? 'Requires persistent storage (not configured)' : ''}
          >{opt.label}</option>
        {/each}
      </select>
    </div>

    <!-- Coordinate display (bottom-right, above Leaflet attribution) -->
    <div class="map-coord-display">{coordText}</div>

    <!-- Status bar (bottom-center) -->
    <div class="map-status-bar">
      <span class="status-dot" style:background={statusDotColor}></span>
      <span>{statusLabel}</span>
      <span class="status-sep">&middot;</span>
      <span>{stationCount} station{stationCount !== 1 ? 's' : ''}</span>
      <span class="status-sep">&middot;</span>
      <span>{timerangeLabel}</span>
      {#if lastFetchAgo}
        <span class="status-sep">&middot;</span>
        <span>{lastFetchAgo}</span>
      {/if}
    </div>

    <!-- Empty state -->
    {#if stationCount === 0 && initialLoadDone}
      <div class="map-empty">No stations heard yet</div>
    {/if}
  {/if}
</div>

<style>
  /* ── Layout ──────────────────────────────────────── */

  .map-wrapper {
    position: relative;
    width: 100%;
    flex: 1;
    min-height: 0;
  }
  .map-container {
    width: 100%;
    height: 100%;
  }

  /* ── Layer control (top-left, below zoom) ────────── */

  .map-layer-control {
    position: absolute;
    top: 80px;
    left: 10px;
    z-index: 1000;
  }
  .layer-toggle-btn {
    display: none;
    align-items: center;
    justify-content: center;
    width: 34px;
    height: 34px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    color: var(--color-text-muted);
    cursor: pointer;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.4);
  }
  .layer-toggle-btn:hover {
    color: var(--color-text);
    background: var(--color-surface-raised);
  }
  .layer-panel {
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    padding: 8px 12px;
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--color-text);
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.4);
  }
  .layer-panel label {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 3px 0;
    color: var(--color-text-muted);
    cursor: pointer;
  }
  .layer-panel label:hover {
    color: var(--color-text);
  }

  /* ── Timerange dropdown (top-right) ──────────────── */

  .map-timerange {
    position: absolute;
    top: 10px;
    right: 10px;
    z-index: 1000;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    padding: 6px 8px;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.4);
  }
  .map-timerange select {
    background: transparent;
    border: none;
    color: var(--color-text);
    font-family: var(--font-mono);
    font-size: 12px;
    cursor: pointer;
    outline: none;
  }
  .map-timerange select option {
    background: var(--color-surface);
    color: var(--color-text);
  }
  .map-timerange select option:disabled {
    color: var(--color-text-dim);
  }

  /* ── Coordinate display (bottom-right) ───────────── */

  .map-coord-display {
    position: absolute;
    bottom: 26px;
    right: 10px;
    z-index: 1000;
    background: rgba(22, 27, 34, 0.85);
    color: var(--color-text-dim);
    font-family: var(--font-mono);
    font-size: 11px;
    padding: 2px 8px;
    border-radius: 3px;
    pointer-events: none;
  }

  /* ── Status bar (bottom-center) ──────────────────── */

  .map-status-bar {
    position: absolute;
    bottom: 20px;
    left: 50%;
    transform: translateX(-50%);
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 5px 14px;
    background: rgba(22, 27, 34, 0.9);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    font-size: 11px;
    color: var(--color-text-dim);
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.4);
    backdrop-filter: blur(4px);
    z-index: 1000;
    white-space: nowrap;
    pointer-events: none;
  }
  .status-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .status-sep {
    opacity: 0.5;
  }

  /* ── Empty state ─────────────────────────────────── */

  .map-empty {
    position: absolute;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    z-index: 1000;
    background: var(--color-surface);
    color: var(--color-text-dim);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    padding: 8px 16px;
    font-size: 13px;
    pointer-events: none;
  }

  /* ── Mobile ──────────────────────────────────────── */

  @media (max-width: 768px) {
    .layer-toggle-btn {
      display: flex;
    }
    .layer-panel {
      margin-top: 6px;
    }
    .map-coord-display {
      bottom: 20px;
      right: 6px;
      font-size: 10px;
    }
    .map-status-bar {
      bottom: 14px;
      font-size: 10px;
      padding: 4px 10px;
      gap: 4px;
    }
  }

  /* ── Leaflet → Chonky theme ────────────────────── */

  /* Popup */
  :global(.leaflet-popup-content-wrapper) {
    background: var(--color-surface);
    color: var(--color-text);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    font-family: var(--font-mono);
    font-size: 12px;
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.5);
    padding: 0;
  }
  :global(.leaflet-popup-content) {
    margin: 10px 14px;
    line-height: 1.5;
  }
  :global(.leaflet-popup-tip) {
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    box-shadow: none;
  }
  :global(.leaflet-popup-close-button) {
    color: var(--color-text-dim) !important;
    font-size: 18px;
    padding: 6px 8px !important;
  }
  :global(.leaflet-popup-close-button:hover) {
    color: var(--color-text) !important;
  }

  /* Tooltip (callsign labels) */
  :global(.leaflet-tooltip.callsign-label) {
    background: rgba(22, 27, 34, 0.9);
    color: #d4a040;
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 700;
    padding: 1px 4px;
    border: 1px solid var(--color-border-subtle);
    border-radius: 3px;
    box-shadow: none;
    white-space: nowrap;
    pointer-events: none;
  }
  :global(.leaflet-tooltip.callsign-label::before) {
    display: none;
  }

  /* Zoom controls */
  :global(.leaflet-control-zoom a) {
    background: var(--color-surface) !important;
    color: var(--color-text-muted) !important;
    border-color: var(--color-border) !important;
    font-family: var(--font-mono);
  }
  :global(.leaflet-control-zoom a:hover) {
    background: var(--color-surface-raised) !important;
    color: var(--color-text) !important;
  }

  /* Scale bar */
  :global(.leaflet-control-scale-line) {
    background: rgba(22, 27, 34, 0.85);
    color: var(--color-text-dim);
    border-color: var(--color-border);
    font-family: var(--font-mono);
    font-size: 10px;
  }

  /* Attribution */
  :global(.leaflet-control-attribution) {
    background: rgba(22, 27, 34, 0.75) !important;
    color: var(--color-text-dim) !important;
    font-family: var(--font-mono);
    font-size: 10px;
  }
  :global(.leaflet-control-attribution a) {
    color: var(--color-primary) !important;
  }

  /* ── APRS markers ──────────────────────────────── */

  :global(.aprs-marker) {
    background: none !important;
    border: none !important;
  }
  :global(.aprs-icon) {
    width: 24px;
    height: 24px;
    margin: 10px;
    background-size: 384px 144px;
  }
  :global(.aprs-icon-fallback) {
    width: 10px;
    height: 10px;
    margin: 17px;
    border-radius: 50%;
    background: var(--color-text-dim);
  }
  :global(.aprs-overlay) {
    width: 24px;
    height: 24px;
    position: absolute;
    top: 0;
    left: 0;
    background-size: 384px 144px;
  }

  /* ── Popup content ─────────────────────────────── */

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

  /* ── Weather labels ────────────────────────────── */

  :global(.wx-label) {
    background: none !important;
    border: none !important;
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

  /* ── Own position marker ───────────────────────── */

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
