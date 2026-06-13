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

// NWS reflectivity color ramp, keyed by the dBZ lower bound of each band.
// Used by the vector backend's fill-color expression and by any legend UI.
export const DBZ_BANDS = [5, 10, 15, 20, 25, 30, 35, 40, 45, 50, 55, 60, 65, 70, 75];
export const DBZ_COLORS = {
  5: '#04e9e7', 10: '#019ff4', 15: '#0300f4', 20: '#02fd02', 25: '#01c501',
  30: '#008e00', 35: '#fdf802', 40: '#e5bc00', 45: '#fd9500', 50: '#fd0000',
  55: '#d40000', 60: '#bc0000', 65: '#f800fd', 70: '#9854c6', 75: '#fdfdfd',
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
const RADAR_SOURCE_ID = 'radar-tiles';

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
      source: {
        type: 'vector',
        // Origin Worker resolves the `latest` pointer GRA-48 publishes to R2.
        tiles: [vectorTileUrl()],
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
          'source-layer': 'radar', // MVT layer name produced by GRA-48
          // fill-antialias MUST be false for stacked dBZ bands: with it on,
          // MapLibre draws an antialiased outline on every band edge, and the
          // feathered edge between two adjacent bands leaks the basemap through
          // as a hairline seam (even though the generator's band geometry is
          // coincident). false => hard edges that tile cleanly, no seams.
          paint: { 'fill-color': buildDbzFillColor(), 'fill-antialias': false },
        },
      ],
      opacity: { property: 'fill-opacity', layerIds: ['radar-fill'] },
      // The vector route is cycle-less (the Worker always serves the latest
      // frame), but MapLibre caches vector tiles in-memory and will not refetch
      // when a new frame publishes. radar.js calls cacheBust() on a cadence and
      // swaps in a new `?v=` template so MapLibre treats it as a new source
      // revision and refetches; the Worker ignores the param.
      cacheBust: (v) => [vectorTileUrl(v)],
    };
  }
  throw new Error(`unsupported radar backend: ${backend}`);
}

// Vector contour tile template. The Worker route is cycle-less; an optional
// cache-bust token `v` (a time bucket, see frameBucket) is appended so a newly
// published frame looks like a new source revision to MapLibre.
export function vectorTileUrl(v) {
  const bust = v == null ? '' : `?v=${v}`;
  return `${RADAR_TILE_BASE}/radar/{z}/{x}/{y}.pbf${bust}`;
}

// Cadence-aligned cache-bust token. GRA-48 republishes on a ~5-minute cycle,
// so a 5-minute bucket changes about once per new frame.
export function frameBucket(nowMs) {
  return Math.floor(nowMs / 300000);
}
