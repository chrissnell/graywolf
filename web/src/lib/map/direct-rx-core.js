// Pure predicate behind the Live Map "Direct RX" filter. A station qualifies
// only if it was heard directly on RF (RX, zero digi hops) at or after the
// given cutoff (milliseconds since epoch). This ages a direct hearing out of
// the selected time window even though the server keeps the fix classified as
// direct for display (graywolf issues #130 + #349).
//
// last_direct_heard is the server-supplied timestamp of the most recent direct
// reception. A zero/absent value means the station has never been heard
// directly. The Go zero time serializes as "0001-01-01T00:00:00Z", which
// parses to a negative epoch and therefore never passes a real cutoff.
export function directHeardWithin(station, cutoffMs) {
  const ts = station?.last_direct_heard;
  if (!ts) return false;
  const t = Date.parse(ts);
  if (Number.isNaN(t)) return false;
  return t >= cutoffMs;
}
