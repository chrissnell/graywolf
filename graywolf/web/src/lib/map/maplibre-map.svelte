<script>
  import { onMount, onDestroy, setContext } from 'svelte';
  import maplibregl from 'maplibre-gl';
  import 'maplibre-gl/dist/maplibre-gl.css';
  import { Protocol } from 'pmtiles';
  import { mapsState } from '../settings/maps-store.svelte.js';
  import { osmRasterStyle } from './sources/osm-raster.js';
  import { graywolfVectorStyle } from './sources/graywolf-vector.js';

  let { initialCenter = [-98, 39], initialZoom = 4, oncreate = null } = $props();

  let container;
  let map = null;
  let bearerToken = $state(null);

  // Register pmtiles:// protocol once per module load. Safe to register
  // even though Plan 1 doesn't actually serve PMTiles -- Plan 2 will.
  // MapLibre v4 added getProtocol; older versions don't have it, so we
  // optimistically try to add and ignore "already registered" errors.
  try {
    maplibregl.addProtocol('pmtiles', new Protocol().tile);
  } catch {
    // already registered (HMR) -- fine
  }

  function buildStyle() {
    if (mapsState.source === 'graywolf' && mapsState.registered) {
      return graywolfVectorStyle();
    }
    return osmRasterStyle();
  }

  // transformRequest: attach Bearer token to maps.nw5w.com requests
  // EXCEPT /style/* (must stay anonymous to keep CF edge cache shared).
  function transformRequest(url) {
    if (
      url.startsWith('https://maps.nw5w.com/') &&
      !url.startsWith('https://maps.nw5w.com/style/') &&
      bearerToken
    ) {
      return { url, headers: { Authorization: `Bearer ${bearerToken}` } };
    }
    return { url };
  }

  // Sync the in-memory token from the server. revealToken() hits the
  // ?include_token=1 GET; nothing else has access to the persisted
  // token after a page reload.
  async function syncToken() {
    if (mapsState.registered) {
      const t = await mapsState.revealToken();
      if (t) bearerToken = t;
    } else {
      bearerToken = null;
    }
  }

  onMount(async () => {
    await syncToken();
    map = new maplibregl.Map({
      container,
      style: buildStyle(),
      center: initialCenter,
      zoom: initialZoom,
      attributionControl: { compact: true },
      transformRequest,
    });
    map.addControl(
      new maplibregl.NavigationControl({ showCompass: false }),
      'top-right',
    );
    map.addControl(
      new maplibregl.ScaleControl({ maxWidth: 100, unit: 'imperial' }),
      'bottom-left',
    );
    setContext('maplibre-map', { getMap: () => map });
    map.once('load', () => oncreate?.(map));
  });

  // Track source/registered changes; re-apply the style. setStyle
  // preserves user-added sources/layers as long as they're not part
  // of the style itself, which is what we want -- Phase 4's station/
  // trail/weather layers add to the map directly and survive a swap.
  $effect(() => {
    const _src = mapsState.source;
    const _reg = mapsState.registered;
    if (!map) return;
    map.setStyle(buildStyle());
  });

  // When registered flips, refresh the token.
  $effect(() => {
    const _ = mapsState.registered;
    syncToken();
  });

  onDestroy(() => {
    map?.remove();
    map = null;
  });
</script>

<div bind:this={container} class="map-container" role="application" aria-label="Map"></div>

<style>
  .map-container {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    background: var(--color-surface);
  }
  /* Style MapLibre's built-in attribution control to honor theme tokens. */
  :global(.maplibregl-ctrl-attrib) {
    background: var(--map-attribution-bg) !important;
    color: var(--map-overlay-fg) !important;
    font-size: 11px;
  }
  :global(.maplibregl-ctrl-attrib a) {
    color: var(--map-overlay-fg) !important;
  }
</style>
