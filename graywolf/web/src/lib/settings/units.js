// Shared unit formatting functions.
// All formatters read the global unit preference internally so callers
// never need to pass the current system.

import { unitsState } from './units-store.svelte.js';

export const FT_PER_M = 3.28084;
const KM_PER_MI = 1.60934;
const MPH_PER_KT = 1.15078;
const KMH_PER_KT = 1.852;
const FT_PER_MI = 5280;
const EM_DASH = '\u2014';

function invalid(v) {
  return v == null || Number.isNaN(v);
}

// Distance — input is statute miles
export function formatDistance(miles) {
  if (invalid(miles)) return EM_DASH;
  if (unitsState.isMetric) {
    const km = miles * KM_PER_MI;
    if (km < 1) return `${(km * 1000).toFixed(0)} m`;
    return `${km.toFixed(1)} km`;
  }
  if (miles < 0.1) return `${(miles * FT_PER_MI).toFixed(0)} ft`;
  return `${miles.toFixed(1)} mi`;
}

// Altitude — input is meters
export function formatAltitude(meters) {
  if (invalid(meters)) return EM_DASH;
  if (unitsState.isMetric) return `${meters.toFixed(0)} m`;
  return `${(meters * FT_PER_M).toFixed(0)} ft`;
}

// Speed — input is knots
export function formatSpeed(knots) {
  if (invalid(knots)) return EM_DASH;
  if (unitsState.isMetric) return `${(knots * KMH_PER_KT).toFixed(1)} km/h`;
  return `${(knots * MPH_PER_KT).toFixed(1)} mph`;
}
