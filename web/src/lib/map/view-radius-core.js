// Pure lat/lon bounding-box math + persisted-value parsing for "N miles
// around a center point", behind the operator-configurable default map
// view radius (LiveMapV2.svelte's "Reset to default view" button,
// mapState.defaultViewRadiusMiles). A fixed MapLibre zoom level doesn't
// reliably represent "N miles" -- meters-per-pixel varies with both zoom
// and viewport width/height -- so the button instead computes a bounding
// box and calls map.fitBounds(), which adapts to whatever the viewport
// actually is.

// Operator's explicit choice (2026-07-31). Both "Center on my station"
// and "Reset to default view" share this one setting so they're always
// visibly consistent with each other -- see LiveMapV2.svelte's
// fitDefaultRadius -- and it's editable from the layer card, so this is
// just the shipped starting point, not a load-bearing constant.
export const DEFAULT_VIEW_RADIUS_MILES = 15;
export const MIN_VIEW_RADIUS_MILES = 1;
export const MAX_VIEW_RADIUS_MILES = 500;

const MILES_PER_DEGREE_LAT = 69.0;

/**
 * @param {number} lat center latitude
 * @param {number} lon center longitude
 * @param {number} radiusMiles
 * @returns {{south: number, west: number, north: number, east: number}}
 */
export function boundsAroundMiles(lat, lon, radiusMiles) {
  const milesPerDegLon = MILES_PER_DEGREE_LAT * Math.cos((lat * Math.PI) / 180);
  const dLat = radiusMiles / MILES_PER_DEGREE_LAT;
  // Guard the degenerate near-pole case (cos(lat) -> 0, so a degree of
  // longitude covers ~0 miles and dLon would blow up to an enormous
  // span) -- APRS stations are never actually there, but stay finite
  // regardless of input.
  const dLon = radiusMiles / Math.max(milesPerDegLon, 0.01);
  return {
    south: lat - dLat,
    west: lon - dLon,
    north: lat + dLat,
    east: lon + dLon,
  };
}

/** @param {unknown} stored value read back from localStorage (miles, as a string) */
export function parseViewRadiusMiles(stored) {
  const n = stored == null ? NaN : parseFloat(stored);
  if (!Number.isFinite(n) || n <= 0) return DEFAULT_VIEW_RADIUS_MILES;
  return Math.min(Math.max(n, MIN_VIEW_RADIUS_MILES), MAX_VIEW_RADIUS_MILES);
}
