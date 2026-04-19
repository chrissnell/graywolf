// Shared callsign color helper.
//
// Deliberately NOT `hash % 360` hue: adjacent-hash callsigns would
// produce visually adjacent hues that are hard to scan apart in a
// busy net, and color alone must never be the sole identity signal
// (WCAG 1.4.1). This helper maps to a curated 12-stop palette picked
// for dark-mode distinguishability and deuteranopia/protanopia
// separability.
//
// `callsignColors(call)` -> `{ bg, fg, stripe }` hex strings suitable
//     for inline `background`, `color`, and `border` styles.
// `callsignMonogram(call)` -> up-to-3-letter uppercase monogram used
//     as a non-color redundancy signal (colorblind operators read
//     the letters directly).
//
// Consumers: MessageBubble (sender label + 2 px left stripe +
// monogram on cluster heads), ParticipantChips (chip background
// and avatar fallback letters). Keep the palette and both helpers
// in this single source of truth.

// 12 stops — tested against #0d1117 background, pairs at
// non-adjacent indices for max separation.
const STOPS = [
  { bg: '#b94a4a33', fg: '#ff8a8a', stripe: '#ee5555' }, // red
  { bg: '#c57a1a33', fg: '#ffb066', stripe: '#ee9900' }, // orange
  { bg: '#a38d1f33', fg: '#e6cc66', stripe: '#c9b040' }, // yellow
  { bg: '#3a8f3a33', fg: '#88d988', stripe: '#44aa44' }, // green
  { bg: '#2a8f8f33', fg: '#77d9d9', stripe: '#22aaaa' }, // teal
  { bg: '#3a6fbf33', fg: '#88bfff', stripe: '#4499aa' }, // sky
  { bg: '#5a5fcf33', fg: '#9999ee', stripe: '#6666cc' }, // blue
  { bg: '#8a5fcf33', fg: '#bb99ee', stripe: '#9966cc' }, // indigo
  { bg: '#bf5fbf33', fg: '#ee99ee', stripe: '#cc66cc' }, // magenta
  { bg: '#cf4f7a33', fg: '#ff99bb', stripe: '#ee6699' }, // pink
  { bg: '#8a8a8a33', fg: '#cccccc', stripe: '#999999' }, // neutral
  { bg: '#5a8f6a33', fg: '#99d9b3', stripe: '#55bb88' }, // sage
];

/**
 * Map a callsign string (case-insensitive) to one of 12 curated color
 * stops. Stable across sessions: same callsign → same stop.
 * @param {string} call
 * @returns {{ bg: string, fg: string, stripe: string }}
 */
export function callsignColors(call) {
  const s = String(call || '').toUpperCase();
  let h = 0;
  for (let i = 0; i < s.length; i++) {
    h = (h * 31 + s.charCodeAt(i)) | 0;
  }
  const idx = ((h % STOPS.length) + STOPS.length) % STOPS.length;
  return STOPS[idx];
}

/**
 * Up-to-3 uppercase letters distilled from the callsign. Strips
 * digits and SSID suffix: "W1ABC-9" -> "WAB", "NET" -> "NET".
 * Falls back to the first 3 characters of the raw string when no
 * letters are available (unlikely in practice).
 * @param {string} call
 * @returns {string}
 */
export function callsignMonogram(call) {
  const s = String(call || '');
  const letters = s.replace(/[^A-Za-z]/g, '').toUpperCase();
  if (letters.length > 0) return letters.slice(0, 3);
  return s.slice(0, 3).toUpperCase();
}
