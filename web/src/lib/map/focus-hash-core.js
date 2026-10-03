// Pure parsing/comparison for #/map?focus=CALL&lat=…&lon=… deep-links,
// factored out of LiveMapV2.svelte so this exact logic -- which has
// caused three separate click-through bugs found only by manual testing
// (see docs/wiki/invariants.md #66, #67, #68) -- is unit-testable.
//
// parseFocusHash takes the hash string directly (not window.location)
// so it's callable from node --test without a browser; LiveMapV2.svelte
// wraps it as `parseFocusFromHash()` -> `parseFocusHash(window.location.hash)`.

/**
 * @param {string | null | undefined} hash e.g. "#/map?focus=W1ABC-9&lat=40&lon=-105"
 * @returns {{callsign: string, lat: number, lon: number} | null}
 */
export function parseFocusHash(hash) {
  const h = hash || '';
  const qIdx = h.indexOf('?');
  if (qIdx < 0) return null;
  const params = new URLSearchParams(h.slice(qIdx + 1));
  const lat = parseFloat(params.get('lat'));
  const lon = parseFloat(params.get('lon'));
  // lat/lon are required, not optional -- invariant #66: a station-focus
  // link that omits them is discarded entirely (even the callsign), since
  // this same parse result also drives the camera fly-to before the
  // station has loaded from a poll.
  if (!Number.isFinite(lat) || !Number.isFinite(lon)) return null;
  return { callsign: params.get('focus') || '', lat, lon };
}

/**
 * True when two parsed focus targets are the same navigation -- used to
 * skip redundant work on a hashchange event that isn't actually a new
 * focus target (invariant #68).
 * @param {{callsign: string, lat: number, lon: number} | null} a
 * @param {{callsign: string, lat: number, lon: number} | null} b
 */
export function sameFocus(a, b) {
  return !!a && !!b && a.callsign === b.callsign && a.lat === b.lat && a.lon === b.lon;
}
