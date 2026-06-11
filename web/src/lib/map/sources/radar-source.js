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

// v1 ships raster. Flip to RADAR_BACKEND_VECTOR once GRA-48 tiles are live.
export const ACTIVE_RADAR_BACKEND = RADAR_BACKEND_RASTER;

// Tile base. In production this points at the origin Worker (R2-backed,
// edge-cached). For local dev you may point RADAR_TILE_BASE straight at IEM:
//   https://mesonet.agron.iastate.edu/cache/tile.py/1.0.0
// Production flips it to the Worker with no other code change (per GRA-42).
export const RADAR_TILE_BASE = 'https://mesonet.agron.iastate.edu/cache/tile.py/1.0.0';

const RADAR_ATTRIBUTION = 'NEXRAD via NWS / Iowa State Mesonet';
const RADAR_SOURCE_ID = 'radar-tiles';

// Build an XYZ tile-URL template under the configured base.
export function radarTileUrl(product, ext) {
  return `${RADAR_TILE_BASE}/${product}/{z}/{x}/{y}.${ext}`;
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
  throw new Error(`unsupported radar backend: ${backend}`);
}
