// Pure logic for the map popup's "Navigate" provider preference. No
// runes/DOM/localStorage, so it is unit-testable under `node --test`
// (see nav-provider-core.test.js) -- mirrors the layer-toggles-core.js /
// layer-toggles.svelte.js split.

// Order matters: this drives both the Preferences select and the request's
// required list order (Organic Maps first for its offline support).
export const NAV_PROVIDERS = [
  { value: 'organic', label: 'Organic Maps' },
  { value: 'google', label: 'Google Maps' },
  { value: 'apple', label: 'Apple Maps' },
];

const VALID_PROVIDERS = new Set(NAV_PROVIDERS.map((p) => p.value));

export function isValidProvider(v) {
  return VALID_PROVIDERS.has(v);
}

// normalizeProvider(v) -> the value if valid, else null (caller resolves
// the OS-based default). Mirrors parseLayerToggles's corrupt-input handling.
export function normalizeProvider(v) {
  return isValidProvider(v) ? v : null;
}

// detectDefaultProvider({ isAndroid, userAgent, platform }) -> provider value
//
// Per the feature's device-default table: Apple hardware (Mac/iPhone/iPad)
// defaults to Apple Maps; Android and everything else (Windows, Linux,
// unknown) defaults to Google Maps, the one option with no desktop-app
// ambiguity. Takes explicit inputs rather than reading navigator/Platform
// directly so this stays pure and testable.
export function detectDefaultProvider({ isAndroid = false, userAgent = '', platform = '' } = {}) {
  if (isAndroid) return 'google';
  const isApple = /Mac|iPhone|iPad|iPod/.test(platform) || /Macintosh|iPhone|iPad|iPod/.test(userAgent);
  return isApple ? 'apple' : 'google';
}
