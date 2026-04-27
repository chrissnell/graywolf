// Theme registry — reads the shipped manifest at
// graywolf/web/themes/themes.json and exposes it to the rest of the app.
//
// Source of truth: graywolf/web/themes/themes.json. Vite can import
// JSON directly, so adding or removing a theme means editing exactly
// one manifest + dropping a .css file next to it. No other edits
// anywhere in src/.

import manifest from '../../../themes/themes.json' with { type: 'json' };

export const THEMES = manifest.themes;
export const DEFAULT_THEME_ID = manifest.default;

const IDS = new Set(THEMES.map((t) => t.id));

export function isValidTheme(id) {
  return typeof id === 'string' && IDS.has(id);
}
