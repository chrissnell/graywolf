<script>
  // LiveMapV2: MapLibre-based replacement for LiveMap.svelte (Leaflet).
  // Currently scaffolds the stations layer only; trails, weather,
  // hover-path, popups, my-position, info panel, time-range, and coord
  // display are added in tasks 21-28. Cutover at task 29.

  import { onDestroy } from 'svelte';
  import MaplibreMap from '../lib/map/maplibre-map.svelte';
  import { createDataStore } from '../lib/map/data-store.svelte.js';
  import { mountStationsLayer } from '../lib/map/layers/stations.js';
  import { mountTrailsLayer } from '../lib/map/layers/trails.js';
  import { mountWeatherLayer } from '../lib/map/layers/weather.js';
  import { unitsState } from '../lib/settings/units-store.svelte.js';

  const dataStore = createDataStore();
  let stationsLayer = null;
  let trailsLayer = null;
  let weatherLayer = null;
  let mapRef = null;

  function onMapReady(map) {
    mapRef = map;
    // Trails first so the line sits beneath the (DOM) station markers
    // and below the weather labels in symbol-layer order.
    trailsLayer = mountTrailsLayer(map, () => dataStore.stations);
    weatherLayer = mountWeatherLayer(map, () => dataStore.stations);
    stationsLayer = mountStationsLayer(map, () => dataStore.stations);

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
    if (stationsLayer) stationsLayer.refresh();
    if (trailsLayer) trailsLayer.refresh();
    if (weatherLayer) weatherLayer.refresh();
  });

  onDestroy(() => {
    dataStore.stop();
    stationsLayer?.destroy();
    trailsLayer?.destroy();
    weatherLayer?.destroy();
    stationsLayer = null;
    trailsLayer = null;
    weatherLayer = null;
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
</style>
