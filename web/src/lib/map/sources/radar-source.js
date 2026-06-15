// graywolf/web/src/lib/map/sources/radar-source.js
//
// Single source of truth for the Live Map radar overlay. Pure data + small
// builders only -- no MapLibre or DOM imports -- so it is unit-testable under
// `node --test` and so the raster (v1) and vector (GRA-48) backends share one
// palette and one tile-base.
//
// GRA-48 INTEGRATION SEAM: when the Rust contour generator's MVT tiles are
// live on the origin Worker, flip ACTIVE_RADAR_BACKEND to RADAR_BACKEND_VECTOR.
// Nothing else in the client changes -- radar.js and LiveMapV2 consume the
// descriptor returned by radarProvider() and are backend-agnostic.

// RainViewer "Universal Blue" reflectivity color ramp, keyed by the dBZ lower
// bound of each band. Used by the vector backend's fill-color expression and by
// any legend UI. Values are RainViewer's published rain palette taken at each
// band's lower bound; per-band alpha is dropped (opacity is the global
// fill-opacity slider's job, and the test contract requires 6-digit hex). The
// two lightest bands (5/10) sit below RainViewer's visible threshold -- where
// the source palette is a near-transparent haze -- so we continue the cyan ramp
// downward to keep light returns reading as faint blue, matching the reference.
export const DBZ_BANDS = [5, 10, 15, 20, 25, 30, 35, 40, 45, 50, 55, 60, 65, 70, 75];
export const DBZ_COLORS = {
  5: '#c5f0f7', 10: '#a8e6f0', 15: '#88ddee', 20: '#00a3e0', 25: '#0077aa',
  30: '#005588', 35: '#ffee00', 40: '#ffaa00', 45: '#ff4400', 50: '#c10000',
  55: '#ffaaff', 60: '#ff77ff', 65: '#ffffff', 70: '#ffffff', 75: '#00ff00',
};

export const RADAR_BACKEND_RASTER = 'raster';
export const RADAR_BACKEND_VECTOR = 'vector';

// GRA-48 contour tiles are live on the origin Worker, so the overlay now
// serves the smoothed vector backend. Flip back to RADAR_BACKEND_RASTER to
// fall back to the IEM raster pull-through.
export const ACTIVE_RADAR_BACKEND = RADAR_BACKEND_VECTOR;

// Tile host. Points at the graywolf-maps origin Worker, which serves radar
// under /radar/*: the raster product is an edge-cached pull-through of IEM
// (so we don't hotlink a university server in production), and /radar/{z}/{x}/{y}.pbf
// is the GRA-48 contour vector tiles from R2. The Worker rides the same bearer
// token as the basemap (the client's transformRequest appends ?t=), so no
// extra auth wiring is needed here. To test against IEM directly in local dev,
// temporarily set this to the IEM base and drop the `/radar` segment in
// radarTileUrl: https://mesonet.agron.iastate.edu/cache/tile.py/1.0.0
export const RADAR_TILE_BASE = 'https://maps.nw5w.com';

const RADAR_ATTRIBUTION = 'NEXRAD via NWS / Iowa State Mesonet';
const RAINVIEWER_ATTRIBUTION = 'Radar © RainViewer';
const RADAR_SOURCE_ID = 'radar-tiles';

// Coverage region -- an axis orthogonal to the US backend (raster|vector). The
// US overlay is high-fidelity NEXRAD (radarProvider); the rest of the world is
// the RainViewer global composite, proxied as a raster overlay by the origin
// Worker under /radar/rainviewer/*. The operator picks the region on the maps
// tab; default is US.
export const RADAR_REGION_US = 'us';
export const RADAR_REGION_WORLD = 'world';
export const ACTIVE_RADAR_REGION = RADAR_REGION_US;

// Build an XYZ tile-URL template under the Worker's /radar/ namespace.
export function radarTileUrl(product, ext) {
  return `${RADAR_TILE_BASE}/radar/${product}/{z}/{x}/{y}.${ext}`;
}

// MapLibre `step` expression mapping a polygon's `dbz` property to the NWS
// ramp. Output below the first stop is the lowest band's color.
export function buildDbzFillColor() {
  // Base output is the lowest band's color (a dbz==first-band polygon falls
  // below the first stop and takes it); stops begin at the second band.
  const expr = ['step', ['get', 'dbz'], DBZ_COLORS[DBZ_BANDS[0]]];
  for (let i = 1; i < DBZ_BANDS.length; i++) {
    expr.push(DBZ_BANDS[i], DBZ_COLORS[DBZ_BANDS[i]]);
  }
  return expr;
}

