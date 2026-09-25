// Pure base-callsign matching, mirroring pkg/messages/blocklist_set.go's
// BlocklistSet.Blocked(): a bare (no-SSID) callsign matches every SSID of
// that base call, while an SSID-qualified callsign matches only that
// exact station. Used by favoriteStationsStore.svelte.js so a favorite
// entered as e.g. "KV4S" alerts for KV4S-9/KV4S-1/etc, but NOT for an
// unrelated callsign that merely shares a prefix, like KV4SM (operator's
// own example, 2026-07-31: "it needs to be exact callsign... I don't
// want it to notify me if KV4SM beacons").

/** @param {string} callsign already-uppercased */
export function baseCallsign(callsign) {
  const idx = callsign.indexOf('-');
  return idx === -1 ? callsign : callsign.slice(0, idx);
}

/**
 * @param {string} favorite the stored favorite entry's callsign
 * @param {string} heard the callsign just heard on the air
 * @returns {boolean} true if `favorite` should match `heard`
 */
export function callsignMatches(favorite, heard) {
  if (!favorite || !heard) return false;
  const fav = favorite.toUpperCase();
  const call = heard.toUpperCase();
  if (fav === call) return true;
  const base = baseCallsign(call);
  // Only a callsign that actually HAS an SSID (base !== call) can match
  // via its base -- otherwise a bare heard callsign would match itself
  // twice through two different branches, and a bare favorite would
  // incorrectly appear to match a same-length-prefix callsign it isn't
  // actually a base of (base === call for one with no "-").
  return base !== call && base === fav;
}
