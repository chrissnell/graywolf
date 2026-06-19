// Pure mappers between the server's fixed-point JSON (snake_case,
// latitude/longitude/symbol_table) and the shape the map layer consumes
// (lat/lon/table). Kept free of DOM and network so they are unit-tested
// directly. See fixed-points-store.svelte.js for the stateful store that
// uses them.

// fixedPointFromApi converts one server record into the layer shape, or
// returns null for a malformed record so a single bad row can't crash
// the layer (mirrors the old localStorage load() guard).
export function fixedPointFromApi(p) {
  if (
    !p ||
    typeof p.name !== 'string' ||
    !Number.isFinite(p.latitude) ||
    !Number.isFinite(p.longitude)
  ) {
    return null;
  }
  return {
    id: p.id,
    name: p.name,
    table: p.symbol_table || '/',
    symbol: p.symbol || '/',
    overlay: p.overlay || '',
    lat: p.latitude,
    lon: p.longitude,
  };
}

// fixedPointToApi converts a dialog result (client shape) into the POST
// request body the server expects.
export function fixedPointToApi({ name, table, symbol, overlay, lat, lon }) {
  return {
    name: (name || '').trim(),
    symbol_table: table || '/',
    symbol: symbol || '/',
    overlay: overlay || '',
    latitude: lat,
    longitude: lon,
  };
}
