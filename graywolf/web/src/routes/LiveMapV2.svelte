<script>
  // LiveMapV2: MapLibre-based replacement for LiveMap.svelte (Leaflet).
  // Currently scaffolds the stations + trails + weather + hover-path
  // layers and click-to-popup; my-position, info panel, time-range, and
  // coord display are added in tasks 25-28. Cutover at task 29.

  import { onDestroy } from 'svelte';
  import maplibregl from 'maplibre-gl';
  import MaplibreMap from '../lib/map/maplibre-map.svelte';
  import { createDataStore } from '../lib/map/data-store.svelte.js';
  import { mountStationsLayer } from '../lib/map/layers/stations.js';
  import { mountTrailsLayer } from '../lib/map/layers/trails.js';
  import { mountWeatherLayer } from '../lib/map/layers/weather.js';
  import { mountHoverPathLayer } from '../lib/map/layers/hover-path.js';
  import { mountMyPositionLayer } from '../lib/map/layers/my-position.js';
  import { renderStationPopupHTML } from '../lib/map/popup.js';
  import { unitsState } from '../lib/settings/units-store.svelte.js';

  const dataStore = createDataStore();
  let stationsLayer = null;
  let trailsLayer = null;
  let weatherLayer = null;
  let hoverPathLayer = null;
  let myPositionLayer = null;
  let mapRef = null;
  let activePopup = null;

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
        // If myPosition has a matching station record, show its hover path.
        // The /api/position DTO does not currently include a callsign, so
        // this gracefully no-ops; left wired for forward compatibility.
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
  });
</script>

<div class="livemap-shell">
  <MaplibreMap oncreate={onMapReady} />
</div>

<style>
  .livemap-shell {
    position: absolute;
    inset: 0;
    overflow: hidden;
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

  /* Station popup: theme-aware container + tip + close button. The
     interior structure (.stn-popup, .stn-hdr, ...) is styled by the
     legacy LiveMap.svelte's :global rules for now and migrates to a
     shared stylesheet at task 29 cutover. */
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

  /* Own position marker. Copied verbatim from LiveMap.svelte:844 so the
     MapLibre marker DOM (which is outside this component's scope) picks
     up the same styling. Migrates to a shared stylesheet at task 31. */
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
