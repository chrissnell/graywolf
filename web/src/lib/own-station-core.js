// Pure helper for stationNewTransport.js: builds the set of callsigns
// this graywolf instance itself transmits as, so "new station" / "favorite
// station" notifications never fire for the operator's own station.
//
// Two sources, both exact CALL[-SSID] strings (never a base-callsign
// wildcard -- unlike favorites/exclusions, this must NOT also match other
// SSIDs of the same base call: the operator's own KV4S-1 home station and
// their separate KV4S-7 handheld are different stations, and a new-station
// alert for KV4S-7 is exactly what's wanted, per the operator's own
// worked example):
//   - the base Station Callsign (GET /api/station/config's `callsign`,
//     what every beacon transmits as unless overridden per-beacon).
//   - any per-beacon Callsign override (GET /api/beacons' `callsign`
//     field; empty means "inherit from station callsign", so only
//     non-empty overrides add anything beyond the base callsign above).

/**
 * @param {string|null|undefined} stationCallsign GET /api/station/config's `callsign`
 * @param {Array<{callsign?: string}>|null|undefined} beacons GET /api/beacons response
 * @returns {Set<string>} uppercased, exact CALL[-SSID] strings this instance transmits as
 */
export function buildOwnCallsignSet(stationCallsign, beacons) {
  const set = new Set();
  const base = (stationCallsign || '').trim().toUpperCase();
  if (base) set.add(base);
  for (const b of beacons || []) {
    const override = (b && b.callsign || '').trim().toUpperCase();
    if (override) set.add(override);
  }
  return set;
}

/**
 * @param {Set<string>} ownCallsigns from buildOwnCallsignSet
 * @param {string|null|undefined} callsign a roster row's callsign
 * @returns {boolean}
 */
export function isOwnStation(ownCallsigns, callsign) {
  if (!callsign) return false;
  return ownCallsigns.has(callsign.trim().toUpperCase());
}
