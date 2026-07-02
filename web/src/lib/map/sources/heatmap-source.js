// Data source for the Live Map direct-RX heatmap layer. Pure URL/weight
// helpers are unit-tested under node --test; loadHeatmap wraps fetch.

// Build the /api/heatmap query for a viewport bbox and interval (seconds).
// bbox matches the server's parseBBox order: sw_lat,sw_lon,ne_lat,ne_lon.
export function heatmapUrl(bbox, timerangeSec) {
  const b = `${bbox.swLat.toFixed(5)},${bbox.swLon.toFixed(5)},${bbox.neLat.toFixed(5)},${bbox.neLon.toFixed(5)}`;
  const params = new URLSearchParams();
  params.set('bbox', b);
  params.set('timerange', String(Math.floor(timerangeSec)));
  return `/api/heatmap?${params.toString()}`;
}

// Return a shallow-copied feature list with a normalized weight property
// `w` = count / maxCount (0..1). The heatmap paint reads `w` so the color
// ramp is independent of absolute packet volume. Safe for empty input and
// maxCount <= 0.
export function normalizeFeatureWeights(features, maxCount) {
  if (!Array.isArray(features)) return [];
  const denom = maxCount > 0 ? maxCount : 0;
  return features.map((f) => ({
    ...f,
    properties: {
      ...f.properties,
      w: denom ? (f.properties?.count ?? 0) / denom : 0,
    },
  }));
}

// Fetch and parse the heatmap for the given viewport/interval. Returns
// { geojson, maxCount, unlocatable }. fetchFn is injectable for tests.
export async function loadHeatmap(bbox, timerangeSec, fetchFn = fetch) {
  const res = await fetchFn(heatmapUrl(bbox, timerangeSec), {
    credentials: 'same-origin',
  });
  if (!res.ok) {
    return { geojson: { type: 'FeatureCollection', features: [] }, maxCount: 0, unlocatable: 0 };
  }
  const body = await res.json();
  return {
    geojson: { type: 'FeatureCollection', features: body.features ?? [] },
    maxCount: body.properties?.max_count ?? 0,
    unlocatable: body.properties?.unlocatable ?? 0,
  };
}
