// Pure predicate behind the Live Map "RF Only" filter. A station qualifies
// when its current fix arrived over the air (RX) and did not reach us as
// Internet-to-RF gated traffic (the inner packet of an APRS third-party gate).
// Unlike "Direct RX" this keeps RF-digipeated stations (hops > 0); it only
// drops points whose latest reception was APRS-IS or Internet-to-RF gated.
//
// The check is against the current fix (positions[0]) only, never the whole
// trail: the marker and popup label a station by its newest reception, so a
// station now arriving via APRS-IS must not stay visible under RF Only just
// because an older breadcrumb in its accumulated trail was once heard on RF
// (graywolf #394). For static stations the server already folds the most
// RF-reachable copy of a fix into positions[0] (see stationcache rfRank), so a
// fixed station heard on RF and later re-beaconed via a gated/IS copy still
// qualifies.
export function isRfOnly(station) {
  const p = station?.positions?.[0];
  return !!p && p.direction === 'RX' && !p.gated;
}
