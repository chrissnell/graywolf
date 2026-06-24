// Pure load/merge logic for the Live Map layer toggles. No runes, no DOM, no
// localStorage -- so it is unit-testable under `node --test`. LiveMapV2.svelte
// wraps this with $state and the actual localStorage read/write (mirrors the
// radar-frames-core.js / radar-frames.svelte.js split).
//
// The toggles are a per-browser preference persisted as one JSON blob under the
// gw_map_layer_toggles key, so unchecking e.g. Trails or Fixed Points survives
// navigating away and back.

// Default visibility for every Live Map layer toggle. All display layers start
// on; the RF reachability filters start off.
export const LAYER_TOGGLES_DEFAULTS = {
  stations: true,
  trails: true,
  weather: true,
  myPosition: true,
  fixedPoints: true,
  fronts: true,
  directRxOnly: false,
  rfOnly: false,
};

export const LAYER_TOGGLES_KEY = 'gw_map_layer_toggles';

// Parse a persisted toggle blob into a complete toggle set. Saved values are
// merged OVER the defaults so a toggle added in a later version picks up its
// default instead of becoming undefined, and a stale key from an old version is
// harmlessly ignored by consumers. Missing or corrupt input yields a fresh copy
// of the defaults. Always returns a new object (never the shared defaults).
export function parseLayerToggles(raw) {
  if (!raw) return { ...LAYER_TOGGLES_DEFAULTS };
  try {
    const saved = JSON.parse(raw);
    if (saved == null || typeof saved !== 'object') return { ...LAYER_TOGGLES_DEFAULTS };
    return { ...LAYER_TOGGLES_DEFAULTS, ...saved };
  } catch {
    return { ...LAYER_TOGGLES_DEFAULTS };
  }
}
