// Pure logic for filling the fixed-coordinate GPS form from the current
// station position (GH #621). Kept free of Svelte runes and fetch so it
// runs under `node --test`; the fetch + form glue lives in routes/Gps.svelte.

// fixedCoordsFromPosition reduces a GET /api/position response to the
// string values the fixed-coordinate fields should show, or null when the
// response holds no GPS fix to copy. Only a live receiver fix counts:
// with the position source set to fixed coordinates the endpoint reports
// those same coordinates back (source "fixed"), and copying them into the
// form would be a no-op dressed up as a GPS read. Altitude comes back as
// null when the fix has none, so the caller can leave the optional
// altitude field untouched.
export function fixedCoordsFromPosition(pos) {
  if (!pos || typeof pos !== 'object') return null;
  if (pos.valid !== true || pos.source !== 'gps') return null;
  if (!Number.isFinite(pos.lat) || !Number.isFinite(pos.lon)) return null;
  return {
    lat: String(pos.lat),
    lon: String(pos.lon),
    alt: pos.has_alt && Number.isFinite(pos.alt_m) ? String(pos.alt_m) : null,
  };
}
