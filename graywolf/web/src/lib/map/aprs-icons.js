// APRS symbol → Leaflet DivIcon.
//
// Sprite sheets are 16x6 grids of 24px cells, 0-indexed from '!' (0x21).
// Primary table '/': sheet 0, alternate table '\': sheet 1, overlays: sheet 2.
// Overlay characters (0-9, A-Z, a-z) replace the table byte — the base icon
// comes from sheet 1 (alternate) and the overlay glyph composites from sheet 2.
// Lowercase overlays are normalized to uppercase for glyph lookup (hessu sheets
// only have 0-9 and A-Z glyphs).

import L from 'leaflet';
import {
  CELL_PX, COLS, FIRST_SYMBOL_CODE,
  PRIMARY_TABLE, ALTERNATE_TABLE,
  SPRITE_URLS, SPRITE_URLS_2X,
} from '../aprsSymbols.js';

const RETINA = window.devicePixelRatio > 1.5;
const SHEET_W = COLS * CELL_PX;   // 384
const SHEET_H = 6 * CELL_PX;     // 144

// Pick 1x or 2x sprite URLs
function sheetUrl(key) {
  return RETINA ? SPRITE_URLS_2X[key] : SPRITE_URLS[key];
}

const primarySheet   = sheetUrl(PRIMARY_TABLE);
const alternateSheet = sheetUrl(ALTERNATE_TABLE);
const overlaySheet   = sheetUrl('overlay');

// Overlay detection: 0-9, A-Z, a-z
const OVERLAY_RE = /^[0-9A-Za-z]$/;

function isOverlay(table) {
  return OVERLAY_RE.test(table);
}

// Sprite cell for a symbol character
function cellOf(ch) {
  const idx = ch.charCodeAt(0) - FIRST_SYMBOL_CODE;
  return [idx % COLS, Math.floor(idx / COLS)];
}

// Background CSS for a sprite cell
function spriteBg(url, col, row) {
  return `background-image:url(${url});background-position:-${col * CELL_PX}px -${row * CELL_PX}px;background-size:${SHEET_W}px ${SHEET_H}px`;
}

// Icon cache keyed by table+code
const cache = new Map();

// Fallback dot icon for missing/invalid symbols
let fallbackIcon = null;
function getFallback() {
  if (!fallbackIcon) {
    fallbackIcon = L.divIcon({
      className: 'aprs-marker',
      html: '<div class="aprs-icon-fallback"></div>',
      iconSize: [44, 44],
      iconAnchor: [22, 22],
      popupAnchor: [0, -22],
    });
  }
  return fallbackIcon;
}

// Create a Leaflet DivIcon for the given APRS symbol.
// table: single char ('/', '\', or overlay char 0-9, A-Z, a-z)
// code: single char ('!' through '~')
export function aprsIcon(table, code) {
  if (!table || !code) return getFallback();

  const codePoint = code.charCodeAt(0);
  if (codePoint < FIRST_SYMBOL_CODE || codePoint > 0x7e) return getFallback();

  const key = table + code;
  if (cache.has(key)) return cache.get(key);

  const [col, row] = cellOf(code);
  let html;

  if (table === PRIMARY_TABLE) {
    html = `<div class="aprs-icon" style="${spriteBg(primarySheet, col, row)}"></div>`;
  } else if (table === ALTERNATE_TABLE) {
    html = `<div class="aprs-icon" style="${spriteBg(alternateSheet, col, row)}"></div>`;
  } else if (isOverlay(table)) {
    // Overlay: base icon from alternate sheet, overlay glyph composited on top
    const overlayChar = table.toUpperCase();
    const [oCol, oRow] = cellOf(overlayChar);
    html =
      `<div class="aprs-icon" style="${spriteBg(alternateSheet, col, row)}">` +
      `<div class="aprs-overlay" style="${spriteBg(overlaySheet, oCol, oRow)}"></div>` +
      `</div>`;
  } else {
    // Unrecognized table char — fallback
    const icon = getFallback();
    cache.set(key, icon);
    return icon;
  }

  const icon = L.divIcon({
    className: 'aprs-marker',
    html,
    iconSize: [44, 44],
    iconAnchor: [22, 22],
    popupAnchor: [0, -22],
  });

  cache.set(key, icon);
  return icon;
}