// Uniform descriptor consumed by radar.js. `layers` is ordered; `opacity`
// tells the layer module which paint property and which layer ids the opacity
// slider drives (raster-opacity for raster, fill-opacity for vector).
export function radarProvider(backend = ACTIVE_RADAR_BACKEND) {
  if (backend === RADAR_BACKEND_RASTER) {
    return {
      sourceId: RADAR_SOURCE_ID,
      source: {
        type: 'raster',
        tiles: [radarTileUrl('nexrad-n0q', 'png')],
        tileSize: 256,
        // IEM's N0Q composite tops out ~z10; cap so MapLibre overzooms the
        // last available tile instead of requesting non-existent z11+ tiles.
        maxzoom: 10,
        attribution: RADAR_ATTRIBUTION,
      },
      layers: [
        {
          id: 'radar-raster',
          type: 'raster',
          source: RADAR_SOURCE_ID,
          // Cheap browser bilinear -- harmless, marginal at native zoom.
          paint: { 'raster-resampling': 'linear' },
        },
      ],
      opacity: { property: 'raster-opacity', layerIds: ['radar-raster'] },
    };
  }
  if (backend === RADAR_BACKEND_VECTOR) {
    return {
      sourceId: RADAR_SOURCE_ID,
      // `tiles` is intentionally omitted: this is a per-frame provider, so the
      // tile template depends on the current frame ts. radar.js injects
      // `tiles: frameTiles(ts)` once the manifest yields a frame.
      source: {
        type: 'vector',
        // The generated archive only covers z3-z8 (national CONUS ~1 km).
        // Without bounds MapLibre would request z>maxzoom tiles (Worker 404 ->
        // blank above the data) and waste requests below minzoom; maxzoom lets
        // it overzoom (scale) the z8 tile instead. Keep in sync with the
        // generator's deployed --min-zoom / --max-zoom.
        minzoom: 3,
        maxzoom: 8,
        attribution: RADAR_ATTRIBUTION,
      },
      layers: [
        {
          id: 'radar-fill',
          type: 'fill',
          source: RADAR_SOURCE_ID,
          'source-layer': 'radar', // MVT layer name produced by the generator
          // fill-antialias MUST be false for stacked dBZ bands: with it on,
          // MapLibre draws an antialiased outline on every band edge, and the
          // feathered edge between two adjacent bands leaks the basemap through
          // as a hairline seam (even though the generator's band geometry is
          // coincident). false => hard edges that tile cleanly, no seams.
          paint: { 'fill-color': buildDbzFillColor(), 'fill-antialias': false },
        },
      ],
      opacity: { property: 'fill-opacity', layerIds: ['radar-fill'] },
      // Per-frame loop: each frame is an immutable URL keyed by its epoch ts
      // (radar/<ts>/{z}/{x}/{y}.pbf). radar.js calls setFrameTs(ts) to swap the
      // tile template; the ts IS the cache key, so no `?v=` cache-bust is needed.
      perFrame: true,
      frameTiles: (ts) => [vectorTileUrl(ts)],
    };
  }
  throw new Error(`unsupported radar backend: ${backend}`);
}

// Rest-of-world overlay: the RainViewer global composite, animated as a
// per-frame raster loop by the origin Worker (/radar/rainviewer/*). The Worker
// now exposes the full frame list (/radar/rainviewer/manifest.json) and an
// immutable per-frame tile route (/radar/rainviewer/{ts}/{z}/{x}/{y}.png), so
// the world overlay uses the SAME smooth per-frame machinery as the US vector
// loop: radar.js preloads one raster source per frame at opacity 0 and animates
// by handing opacity between already-cached frames -- no setTiles, no refetch.
// `tiles` is omitted (the template depends on the frame ts); radar.js injects
// `tiles: frameTiles(ts)`. maxzoom caps at RainViewer's native z7 so MapLibre
// overzooms the last real tile instead of requesting non-existent z8+ tiles.
export function worldRadarProvider() {
  return {
    sourceId: RADAR_SOURCE_ID,
    source: {
      type: 'raster',
      tileSize: 256,
      maxzoom: 7,
      attribution: RAINVIEWER_ATTRIBUTION,
    },
    layers: [
      {
        id: 'radar-raster',
        type: 'raster',
        source: RADAR_SOURCE_ID,
        paint: { 'raster-resampling': 'linear' },
      },
    ],
    opacity: { property: 'raster-opacity', layerIds: ['radar-raster'] },
    // Per-frame loop: each frame is an immutable URL keyed by its epoch ts
    // (radar/rainviewer/<ts>/{z}/{x}/{y}.png). The ts IS the cache key, so no
    // `?v=` cache-bust is needed (and the Worker 400s any stray query param).
    perFrame: true,
    frameTiles: (ts) => [rainviewerFrameTileUrl(ts)],
  };
}

