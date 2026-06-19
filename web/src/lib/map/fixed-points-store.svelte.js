// User-defined fixed map points: operator-placed landmarks shown on the
// live map. Persisted SERVER-SIDE (GET/POST/DELETE /api/fixed-points) so
// they are shared across every device/browser pointed at this server and
// survive a client browser-data wipe (graywolf#347). Uses .svelte.js so
// the $state rune drives the map layer's reactive refresh.
//
// load() is called when the map mounts, so navigating back to the map --
// or opening the map on a second device -- shows the current server set.

import { api } from '../api.js';
import { fixedPointFromApi, fixedPointToApi } from './fixed-points-api.js';

export const fixedPointsStore = (() => {
  let points = $state([]);
  let loaded = $state(false);

  const LEGACY_KEY = 'map-fixed-points';
  const MIGRATED_FLAG = 'map-fixed-points-migrated';

  // One-time upload of points saved by the old localStorage-only build
  // so upgrading operators don't lose them. Guarded by a flag so it runs
  // at most once per browser. Best-effort: on any failure we leave the
  // legacy data in place and do NOT set the flag, so a later load retries.
  //
  // The flag is per-browser, so an operator who had legacy points in two
  // browsers will upload both sets -- a few duplicate server rows they can
  // delete. Accepted trade-off (no per-point dedup); far better than losing
  // points to silent data loss.
  async function migrateLegacyLocalStorage() {
    let legacy;
    try {
      if (localStorage.getItem(MIGRATED_FLAG)) return;
      const raw = localStorage.getItem(LEGACY_KEY);
      if (!raw) {
        localStorage.setItem(MIGRATED_FLAG, '1');
        return;
      }
      legacy = JSON.parse(raw);
    } catch {
      return;
    }
    if (!Array.isArray(legacy) || legacy.length === 0) {
      try {
        localStorage.setItem(MIGRATED_FLAG, '1');
      } catch {
        /* ignore */
      }
      return;
    }
    try {
      for (const p of legacy) {
        if (!p || typeof p.name !== 'string' || !Number.isFinite(p.lat) || !Number.isFinite(p.lon)) continue;
        await api.post('/fixed-points', fixedPointToApi(p));
      }
      localStorage.setItem(MIGRATED_FLAG, '1');
      localStorage.removeItem(LEGACY_KEY);
    } catch {
      // Leave legacy data + unset flag so the next load() retries.
    }
  }

  return {
    get points() {
      return points;
    },
    get loaded() {
      return loaded;
    },

    // Fetch the server set and replace the in-memory list. Safe to call
    // on every map mount; malformed rows are dropped, not fatal. On
    // failure the previous list is kept and the error is rethrown so the
    // caller can surface it.
    async load() {
      await migrateLegacyLocalStorage();
      const rows = (await api.get('/fixed-points')) || [];
      points = rows.map(fixedPointFromApi).filter(Boolean);
      loaded = true;
      return points;
    },

    // Create a point on the server, then append the persisted record
    // (with its server-assigned id) to the list. Returns the point.
    async add({ name, table, symbol, overlay = '', lat, lon }) {
      const created = await api.post('/fixed-points', fixedPointToApi({ name, table, symbol, overlay, lat, lon }));
      const point = fixedPointFromApi(created);
      if (point) points = [...points, point];
      return point;
    },

    // Delete a point on the server, then drop it locally.
    async remove(id) {
      await api.delete(`/fixed-points/${id}`);
      points = points.filter((p) => p.id !== id);
    },
  };
})();
