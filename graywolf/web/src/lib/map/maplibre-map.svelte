<script module>
  // Module-level guard so HMR re-evaluating the component module
  // doesn't double-register the gw-tile protocol with MapLibre.
  let gwTileRegistered = false;
</script>

<script>
  import { onMount, onDestroy, setContext } from 'svelte';
  import maplibregl from 'maplibre-gl';
  import 'maplibre-gl/dist/maplibre-gl.css';
  import { Protocol } from 'pmtiles';
  import { mapsState } from '../settings/maps-store.svelte.js';
  import { osmRasterStyle } from './sources/osm-raster.js';
  import { downloadsState } from '../maps/downloads-store.svelte.js';
  import { createFederatedProtocol } from './sources/gw-federated-protocol.js';

  let { initialCenter = [-98, 39], initialZoom = 4, oncreate = null } = $props();

  let container;
  let map = null;
  let bearerToken = $state(null);

  // Set context synchronously during component init -- setContext after
  // an await throws lifecycle_outside_component because Svelte's
  // current_component is only set during the synchronous setup phase.
  // Children (Phase 4 layers) call getMap() after the map is created
  // via the oncreate callback, so reading the closed-over `map` later
  // is fine.
  setContext('maplibre-map', { getMap: () => map });

  // Register pmtiles:// protocol once per module load. Safe to register
  // even though Plan 1 doesn't actually serve PMTiles -- Plan 2 will.
  // MapLibre v4 added getProtocol; older versions don't have it, so we
  // optimistically try to add and ignore "already registered" errors.
  try {
    maplibregl.addProtocol('pmtiles', new Protocol().tile);
  } catch {
    // already registered (HMR) -- fine
  }

  // gw-tile:// must be registered inside the component because its
  // fetchOnline closure needs to read the live `bearerToken` per
  // request. A module-level guard prevents double-registration on HMR.
  function ensureGwTileProtocol() {
    if (gwTileRegistered) return;
    const federated = createFederatedProtocol({
      completedSlugsProvider: () => downloadsState.completed,
      fetchOnline: async (z, x, y, signal) => {
        const url = `https://maps.nw5w.com/${z}/${x}/${y}.mvt`;
        const headers = bearerToken
          ? { Authorization: `Bearer ${bearerToken}` }
          : {};
        const res = await fetch(url, { headers, signal });
        if (!res.ok) {
          throw new Error(`tile ${z}/${x}/${y} fetch failed: ${res.status}`);
        }
        return new Uint8Array(await res.arrayBuffer());
      },
    });
    try {
      maplibregl.addProtocol('gw-tile', federated.request);
      gwTileRegistered = true;
    } catch {
      gwTileRegistered = true; // assume already registered on prior HMR
    }
  }

  // Cache the upstream americana style.json across style swaps so we
  // don't re-fetch every time downloads change.
  let cachedUpstreamStyle = null;

  // hasIncompatibleExpression scans a style.layers entry and returns
  // true if any layout/paint property references an expression that
  // MapLibre v4 cannot parse. The americana style uses `global-state`
  // (a v5 expression) on a couple of highway-shield text-field layers;
  // those layers are dropped so the rest of the style still renders.
  // The visible loss is the dynamic shield numbering -- everything
  // else (roads, water, labels, POIs) is unaffected.
  const V5_ONLY_EXPRESSIONS = ['global-state'];
  function hasIncompatibleExpression(layer) {
    const stringify = (v) => {
      try {
        return JSON.stringify(v);
      } catch {
        return '';
      }
    };
    const blobs = [stringify(layer.layout), stringify(layer.paint), stringify(layer.filter)];
    const json = blobs.join('|');
    return V5_ONLY_EXPRESSIONS.some((expr) => json.includes(`"${expr}"`));
  }

  async function buildGraywolfStyle({ federated }) {
    if (!cachedUpstreamStyle) {
      const res = await fetch(
        'https://maps.nw5w.com/style/americana-roboto/style.json',
      );
      if (!res.ok) throw new Error(`fetch upstream style: ${res.status}`);
      cachedUpstreamStyle = await res.json();
    }
    // Deep clone so we don't mutate the cached upstream payload.
    const style = JSON.parse(JSON.stringify(cachedUpstreamStyle));
    style.layers = style.layers.filter((l) => !hasIncompatibleExpression(l));
    if (federated) {
      for (const src of Object.values(style.sources)) {
        if (src.type === 'vector') {
          delete src.url; // drop the tilejson pointer
          src.tiles = ['gw-tile://{z}/{x}/{y}'];
        }
      }
    }
    return style;
  }

  async function buildStyle() {
    if (mapsState.source === 'graywolf' && mapsState.registered) {
      return await buildGraywolfStyle({
        federated: downloadsState.completed.size > 0,
      });
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
    ensureGwTileProtocol();
    // Hydrate mapsState + downloadsState before the first style build
    // so the first paint reflects the persisted source choice and any
    // already-downloaded states. Without this, mapsState.source defaults
    // to 'osm' on a direct page-load to /map even when the operator has
    // selected Graywolf in settings, and the very first style is OSM.
    await Promise.all([
      mapsState.fetchConfig(),
      downloadsState.refresh(),
    ]);
    await syncToken();
    const initialStyle = await buildStyle();
    map = new maplibregl.Map({
      container,
      style: initialStyle,
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
    map.once('load', () => oncreate?.(map));
  });

  // Track source/registered/completed-downloads changes; re-apply the
  // style. setStyle preserves user-added sources/layers as long as
  // they're not part of the style itself, which is what we want --
  // Phase 4's station/trail/weather layers add to the map directly
  // and survive a swap.
  $effect(() => {
    const _src = mapsState.source;
    const _reg = mapsState.registered;
    const _dlcount = downloadsState.completed.size;
    if (!map) return;
    buildStyle()
      .then((style) => {
        if (map) map.setStyle(style);
      })
      .catch((err) => {
        console.warn('build map style failed:', err);
      });
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
  /* Hide MapLibre's +/- zoom buttons on touch viewports — pinch-zoom
     is sufficient and the buttons would clash with the FAB. */
  @media (max-width: 768px) {
    :global(.maplibregl-ctrl-top-right .maplibregl-ctrl-group) {
      display: none;
    }
  }
</style>
