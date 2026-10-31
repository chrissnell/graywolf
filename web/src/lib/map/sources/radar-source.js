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

// Reflectivity color ramp for liquid precipitation, keyed by the dBZ lower
// bound of each band. Used by the vector backend's fill-color expression and by
// any legend UI; per-band alpha is dropped (opacity is the global fill-opacity
// slider's job, and the test contract requires 6-digit hex).
//
// WHY THE WEAK BANDS ARE GREEN. This used to be RainViewer's "Universal Blue"
// ramp, which spends its six weakest bands (5-30) on cyan and blue. Winter
// precipitation lives almost entirely in that same 10-30 dBZ window, and snow
// is blue on every radar product anyone recognizes -- so blue cannot mean both
// "light rain" and "snow". Liquid moved to the classic NWS green/yellow/red
// progression and blue became snow's alone (see PTYPE_COLORS).
//
// The extreme end (65+) keeps the conventional magenta/purple/white, which
// overlaps the deepest sleet pinks. That is deliberate and harmless: sleet
// above 35 dBZ essentially does not occur, so the two never draw together.
export const DBZ_BANDS = [5, 10, 15, 20, 25, 30, 35, 40, 45, 50, 55, 60, 65, 70, 75];
export const DBZ_COLORS = {
  5: '#cdfccd', 10: '#9bf79b', 15: '#5fe85f', 20: '#22cc22', 25: '#11a011',
  30: '#0a7a0a', 35: '#ffee00', 40: '#ffaa00', 45: '#ff4400', 50: '#e60000',
  55: '#c10000', 60: '#9a0000', 65: '#f800fd', 70: '#9854c6', 75: '#ffffff',
};

// Surface precipitation type codes carried on the `ptype` MVT attribute. This
// is a cross-repo contract with the generator (`radar-contour/src/ptype.rs`);
// never renumber. 0 means the generator had no type information (no model data,
// or a source with no polarimetric moments) and renders on the liquid ramp.
export const PTYPE_NONE = 0;
export const PTYPE_RAIN = 1;
export const PTYPE_SNOW = 2;
export const PTYPE_SLEET = 3;
export const PTYPE_FREEZING_RAIN = 4;
export const PTYPE_MIXED = 5;

// Per-type ramps for frozen and mixed precipitation. Hues follow NWS hazard
// convention so they read correctly to anyone who has seen a winter-weather
// graphic: snow blue, sleet pink, freezing rain violet, mixed neutral.
//
// These stop at 40 dBZ on purpose. Frozen precipitation is a weak scatterer and
// lives in the 5-40 window; a `step` with no stops above 40 holds the darkest
// color for anything stronger, which is the right answer for the rare very
// heavy wet snow rather than running off the end of a ramp nobody can read.
//
// WHY EACH TYPE HAS ITS OWN LIGHTNESS RANGE, not just its own hue. Red-green
// dichromats (roughly 8 percent of men) keep lightness and blue-yellow but lose
// red-green, so hue-only separation collapses for them. Two pairs collapse
// specifically: the two blues (snow and freezing rain) and the two
// near-neutrals (sleet, which desaturates toward yellow, and mixed). Each of
// those PAIRS is therefore split in lightness -- snow light / freezing rain
// dark, sleet light / mixed dark -- while the two pairs stay far apart in
// chroma, so their lightness ranges are free to overlap.
//
// An earlier draft separated all four by hue alone and measured 1-10 deltaE
// between snow and freezing rain under simulated deuteranopia and protanopia,
// i.e. indistinguishable, on exactly the pair where confusion matters most:
// freezing rain is the hazard that brings down power lines, snow is the benign
// case. Every pair is now >= 21 deltaE in every band that publishes, under
// normal vision and both dichromacies.
//
// The consequence to know about: lightness now encodes type as well as
// intensity, so a light snow and a dark freezing rain at the same dBZ no longer
// look equally strong. Intensity still reads monotonically *within* a type,
// which is what a reader actually compares. `radar-source.test.js` pins all of
// this, dichromacy simulation included, so it cannot be undone by eye.
export const PTYPE_BANDS = [5, 10, 15, 20, 25, 30, 35, 40];
export const PTYPE_COLORS = {
  [PTYPE_SNOW]: {
    5: '#e4f0fe', 10: '#cde3fe', 15: '#b1d5fd', 20: '#97c6fc',
    25: '#7ab6fc', 30: '#5ea7fb', 35: '#3f96fa', 40: '#2085f9',
  },
  [PTYPE_SLEET]: {
    5: '#fedcec', 10: '#fec9e1', 15: '#fdb4d6', 20: '#fc9dc9',
    25: '#fc84bc', 30: '#fb6bae', 35: '#fa4d9e', 40: '#f9117d',
  },
  [PTYPE_FREEZING_RAIN]: {
    5: '#b179fc', 10: '#a767fb', 15: '#9c54fb', 20: '#9040fa',
    25: '#8226f9', 30: '#6e06f5', 35: '#6006d7', 40: '#5305ba',
  },
  [PTYPE_MIXED]: {
    5: '#988881', 10: '#8d7c74', 15: '#81716a', 20: '#74665f',
    25: '#675a54', 30: '#5c504b', 35: '#4f4540', 40: '#433a36',
  },
};

