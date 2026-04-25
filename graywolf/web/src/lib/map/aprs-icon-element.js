// APRS symbol → DOM element factory for maplibregl.Marker.
//
// Mirrors the compositing logic in aprs-icons.js (Leaflet) but produces a raw
// HTMLElement sized to displayPx. Sprite sheets are 16x6 grids of 24px source
// cells; we scale background-size to displayPx so cells fall on integer pixel
// offsets at any rendered size.
//
// Table semantics:
//   '/'  → primary sheet (sheet 0)
//   '\\' → alternate sheet (sheet 1), no overlay
//   else → overlay character; base from alternate sheet, glyph from sheet 2
//          (uppercased — hessu sheets only carry 0-9 and A-Z glyphs).

import {
  COLS, ROWS,
  SPRITE_URLS, SPRITE_URLS_2X,
  cellOf,
} from '../aprsSymbols.js';

// Pick sprite resolution once at module load.
const RETINA = typeof window !== 'undefined' && window.devicePixelRatio > 1.5;
const SHEETS = RETINA ? SPRITE_URLS_2X : SPRITE_URLS;

function applySprite(el, url, col, row, displayPx) {
  el.style.backgroundImage = `url(${url})`;
  el.style.backgroundRepeat = 'no-repeat';
  el.style.backgroundSize = `${COLS * displayPx}px ${ROWS * displayPx}px`;
  el.style.backgroundPosition = `-${col * displayPx}px -${row * displayPx}px`;
  // 1x sprites scaled past native 24px need pixelated rendering; 2x looks fine smoothed.
  if (!RETINA) el.style.imageRendering = 'pixelated';
}

export function createAprsIconElement({ table, symbol, overlay = null, displayPx = 28 }) {
  const el = document.createElement('div');
  el.style.width = `${displayPx}px`;
  el.style.height = `${displayPx}px`;
  el.style.position = 'relative';

  const [col, row] = cellOf(symbol);

  // Determine base sheet and whether an overlay glyph composites on top.
  let baseSheet;
  let overlayChar = null;
  if (table === '/') {
    baseSheet = SHEETS['/'];
  } else if (table === '\\') {
    baseSheet = SHEETS['\\'];
    if (overlay && overlay !== '/' && overlay !== '\\') {
      overlayChar = overlay.toUpperCase();
    }
  } else {
    // Anything else in `table` is itself the overlay character.
    baseSheet = SHEETS['\\'];
    overlayChar = table.toUpperCase();
  }

  applySprite(el, baseSheet, col, row, displayPx);

  if (overlayChar) {
    const [oCol, oRow] = cellOf(overlayChar);
    const ov = document.createElement('div');
    ov.style.position = 'absolute';
    ov.style.left = '0';
    ov.style.top = '0';
    ov.style.width = `${displayPx}px`;
    ov.style.height = `${displayPx}px`;
    applySprite(ov, SHEETS.overlay, oCol, oRow, displayPx);
    el.appendChild(ov);
  }

  return el;
}
