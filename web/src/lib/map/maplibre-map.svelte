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
  import { URLShieldRenderer } from '@americana/maplibre-shield-generator';
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
        const base = `https://maps.nw5w.com/${z}/${x}/${y}.mvt`;
        const url = bearerToken
          ? `${base}?t=${encodeURIComponent(bearerToken)}`
          : base;
        const res = await fetch(url, { signal });
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
  // don't re-fetch every time downloads change. The cache is in-memory
  // only; a full page reload always re-fetches from the network.
  //
  // Freshness: maps.nw5w.com serves style.json with `Cache-Control:
  // no-cache` so the browser revalidates with origin on every request
  // and we never sit on a stale style after a deploy. Tiles still go
  // through CF's edge cache untouched.
  //
  // Offline: we save the most recent successful response into
  // localStorage so that an operator who loaded the app online and
  // later went offline still gets a working style. A first-ever load
  // with no network can't be saved by anything we do here — it has to
  // fail visibly so the operator knows to come online once.
  const STYLE_URL = 'https://maps.nw5w.com/style/americana-roboto/style.json';
  const STYLE_CACHE_KEY = 'graywolf:upstream-style:v1';
  let cachedUpstreamStyle = null;

  async function fetchUpstreamStyle() {
    try {
      const res = await fetch(STYLE_URL);
      if (!res.ok) throw new Error(`fetch upstream style: ${res.status}`);
      const text = await res.text();
      try {
        localStorage.setItem(STYLE_CACHE_KEY, text);
      } catch {
        // Quota or disabled storage — non-fatal; next online load retries.
      }
      return JSON.parse(text);
    } catch (err) {
      const cached = localStorage.getItem(STYLE_CACHE_KEY);
      if (!cached) throw err;
      console.warn(
        '[graywolf] upstream style fetch failed, using cached fallback:',
        err,
      );
      return JSON.parse(cached);
    }
  }

  async function buildGraywolfStyle({ federated }) {
    if (!cachedUpstreamStyle) {
      cachedUpstreamStyle = await fetchUpstreamStyle();
    }
    // Deep clone so we don't mutate the cached upstream payload.
    const style = JSON.parse(JSON.stringify(cachedUpstreamStyle));
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

  // transformRequest: attach the bearer token as ?t=<token> on
  // maps.nw5w.com tile and tiles.json requests. URL-token auth keeps
  // requests CORS-simple (no preflight) and lets the worker share its
  // CF edge cache across operators (cache key is the canonical URL,
  // stripped of ?t=). /style/* stays anonymous; /download/ keeps the
  // Authorization-header path for any future browser-initiated download.
  function transformRequest(url) {
    if (url.startsWith('https://maps.nw5w.com/download/')) {
      if (bearerToken) {
        return { url, headers: { Authorization: `Bearer ${bearerToken}` } };
      }
      return { url };
    }
    if (url.startsWith('https://maps.nw5w.com/style/')) {
      return { url };
    }
    if (url.startsWith('https://maps.nw5w.com/') && bearerToken) {
      return { url: appendToken(url, bearerToken) };
    }
    return { url };
  }

  function appendToken(url, token) {
    const u = new URL(url);
    u.searchParams.set('t', token);
    return u.toString();
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
      new maplibregl.NavigationControl({
        showCompass: true,
        visualizePitch: true,
      }),
      'top-right',
    );
    map.addControl(
      new maplibregl.ScaleControl({ maxWidth: 100, unit: 'imperial' }),
      'bottom-left',
    );
    // Wire up the americana highway-shield generator. The americana
    // style references runtime-generated shield images via image IDs
    // like "shield\nUS:I\n70\n" -- one styleimagemissing event per
    // unique shield. The URLShieldRenderer fetches the shields.json
    // (which describes shield shape/color per route network) and
    // generates the artwork on demand. We restrict it to image IDs
    // that start with "shield" so non-shield missing images (e.g. POI
    // runtime sprites) fall through.
    new URLShieldRenderer(
      'https://maps.nw5w.com/style/americana/shields.json',
      {
        parse: (id) => {
          // image-id format: "shield\n<network>\n<ref>\n<name>"
          const parts = String(id).split('\n');
          return {
            network: parts[1] ?? '',
            ref: parts[2] ?? '',
            name: parts[3] ?? '',
          };
        },
        format: (network, ref, name) =>
          `shield\n${network}\n${ref}\n${name}`,
      },
    )
      .filterImageID((id) => {
        const s = String(id);
        if (!s.startsWith('shield\n')) return false;
        // Skip shields with no route number — americana defines these
        // (NHS corridors, named-only routes, unsigned co-routings) but
        // the renderer can't draw them and would emit "Didn't produce"
        // warnings for every tile. The style falls back to plain text.
        const ref = s.split('\n')[2] ?? '';
        return ref.length > 0;
      })
      .renderOnMaplibreGL(map);

    // Catch-all for image IDs the americana style asks for that no one
    // produces: provide a 1x1 transparent placeholder so MapLibre stops
    // emitting "could not be loaded" warnings and the rest of the map
    // renders. Shields with a non-empty `ref` are handled by the
    // URLShieldRenderer above and short-circuit here; shields with an
    // empty ref (NHS corridors, named-only routes, unsigned co-routings)
    // were filtered out of the renderer and DO need the placeholder, or
    // MapLibre logs once per tile that contains them.
    map.on('styleimagemissing', (e) => {
      if (!map || !e || !e.id) return;
      const id = String(e.id);
      if (id.startsWith('shield\n')) {
        const ref = id.split('\n')[2] ?? '';
        if (ref.length > 0) return; // URLShieldRenderer will handle it
      }
      if (map.hasImage && map.hasImage(id)) return;
      try {
        map.addImage(id, { width: 1, height: 1, data: new Uint8Array(4) });
      } catch {
        // Style may be mid-swap; ignore.
      }
    });
    // Mount the operator's data layers (stations, trails, weather,
    // my-position) as soon as the style *spec* is parsed -- do NOT wait
    // for the basemap's `load`, which only fires after every basemap
    // source has completed its first fetch. If maps.nw5w.com is slow,
    // unreachable, or its tiles.json hangs, `load` never fires and the
    // operator's stations silently fail to appear on what is otherwise
    // a working map. style.load fires as soon as `style._loaded` flips
    // true, which is all addSource/addLayer/Marker.addTo need.
    if (map.style?._loaded) {
      oncreate?.(map);
    } else {
      map.once('style.load', () => oncreate?.(map));
    }
    if (typeof window !== 'undefined') window.__gwMap = map;
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
     is sufficient and the buttons would clash with the FAB. Keep the
     compass so operators can still reset bearing after a rotate. */
  @media (max-width: 768px) {
    :global(.maplibregl-ctrl-top-right .maplibregl-ctrl-zoom-in),
    :global(.maplibregl-ctrl-top-right .maplibregl-ctrl-zoom-out) {
      display: none;
    }
  }
</style>