// Human labels, ready for a legend. No legend UI exists yet -- color encoded one
// dimension (intensity) before this and was self-evident; it now encodes two
// (type x intensity), so a key is a genuine follow-up, not polish.
export const PTYPE_LABELS = {
  [PTYPE_NONE]: 'Precipitation',
  [PTYPE_RAIN]: 'Rain',
  [PTYPE_SNOW]: 'Snow',
  [PTYPE_SLEET]: 'Sleet',
  [PTYPE_FREEZING_RAIN]: 'Freezing rain',
  [PTYPE_MIXED]: 'Mixed',
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

// PALETTE PARITY WITH THE WORLD OVERLAY IS DELIBERATELY DROPPED. The weak bands
// used to continue a cyan ramp downward to match the RainViewer global
// composite, which is what `worldRadarProvider` still serves -- RainViewer's own
// baked-in Universal Blue PNGs, which we cannot recolor client-side. Now that
// blue means snow in US mode, blue still means light rain in world mode, and the
// operator can switch regions at will. Parity was worth less than telling snow
// from rain: the US overlay is the high-fidelity product and the one where
// winter type information exists at all. Revisit only if the world composite
// ever gains a recolorable vector form.
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

// MapLibre `step` expression over a polygon's `dbz` property. Output below the
// first stop is the lowest band's color.
function buildStep(bands, colors) {
  // Base output is the lowest band's color (a dbz==first-band polygon falls
  // below the first stop and takes it); stops begin at the second band.
  const expr = ['step', ['get', 'dbz'], colors[bands[0]]];
  for (let i = 1; i < bands.length; i++) {
    expr.push(bands[i], colors[bands[i]]);
  }
  return expr;
}

// The liquid-precipitation intensity ramp. Still exported on its own because a
// legend wants the plain ramp, and because `ptype` 0/1 both resolve to it.
export function buildDbzFillColor() {
  return buildStep(DBZ_BANDS, DBZ_COLORS);
}

// The intensity ramp for one frozen/mixed type.
export function buildPtypeFillColor(ptype) {
  const colors = PTYPE_COLORS[ptype];
  if (!colors) return buildDbzFillColor();
  return buildStep(PTYPE_BANDS, colors);
}

// Full fill-color expression: switch palette on the surface precipitation type,
// then ramp on intensity within it. Rain and "no type information" share the
// default branch, so a tile generated before `ptype` existed -- or by a source
// with no polarimetric moments -- renders exactly as it always did.
export function buildRadarFillColor() {
  return [
    'match',
    ['get', 'ptype'],
    PTYPE_SNOW, buildPtypeFillColor(PTYPE_SNOW),
    PTYPE_SLEET, buildPtypeFillColor(PTYPE_SLEET),
    PTYPE_FREEZING_RAIN, buildPtypeFillColor(PTYPE_FREEZING_RAIN),
    PTYPE_MIXED, buildPtypeFillColor(PTYPE_MIXED),
    buildDbzFillColor(),
  ];
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
          paint: { 'fill-color': buildRadarFillColor(), 'fill-antialias': false },
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
