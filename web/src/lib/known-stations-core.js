// Pure load/cap logic for the persisted "known stations" map behind the
// new-station notification (stationNewTransport.js). Kept runes-free and
// localStorage-free so it's unit-testable under `node --test` (mirrors
// the layer-toggles-core.js / radar-frames-core.js split).
//
// The map is callsign -> last-heard epoch ms, NOT just a membership set:
// station-new-diff-core.js's diffNewlyHeard needs a real timestamp per
// callsign to decide whether enough time has passed since it was last
// heard to count as "new again" (the operator-configurable staleness
// threshold in notification-prefs-store.svelte.js). Persisted as a JSON
// array of [callsign, lastHeardMs] pairs so it round-trips through
// `new Map(pairs)` directly.
//
// Trimmed to the MAX_KNOWN_STATIONS most-recently-heard callsigns rather
// than cleared outright once over the cap (a full clear would re-flag
// every currently-active station as new the next time it beacons -- fine
// for a short-lived in-memory dedup set like messagesTransport.js's,
// wrong here since this map is the whole point of the feature).

export const MAX_KNOWN_STATIONS = 20_000;

/**
 * Parse a persisted JSON array of [callsign, lastHeardMs] pairs into a
 * Map. Corrupt, missing, or malformed input (and malformed individual
 * entries) yields an empty Map or drops just the bad entries.
 * @param {string | null} raw
 * @returns {Map<string, number>}
 */
export function parseKnownStations(raw) {
  const out = new Map();
  if (!raw) return out;
  let arr;
  try {
    arr = JSON.parse(raw);
  } catch {
    return out;
  }
  if (!Array.isArray(arr)) return out;
  for (const entry of arr) {
    if (!Array.isArray(entry) || entry.length !== 2) continue;
    const [callsign, lastHeardMs] = entry;
    if (typeof callsign !== 'string' || typeof lastHeardMs !== 'number' || !Number.isFinite(lastHeardMs)) continue;
    out.set(callsign, lastHeardMs);
  }
  return out;
}

/**
 * Serialize a callsign -> last-heard-ms Map back to the array persisted
 * in localStorage, evicting the least-recently-heard entries once over
 * MAX_KNOWN_STATIONS.
 * @param {Map<string, number>} known
 * @returns {Array<[string, number]>}
 */
export function serializeKnownStations(known) {
  const arr = Array.from(known.entries());
  if (arr.length <= MAX_KNOWN_STATIONS) return arr;
  arr.sort((a, b) => a[1] - b[1]);
  return arr.slice(arr.length - MAX_KNOWN_STATIONS);
}
