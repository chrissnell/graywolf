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

// bboxArea: lon-span * lat-span of a [w, s, e, n] tuple. Used only to
// rank overlapping archives by specificity, so a plain rectangular
// area (no spherical correction) is sufficient and monotonic.
function bboxArea([w, s, e, n]) {
  return Math.max(0, e - w) * Math.max(0, n - s);
}

// rankCoveringSlugs: returns every slug from `completedSlugs` whose
// bbox intersects the tile, ordered smallest-bbox-first. A more
// specific regional archive is therefore tried before the
// globe-spanning world archive, so a user who has both renders the
// region at full detail. Bounds come from the live catalog via
// boundsBySlug.
export function rankCoveringSlugs(tileBBox, completedSlugs, boundsBySlug) {
  const hits = [];
  for (const slug of completedSlugs) {
    const bbox = boundsBySlug.get(slug);
    if (!bbox) continue;
    if (bboxIntersects(tileBBox, bbox)) hits.push({ slug, area: bboxArea(bbox) });
  }
  hits.sort((a, b) => a.area - b.area);
  return hits.map((h) => h.slug);
}

// createFederatedProtocol returns a MapLibre protocol handler.
// The caller (maplibre-map.svelte) provides:
//   completedSlugsProvider: () => Set<string>  -- live; checked per request
//   boundsBySlugProvider:   () => Map<string, [west, south, east, north]>
//                              -- catalog-derived bounds, live per request
//   maxZoomBySlugProvider: () => Map<string, number>  -- optional;
//                          per-archive top zoom (0/absent = no cap).
//                          Lets the world archive be skipped for zooms
//                          it cannot hold instead of reading and missing.
//   fetchOnline:           (z, x, y, signal) => Promise<Uint8Array>
//                          fetches the corresponding online tile;
//                          throws if not retrievable.
//
// MapLibre's addProtocol API (v4) signature:
//   request: (params, abortController) => Promise<{data: Uint8Array}>
// The abortController is provided by MapLibre and aborted when the
// tile is no longer needed (panned out of view).
export function createFederatedProtocol({
  completedSlugsProvider,
  boundsBySlugProvider,
  maxZoomBySlugProvider,
  fetchOnline,
}) {
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
      const maxZoomBySlug =
        typeof maxZoomBySlugProvider === 'function' ? maxZoomBySlugProvider() : new Map();

      const ranked = rankCoveringSlugs(tileBBox, completed, bounds);
      const fallback = () =>
        fetchOnline(z, x, y, abortController.signal).then((data) => ({ data }));

      if (ranked.length === 0) {
        // No offline coverage for this tile.
        return fallback();
      }

      // Walk the ranked list (most-specific archive first); the first
      // archive that actually holds the tile wins. Network is the final
      // fallback. A missing tile in an archive isn't an error per se --
      // the source style may request zooms outside the archive's range.
      const tryNext = (i) => {
        if (i >= ranked.length) return fallback();
        const slug = ranked[i];
        const cap = maxZoomBySlug.get(slug);
        // Skip archives that provably cannot hold this zoom (e.g. the
        // world archive at z>cap). Avoids a guaranteed-miss range read;
        // the source maxzoom makes MapLibre overzoom instead.
        if (typeof cap === 'number' && cap > 0 && z > cap) return tryNext(i + 1);
        return getArchive(slug)
          .getZxy(z, x, y, abortController.signal)
          .then((tile) => {
            if (tile && tile.data) return { data: new Uint8Array(tile.data) };
            return tryNext(i + 1);
          })
          .catch(() => tryNext(i + 1));
      };
      return tryNext(0);
    },
  };
}
