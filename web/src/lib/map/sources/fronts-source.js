// Provider descriptor for the surface-fronts overlay. One GeoJSON source (no
// per-frame loop, unlike radar): a single document holds the current analysis
// and the overlay renders whatever features it carries. Region-agnostic.
//
// Pure data + small builders only -- no MapLibre or DOM imports -- so it is
// unit-testable under `node --test`, mirroring radar-source.js.
//
// Auth: the GeoJSON data URL is fetched by MapLibre as a source request, so the
// map's transformRequest appends the bearer token (?t=) exactly as it does for
// radar tiles -- no token wiring is needed on the data URL here. The manifest is
// a plain fetch (transformRequest doesn't see it), so the caller appends ?t=
// itself, the same pattern as radarManifestUrl().

// Same origin Worker as radar (see RADAR_TILE_BASE in radar-source.js). The
// Worker serves the fronts product under /fronts/*.
export const FRONTS_BASE = 'https://maps.nw5w.com';

export const FRONTS_SOURCE_ID = 'fronts';
export const FRONTS_MANIFEST_URL = `${FRONTS_BASE}/fronts/manifest.json`;
export const FRONTS_DATA_URL = `${FRONTS_BASE}/fronts/latest.geojson`;

// WMO frontal symbology colors. Cold = blue, warm = red, stationary alternates
// (rendered as a neutral violet line in v1), occluded = purple, trough = brown.
export const FRONT_COLORS = {
  cold: '#1565d8',
  warm: '#d81e1e',
  stationary: '#7a5cff',
  occluded: '#8e24aa',
  trough: '#b8702a',
};

export function frontsProvider() {
  return {
    sourceId: FRONTS_SOURCE_ID,
    source: { type: 'geojson', data: FRONTS_DATA_URL },
    dataUrl: FRONTS_DATA_URL,
    manifestUrl: FRONTS_MANIFEST_URL,
  };
}

// Model-derived GLOBAL fronts (GFS Thermal Front Parameter). Same schema and
// styling as the WPC source, served under /fronts/world/* and tagged
// region:"world". Rendered BENEATH the WPC layers so the analyst product wins
// over North America and the model shows through everywhere else.
export const FRONTS_WORLD_SOURCE_ID = 'fronts-world';
export const FRONTS_WORLD_MANIFEST_URL = `${FRONTS_BASE}/fronts/world/manifest.json`;
export const FRONTS_WORLD_DATA_URL = `${FRONTS_BASE}/fronts/world/latest.geojson`;

export function frontsWorldProvider() {
  return {
    sourceId: FRONTS_WORLD_SOURCE_ID,
    source: { type: 'geojson', data: FRONTS_WORLD_DATA_URL },
    dataUrl: FRONTS_WORLD_DATA_URL,
    manifestUrl: FRONTS_WORLD_MANIFEST_URL,
  };
}
