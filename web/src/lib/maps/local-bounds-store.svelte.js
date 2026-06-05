// Reactive store for the render-path bounds of every completed offline
// download. Fetched once per session from /api/maps/local-bounds.
//
// Unlike catalog-store.svelte.js, this store has zero remote
// dependencies on the backend side -- the endpoint reads from the
// local sqlite maps_downloads table. That's the whole point: the
// federated tile protocol uses this store so that a host with no
// network can still render the regions it has on disk.
//
// boundsBySlug returns Map<namespacedSlug, [west, south, east, north]>
// which matches the shape gw-federated-protocol expects.

export const localBoundsStore = (() => {
  let raw = $state(null); // { [slug]: [w, s, e, n] } | null
  let inflight = null;

  async function load() {
    if (raw) return raw;
    if (inflight) return inflight;
    inflight = (async () => {
      try {
        const res = await fetch('/api/maps/local-bounds', { credentials: 'same-origin' });
        if (!res.ok) {
          raw = {};
          return raw;
        }
        const json = await res.json();
        raw = json && typeof json === 'object' ? json : {};
        return raw;
      } catch {
        raw = {};
        return raw;
      } finally {
        inflight = null;
      }
    })();
    return inflight;
  }

  // refresh forces a fresh fetch (e.g. after a download completes so
  // the new region's bounds show up without a page reload).
  async function refresh() {
    raw = null;
    return load();
  }

  return {
    load,
    refresh,
    get boundsBySlug() {
      const out = new Map();
      if (!raw) return out;
      for (const [slug, bbox] of Object.entries(raw)) {
        if (Array.isArray(bbox) && bbox.length === 4) out.set(slug, bbox);
      }
      return out;
    },
  };
})();
