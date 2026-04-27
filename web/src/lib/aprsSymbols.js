// APRS symbol metadata loader.
//
// The vendored sprite sheets at /aprs-symbols/aprs-symbols-24-{0,1,2}.png are
// 16x6 grids of 24px cells. The sprite is 0-indexed starting at `!` (0x21):
// cell (0,0) is `/!` (Police), cell (15,0) is `/0`, and the last printable
// `~` (0x7E) lives at cell (13,5). Cells (14,5) and (15,5) are unused.
//
// `symbols.csv` is tab-separated despite the .csv extension. Columns:
//   CODE         e.g. "/!" — table char + symbol char
//   DSTCALL      tocall encoding (unused here)
//   DESCRIPTION  human label, may be blank
//   ROTATEDEG    optional rotation (unused here)

export const CELL_PX = 24;
export const COLS = 16;
export const ROWS = 6;
export const FIRST_SYMBOL_CODE = 0x21; // '!'
export const LAST_SYMBOL_CODE = 0x7e;  // '~'

export const PRIMARY_TABLE = '/';
export const ALTERNATE_TABLE = '\\';

export const SPRITE_URLS = {
  [PRIMARY_TABLE]: '/aprs-symbols/aprs-symbols-24-0.png',
  [ALTERNATE_TABLE]: '/aprs-symbols/aprs-symbols-24-1.png',
  overlay: '/aprs-symbols/aprs-symbols-24-2.png',
};

export const SPRITE_URLS_2X = {
  [PRIMARY_TABLE]: '/aprs-symbols/aprs-symbols-24-0@2x.png',
  [ALTERNATE_TABLE]: '/aprs-symbols/aprs-symbols-24-1@2x.png',
  overlay: '/aprs-symbols/aprs-symbols-24-2@2x.png',
};

// cellOf returns [col, row] for the given symbol character. The sprite is
// 0-indexed from '!' (0x21), so '!' → (0,0), '0' → (15,0), '1' → (0,1), etc.
export function cellOf(symbolChar) {
  const idx = symbolChar.charCodeAt(0) - FIRST_SYMBOL_CODE;
  return [idx % COLS, Math.floor(idx / COLS)];
}

// backgroundPosition returns the CSS background-position string for a sprite
// rendered at `displayPx` cell size (not the source 24px).
export function backgroundPosition(symbolChar, displayPx = CELL_PX) {
  const [col, row] = cellOf(symbolChar);
  return `${-col * displayPx}px ${-row * displayPx}px`;
}

// Parses tab-separated symbols.csv into:
//   { '/': { '!': 'Police station', ... },
//     '\\': { '!': 'Emergency', ... } }
function parseSymbolsCsv(text) {
  const out = { [PRIMARY_TABLE]: {}, [ALTERNATE_TABLE]: {} };
  const lines = text.split('\n');
  for (let i = 1; i < lines.length; i++) {
    const line = lines[i];
    if (!line.trim()) continue;
    const fields = parseLine(line);
    if (fields.length < 3) continue;
    const code = fields[0];
    const desc = fields[2] || '';
    if (code.length !== 2) continue;
    const tableChar = code[0];
    const sym = code[1];
    if (out[tableChar] !== undefined) {
      out[tableChar][sym] = desc;
    }
  }
  return out;
}

// Minimal CSV/TSV line parser handling double-quoted fields with embedded "".
function parseLine(line) {
  const out = [];
  let i = 0;
  while (i < line.length) {
    let field;
    if (line[i] === '"') {
      i++;
      let buf = '';
      while (i < line.length) {
        if (line[i] === '"') {
          if (line[i + 1] === '"') { buf += '"'; i += 2; continue; }
          i++;
          break;
        }
        buf += line[i++];
      }
      field = buf;
    } else {
      let end = line.indexOf('\t', i);
      if (end === -1) end = line.length;
      field = line.slice(i, end);
      i = end;
    }
    out.push(field);
    if (line[i] === '\t') i++;
  }
  return out;
}

let cached = null;
let pending = null;

// loadSymbols fetches and parses symbols.csv once per page load.
export async function loadSymbols() {
  if (cached) return cached;
  if (pending) return pending;
  pending = fetch('/aprs-symbols/symbols.csv')
    .then((r) => r.text())
    .then((text) => {
      cached = parseSymbolsCsv(text);
      return cached;
    });
  return pending;
}

// describe returns a human label for a symbol, falling back to "table+code".
export function describe(symbols, table, code) {
  if (!symbols || !code) return '';
  return symbols[table]?.[code] || `${table}${code}`;
}

// Overlay characters that may legally replace the alternate table byte.
export const OVERLAY_CHARS = '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ';

export function isValidOverlay(c) {
  return c.length === 1 && OVERLAY_CHARS.includes(c);
}
