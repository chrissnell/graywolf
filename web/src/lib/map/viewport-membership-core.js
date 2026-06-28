// Pure predicate behind the live map's viewport pruning. A station belongs to
// the current map view when ANY position in its accumulated trail falls inside
// the bbox -- not only its newest fix (positions[0]).
//
// A head-only test made a moving station's entire still-visible trail vanish
// the moment its newest position left the viewport: the server stopped
// returning it and the client pruned it (graywolf #413). Keeping membership
// any-position lets a track stay drawn while partly visible and drops it only
// once the whole track has scrolled off-screen, so pruning stays self-cleaning.
//
// This mirrors the server's stationcache.stationInBBox (inclusive bounds, any
// position) so the two layers agree -- see invariant #53. The one intentional
// difference: the client trail is length-capped (MAX_TRAIL_LEN), not
// time-trimmed, so it may scan slightly older breadcrumbs than the server's
// cutoff-trimmed set. pruneStale still removes whole stations by last_heard.
export function trailIntersectsBBox(positions, b) {
  if (!positions || positions.length === 0 || !b) return false;
  return positions.some(
    (p) => p.lat >= b.swLat && p.lat <= b.neLat &&
           p.lon >= b.swLon && p.lon <= b.neLon,
  );
}
