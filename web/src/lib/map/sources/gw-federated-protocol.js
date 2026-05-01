// gw-tile:// protocol handler -- dispatches each tile request between
// downloaded PMTiles archives and the online maps.nw5w.com server.
//
// MapLibre calls our request() function for every tile (and tilejson,
// sprite, glyphs) request whose URL starts with gw-tile://. Style
// generation in maplibre-map.svelte rewrites the upstream americana
// style's tile URLs to gw-tile://{z}/{x}/{y}, so the handler here is
// the single dispatch point for offline-aware tile serving.
//
// Bounds intersection is a coarse "does this region's bbox overlap
// this tile's bbox" check. False positives (tile is in the region's
// bbox but the PMTiles archive doesn't include data for that zoom)
// are handled by falling back to network. False negatives are not
// possible if the bounds in the catalog are correct.

import { PMTiles } from 'pmtiles';

// One PMTiles instance per namespaced slug, lazy-initialized on first
// use, kept alive for the session. The slug is namespaced
// (state/<x>, country/<iso2>, province/<iso2>/<x>); slashes encode
// directly in the URL since the worker layout puts archives under
// matching subdirectories.
const archives = new Map();

function getArchive(slug) {
  let a = archives.get(slug);
  if (!a) {
    a = new PMTiles(`/tiles/${slug}.pmtiles`);
    archives.set(slug, a);
  }
  return a;
}

// tileToBBox: returns [southLat, westLon, northLat, eastLon] for a
// tile in Web Mercator coordinates. The (x, y) are integer tile
// indices at zoom z; (0, 0) is top-left in slippy-map convention.
function tileToBBox(z, x, y) {
  const n = Math.pow(2, z);
  const westLon = (x / n) * 360 - 180;
  const eastLon = ((x + 1) / n) * 360 - 180;
  const northLat = (Math.atan(Math.sinh(Math.PI * (1 - (2 * y) / n))) * 180) / Math.PI;
  const southLat = (Math.atan(Math.sinh(Math.PI * (1 - (2 * (y + 1)) / n))) * 180) / Math.PI;
  return [southLat, westLon, northLat, eastLon];
}

// bboxIntersects: AABB test against a [west, south, east, north] tuple
// (matches the catalog bbox shape and PMTiles convention). All
// catalog regions stay clear of the antimeridian; bbox-crossing
// regions (e.g. the Aleutian tail) trim their bounds upstream.
function bboxIntersects(tileBBox, bboxWSEN) {
  const [tSLat, tWLon, tNLat, tELon] = tileBBox;
  const [w, s, e, n] = bboxWSEN;
  if (tNLat < s || tSLat > n) return false;
  if (tELon < w || tWLon > e) return false;
  return true;
}

// findCoveringSlug: returns the first slug from `completedSlugs`
// whose bbox intersects the tile bbox, or null if none. The bounds
// come from the live catalog via boundsBySlug.
function findCoveringSlug(tileBBox, completedSlugs, boundsBySlug) {
  for (const slug of completedSlugs) {
    const bbox = boundsBySlug.get(slug);
    if (!bbox) continue;
    if (bboxIntersects(tileBBox, bbox)) return slug;
  }
  return null;
}

// createFederatedProtocol returns a MapLibre protocol handler.
// The caller (maplibre-map.svelte) provides:
//   completedSlugsProvider: () => Set<string>  -- live; checked per request
//   boundsBySlugProvider:   () => Map<string, [west, south, east, north]>
//                              -- catalog-derived bounds, live per request
//   fetchOnline:           (z, x, y, signal) => Promise<Uint8Array>
//                          fetches the corresponding online tile;
//                          throws if not retrievable.
//
// MapLibre's addProtocol API (v4) signature:
//   request: (params, abortController) => Promise<{data: Uint8Array}>
// The abortController is provided by MapLibre and aborted when the
// tile is no longer needed (panned out of view).
export function createFederatedProtocol({ completedSlugsProvider, boundsBySlugProvider, fetchOnline }) {
  return {
    request(params, abortController) {
      const m = /^gw-tile:\/\/(\d+)\/(\d+)\/(\d+)$/.exec(params.url);
      if (!m) {
        return Promise.reject(new Error(`gw-tile: malformed URL ${params.url}`));
      }
      const z = parseInt(m[1], 10);
      const x = parseInt(m[2], 10);
      const y = parseInt(m[3], 10);
      const tileBBox = tileToBBox(z, x, y);
      const completed = completedSlugsProvider();
      const bounds = boundsBySlugProvider();

      const slug = findCoveringSlug(tileBBox, completed, bounds);
      const fallback = () =>
        fetchOnline(z, x, y, abortController.signal).then((data) => ({ data }));

      if (!slug) {
        // No offline coverage for this tile.
        return fallback();
      }

      // Try the local archive; on miss (or any error reading it),
      // fall through to network. A missing tile in the archive isn't
      // an error per se -- the source style may request zooms outside
      // the archive's stored range.
      return getArchive(slug)
        .getZxy(z, x, y, abortController.signal)
        .then((tile) => {
          if (tile && tile.data) {
            return { data: new Uint8Array(tile.data) };
          }
          return fallback();
        })
        .catch(() => fallback());
    },
  };
}
