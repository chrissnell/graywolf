// Reactive store for the maps catalog. Fetched once per session from
// /api/maps/catalog, kept in module scope for the lifetime of the SPA.
// boundsBySlug exposes a Map<namespacedSlug, [west, south, east, north]>
// that the federated tile protocol uses for offline-coverage lookup.

import { toasts } from '../stores.js';

export const catalogStore = (() => {
  let catalog = $state(null); // {schemaVersion, generatedAt, countries, provinces, states} or null
  let loading = $state(false);
  let error = $state(null);
  let inflight = null;

  async function load() {
    if (catalog) return catalog;
    if (inflight) return inflight;
    loading = true;
    error = null;
    inflight = (async () => {
      try {
        const res = await fetch('/api/maps/catalog', { credentials: 'same-origin' });
        if (!res.ok) {
          const msg = `catalog fetch failed (${res.status})`;
          error = msg;
          toasts.error(msg);
          return null;
        }
        const json = await res.json();
        if (json?.schemaVersion !== 1) {
          error = 'unsupported catalog schemaVersion';
          toasts.error(error);
          return null;
        }
        catalog = json;
        return json;
      } catch (e) {
        error = e.message ?? 'catalog network error';
        toasts.error(error);
        return null;
      } finally {
        loading = false;
        inflight = null;
      }
    })();
    return inflight;
  }

  function namespacedStateSlug(slug)    { return `state/${slug}`; }
  function namespacedCountrySlug(iso2)  { return `country/${iso2}`; }
  function namespacedProvinceSlug(iso2, slug) { return `province/${iso2}/${slug}`; }

  return {
    get catalog() { return catalog; },
    get loading() { return loading; },
    get error() { return error; },
    load,

    // Returns Map<namespacedSlug, [west, south, east, north]>.
    get boundsBySlug() {
      const out = new Map();
      if (!catalog) return out;
      for (const c of catalog.countries) {
        if (c.bbox) out.set(namespacedCountrySlug(c.iso2), c.bbox);
      }
      for (const p of catalog.provinces) {
        if (p.bbox) out.set(namespacedProvinceSlug(p.iso2, p.slug), p.bbox);
      }
      for (const s of catalog.states) {
        if (s.bbox) out.set(namespacedStateSlug(s.slug), s.bbox);
      }
      return out;
    },

    namespacedStateSlug,
    namespacedCountrySlug,
    namespacedProvinceSlug,
  };
})();
