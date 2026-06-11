// User-defined fixed map points: locally significant landmarks or
// event-specific locations the operator drops via the map's right-click
// menu. Each point carries a name, an APRS symbol (table/symbol/overlay,
// same vocabulary as station markers), and a lat/lon.
//
// Persistence: localStorage, so points survive reloads and outlive a
// single session. They are removed only when the operator clicks a point
// and deletes it (or clears localStorage). Uses .svelte.js so the $state
// rune drives the map layer's reactive refresh.

const STORAGE_KEY = 'map-fixed-points';

function load() {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const arr = JSON.parse(raw);
    if (!Array.isArray(arr)) return [];
    // Keep only well-formed entries — guards against a hand-edited or
    // schema-drifted localStorage value crashing the layer.
    return arr.filter(
      (p) =>
        p &&
        typeof p.id === 'string' &&
        typeof p.name === 'string' &&
        Number.isFinite(p.lat) &&
        Number.isFinite(p.lon),
    );
  } catch {
    return [];
  }
}

// Monotonic counter so the non-crypto fallback can't collide for two
// points added in the same millisecond. Graywolf dashboards are commonly
// served over plain HTTP on a LAN, where crypto.randomUUID is unavailable.
let idSeq = 0;
function newId() {
  if (typeof crypto !== 'undefined' && crypto.randomUUID) {
    return `fp-${crypto.randomUUID()}`;
  }
  idSeq += 1;
  return `fp-${Date.now()}-${idSeq}`;
}

export const fixedPointsStore = (() => {
  let points = $state(load());

  function persist() {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(points));
    } catch {
      // Quota or private-mode failures are non-fatal — the points still
      // live in memory for this session.
    }
  }

  return {
    get points() {
      return points;
    },

    add({ name, table, symbol, overlay = '', lat, lon }) {
      const point = {
        id: newId(),
        name: (name || '').trim() || 'Point',
        table: table || '/',
        symbol: symbol || '/',
        overlay: overlay || '',
        lat,
        lon,
      };
      points = [...points, point];
      persist();
      return point;
    },

    remove(id) {
      const next = points.filter((p) => p.id !== id);
      if (next.length === points.length) return;
      points = next;
      persist();
    },

    clear() {
      points = [];
      persist();
    },
  };
})();
