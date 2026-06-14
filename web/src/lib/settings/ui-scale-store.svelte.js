// Device-local UI scale preference. Unlike theme/units, this is
// intentionally NOT synced to the server: an operator who needs a larger
// interface on their phone doesn't want their desktop blown up to match,
// so the value lives only in this device's localStorage.
//
// It's applied as a CSS `zoom` on the document root, which reflows the
// whole layout and scales every interface element uniformly — text,
// buttons, switches, spacing — the way the browser's own page-zoom would.
// That matters most on Android, where the WebView disables pinch-zoom and
// the page-zoom shortcut, leaving the operator no built-in way to enlarge
// the UI (graywolf #275).

const LS_KEY = 'ui-scale';

const DEFAULT = 1;

// Discrete steps offered in Preferences, in ascending order. These are the
// ONLY values the store will ever hold: reads and writes snap to the nearest
// step (see snap()), so the Select always has an exact match to display and
// the applied zoom is always a known-good size. MIN/MAX below are derived
// from this list and must stay in sync with the inline boot script in
// index.html, which clamps to the same [0.9, 2] range.
export const UI_SCALE_OPTIONS = [
  { value: '0.9', label: 'Small (90%)' },
  { value: '1', label: 'Default (100%)' },
  { value: '1.1', label: 'Large (110%)' },
  { value: '1.25', label: 'Larger (125%)' },
  { value: '1.5', label: 'Huge (150%)' },
  { value: '1.75', label: 'Extra (175%)' },
  { value: '2', label: 'Maximum (200%)' },
];

const STEPS = UI_SCALE_OPTIONS.map((o) => parseFloat(o.value));
const MIN = STEPS[0];
const MAX = STEPS[STEPS.length - 1];

// Clamp into range, then snap to the nearest offered step. A hand-edited or
// legacy localStorage value (e.g. 1.2) thus resolves to a real option (1.25)
// instead of leaving the Select rendering its empty placeholder.
function snap(n) {
  if (!Number.isFinite(n)) return DEFAULT;
  const c = Math.min(MAX, Math.max(MIN, n));
  return STEPS.reduce((best, s) =>
    Math.abs(s - c) < Math.abs(best - c) ? s : best, STEPS[0]);
}

function readStored() {
  try {
    const n = parseFloat(localStorage.getItem(LS_KEY));
    return Number.isFinite(n) ? snap(n) : DEFAULT;
  } catch {
    return DEFAULT;
  }
}

function writeStored(n) {
  try { localStorage.setItem(LS_KEY, String(n)); } catch {}
}

function applyDOM(n) {
  // `zoom` on the root element behaves like page-zoom in Chromium (the
  // Android WebView engine): `vh`, fixed positioning, and the map all
  // recompute against the zoomed viewport, so nothing clips the way a
  // `transform: scale()` would.
  try { document.documentElement.style.zoom = String(n); } catch {}
}

export const uiScaleState = (() => {
  const initial = readStored();
  let scale = $state(initial);
  // The inline boot script already applied this before first paint; re-apply
  // so the rune and the DOM are guaranteed to agree. Cheap and idempotent.
  applyDOM(initial);

  function setScale(next) {
    const n = snap(parseFloat(next));
    scale = n;
    writeStored(n);
    applyDOM(n);
  }

  return {
    get scale() { return scale; },
    setScale,
  };
})();
