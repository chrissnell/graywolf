// Pick the basemap style, falling back to OSM when the graywolf style
// can't be built.
//
// The graywolf style is fetched from /api/maps/style, which 502s when
// the graywolf host can't reach maps.nw5w.com and has no cached copy.
// Without a fallback that failure left the operator staring at an empty
// gray container with no controls or stations (graywolf#636). OSM raster
// tiles are fetched by the browser directly, so they often still work
// in exactly that situation.
//
// Callers are expected not to cache a failed graywolf build, so the next
// call (a later style rebuild or the next visit to the map) retries it.
export async function selectBasemapStyle({
  useGraywolf,
  buildGraywolf,
  buildFallback,
  onFallback,
  onRecovered,
}) {
  if (!useGraywolf) return buildFallback();
  try {
    const style = await buildGraywolf();
    onRecovered?.();
    return style;
  } catch (err) {
    onFallback?.(err);
    return buildFallback();
  }
}
