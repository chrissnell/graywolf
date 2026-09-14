// Pure builders for the station popup's "Navigate" links. No MapLibre/DOM
// imports, so this is unit-testable under `node --test` (see nav-links.test.js).
//
// Organic Maps and Apple Maps ship desktop apps (macOS) as well as mobile
// ones, but desktop browsers have no Universal/App-Link handoff mechanism
// (that's an iOS/Android-only OS feature) -- a plain https:// link to e.g.
// maps.apple.com just opens the website even with the native app installed.
// So those two return BOTH a custom-scheme URL (`om://`, `maps://`), which
// the OS hands to the installed app on any platform/browser, AND an https
// fallback for when the app isn't installed; the caller (popup.js /
// LiveMapV2.svelte) tries the scheme first and only opens the fallback if
// the tab never lost focus (see openNativeOrFallback in LiveMapV2.svelte).
// Google Maps has no desktop app, so it stays a single https link -- mobile
// OSes already auto-handoff that URL to the installed app via their own
// verified App/Universal Links, no scheme trick needed.
//
// URL formats verified against each app's own source/docs:
// - Organic Maps: https://github.com/organicmaps/organicmaps (Ge0Parser, mwm_url.cpp) --
//   om://<lat>,<lon>[/<name>] (scheme) / https://omaps.app/<lat>,<lon>[/<name>] (fallback)
// - Google Maps: https://developers.google.com/maps/documentation/urls/get-started --
//   https://www.google.com/maps/search/?api=1&query=<lat>,<lon>
// - Apple Maps: https://developer.apple.com/library/archive/featuredarticles/iPhoneURLScheme_Reference --
//   maps://?ll=<lat>,<lon>&q=<name> (scheme) / https://maps.apple.com/?ll=<lat>,<lon>&q=<name> (fallback)

// Matches the precision already used for the "Add fixed beacon here" deep
// link (LiveMapV2.svelte) -- ~11cm, far finer than APRS position accuracy.
const COORD_PRECISION = 6;

function formatCoord(n) {
  return Number(n).toFixed(COORD_PRECISION);
}

// Organic Maps' own link generator joins a place name with underscores
// (e.g. "Zoo_Zürich", "Eiffel_Tower"); mirror that so the name renders
// cleanly as a path segment instead of raw %20 escapes.
function slug(name) {
  if (!name) return '';
  return encodeURIComponent(name.trim().replace(/\s+/g, '_'));
}

export function organicMapsLinks(lat, lon, name = '') {
  const coords = `${formatCoord(lat)},${formatCoord(lon)}`;
  const suffix = slug(name) ? `/${slug(name)}` : '';
  return {
    scheme: `om://${coords}${suffix}`,
    fallback: `https://omaps.app/${coords}${suffix}`,
  };
}

export function googleMapsUrl(lat, lon) {
  return `https://www.google.com/maps/search/?api=1&query=${formatCoord(lat)},${formatCoord(lon)}`;
}

export function appleMapsLinks(lat, lon, name = '') {
  const coords = `${formatCoord(lat)},${formatCoord(lon)}`;
  const q = name ? `&q=${encodeURIComponent(name)}` : '';
  return {
    scheme: `maps://?ll=${coords}${q}`,
    fallback: `https://maps.apple.com/?ll=${coords}${q}`,
  };
}

