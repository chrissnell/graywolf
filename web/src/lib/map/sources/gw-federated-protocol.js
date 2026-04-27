// gw-tile:// protocol handler — dispatches each tile request between
// downloaded PMTiles archives and the online maps.nw5w.com server.
//
// MapLibre calls our request() function for every tile (and tilejson,
// sprite, glyphs) request whose URL starts with gw-tile://. Style
// generation in maplibre-map.svelte rewrites the upstream americana
// style's tile URLs to gw-tile://{z}/{x}/{y}, so the handler here is
// the single dispatch point for offline-aware tile serving.
//
// State-bounds intersection is a coarse "does this state's bbox
// overlap this tile's bbox" check. False positives (tile is in the
// state's bbox but the PMTiles archive doesn't include data for that
// zoom) are handled by falling back to network. False negatives are
// not possible if STATE_BOUNDS is correct.

import { PMTiles } from 'pmtiles';
import { STATE_BOUNDS } from '../../maps/state-bounds.js';

// One PMTiles instance per slug, lazy-initialized on first use, kept
// alive for the session. The instances cache directory entries
// internally and reuse Range requests, so amortized cost is low.
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

// bboxIntersects: simple AABB-on-globe test (treating lon as 1D, no
// antimeridian wraparound). All US states except Alaska are entirely
// in the western hemisphere; Alaska's bounds in STATE_BOUNDS already
// exclude the Aleutian tail past the antimeridian.
function bboxIntersects(tileBBox, stateBBox) {
  const [tSLat, tWLon, tNLat, tELon] = tileBBox;
  const [[sSLat, sWLon], [sNLat, sELon]] = stateBBox;
  if (tNLat < sSLat || tSLat > sNLat) return false;
  if (tELon < sWLon || tWLon > sELon) return false;
  return true;
}

// findCoveringSlug: returns the first slug from `completedSlugs`
// whose bbox intersects the tile bbox, or null if none. We could
// extend to "best fit" later (smallest matching state), but for
// now first-match is fine — overlapping states (none in the US)
// would create a redundant fetch path.
function findCoveringSlug(tileBBox, completedSlugs) {
  for (const slug of completedSlugs) {
    const bounds = STATE_BOUNDS[slug];
    if (!bounds) continue;
    if (bboxIntersects(tileBBox, bounds)) {
      return slug;
    }
  }
  return null;
}

// createFederatedProtocol returns a MapLibre protocol handler.
// The caller (maplibre-map.svelte) provides:
//   completedSlugsProvider: () => Set<string>  -- live; checked per request
//   fetchOnline:           (z, x, y, signal) => Promise<Uint8Array>
//                          fetches the corresponding online tile;
//                          throws if not retrievable.
//
// MapLibre's addProtocol API (v4) signature:
//   request: (params, abortController) => Promise<{data: Uint8Array}>
// The abortController is provided by MapLibre and aborted when the
// tile is no longer needed (panned out of view).
export function createFederatedProtocol({ completedSlugsProvider, fetchOnline }) {
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

      const slug = findCoveringSlug(tileBBox, completed);
      const fallback = () =>
        fetchOnline(z, x, y, abortController.signal).then((data) => ({ data }));

      if (!slug) {
        // No offline coverage for this tile.
        return fallback();
      }

      // Try the local archive; on miss (or any error reading it),
      // fall through to network. A missing tile in the archive isn't
      // an error per se — the source style may request zooms outside
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
