// Pure predicate behind the Live Map "RF Only" filter. A station qualifies
// when its current fix arrived over the air (RX) and did not reach us as
// Internet-to-RF gated traffic (the inner packet of an APRS third-party gate).
// Unlike "Direct RX" this keeps RF-digipeated stations (hops > 0); it only
// drops points whose latest reception was APRS-IS or Internet-to-RF gated.
//
// The check is against the current fix (positions[0]) only, never the whole
// trail: the marker is drawn at positions[0], so a station now arriving via
// APRS-IS must not stay visible under RF Only just because an older breadcrumb
// in its accumulated trail was once heard on RF (graywolf #394). For static
// stations the server folds the most RF-reachable copy of a fix into
// positions[0] (see stationcache rfRank), so a fixed station heard on RF and
// later re-beaconed via a gated/IS copy still qualifies. Note positions[0] can
// diverge from the popup's top-level direction/via badge in exactly that case
// (the cache overwrites the station-level fields with the latest packet
// unconditionally) -- positions[0] is the rfRank-protected copy and is the
// correct basis for RF reachability.
export function isRfOnly(station) {
  const p = station?.positions?.[0];
  return !!p && p.direction === 'RX' && !p.gated;
}