// Legacy latest-frame RainViewer raster template under /radar/rainviewer/.
// Retained for reference/back-compat: the Worker still serves it, but the world
// overlay now animates via the per-frame route below rather than this single
// always-latest URL. Carries NO query string (the Worker 400s any param on
// /radar/* except the auth token transformRequest appends).
export function rainviewerTileUrl() {
  return `${RADAR_TILE_BASE}/radar/rainviewer/{z}/{x}/{y}.png`;
}

// Per-frame RainViewer raster template. `ts` is a 10-digit Unix epoch naming an
// immutable frame on the origin Worker, which resolves it to that frame's
// RainViewer path and proxies the tile (long-immutable). Byte-stable per ts, so
// no cache-bust param -- the ts is the cache key.
export function rainviewerFrameTileUrl(ts) {
  return `${RADAR_TILE_BASE}/radar/rainviewer/${ts}/{z}/{x}/{y}.png`;
}

// The RainViewer loop manifest URL (single source of truth for which world
// frames exist). Distinct from the US contour manifest. The bearer token (?t=)
// is appended by the caller's fetch, as with radarManifestUrl().
export function rainviewerManifestUrl() {
  return `${RADAR_TILE_BASE}/radar/rainviewer/manifest.json`;
}

// Region-aware provider seam consumed by radar.js. US delegates to the backend
// logic (vector contours today); world returns the RainViewer raster overlay.
// Region is orthogonal to the US backend, so flipping ACTIVE_RADAR_BACKEND and
// switching regions are independent.
export function radarProviderForRegion(region = ACTIVE_RADAR_REGION) {
  return region === RADAR_REGION_WORLD ? worldRadarProvider() : radarProvider();
}

// Per-frame vector contour tile template. `ts` is a 10-digit Unix epoch naming
// an immutable archive (radar/<ts>.pmtiles) on the origin Worker; the URL is
// byte-stable forever, so it carries no cache-bust param.
export function vectorTileUrl(ts) {
  return `${RADAR_TILE_BASE}/radar/${ts}/{z}/{x}/{y}.pbf`;
}

// The loop manifest URL (single source of truth for which frames exist). The
// bearer token (?t=) is appended by the caller's fetch, since this is a plain
// fetch and not a MapLibre tile request (transformRequest doesn't see it).
export function radarManifestUrl() {
  return `${RADAR_TILE_BASE}/radar/manifest.json`;
}

// Parse a radar manifest into an oldest-first list of { ts, iso } frames. The
// manifest's `frames` is newest-first; the loop animates oldest -> newest, so
// reverse here. Returns [] for anything that isn't a schema_version 1 manifest
// with a frames array (so a transient bad/empty body is treated as "no frames"
// rather than throwing into the poll loop).
export function parseManifestFrames(json) {
  if (!json || typeof json !== 'object') return [];
  if (json.schema_version !== 1 || !Array.isArray(json.frames)) return [];
  const frames = [];
  for (const f of json.frames) {
    if (!f || typeof f.ts !== 'number' || typeof f.iso !== 'string') continue;
    frames.push({ ts: f.ts, iso: f.iso });
  }
  // newest-first -> oldest-first
  frames.reverse();
  return frames;
}

// Parse a RainViewer loop manifest into an oldest-first list of { ts, iso }.
// The RainViewer manifest's frames are `{ ts }` only (no iso, unlike the US
// contour manifest), so synthesize iso from ts to keep the frame shape uniform
// for the loop store and slider label. Same newest-first -> oldest-first
// reversal and same "[] on a bad/empty body" contract as parseManifestFrames.
export function parseRainviewerManifestFrames(json) {
  if (!json || typeof json !== 'object') return [];
  if (json.schema_version !== 1 || !Array.isArray(json.frames)) return [];
  const frames = [];
  for (const f of json.frames) {
    if (!f || typeof f.ts !== 'number') continue;
    frames.push({ ts: f.ts, iso: new Date(f.ts * 1000).toISOString() });
  }
  frames.reverse();
  return frames;
}

// Region-aware manifest URL + parser, so the loop store can poll the correct
// loop (US contour vs RainViewer world) without branching on region itself.
export function radarManifestUrlForRegion(region) {
  return region === RADAR_REGION_WORLD ? rainviewerManifestUrl() : radarManifestUrl();
}
export function parseManifestFramesForRegion(region, json) {
  return region === RADAR_REGION_WORLD
    ? parseRainviewerManifestFrames(json)
    : parseManifestFrames(json);
}
