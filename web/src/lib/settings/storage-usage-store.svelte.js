// Reactive, read-only storage-usage report backed by GET
// /api/storage/usage. The endpoint returns the same shape on every
// platform, so this store (and the card that renders it) is shared by
// desktop and Android with no platform branching.

// Per-location bar/legend color, keyed off the stable `key` the server
// sends (not the human label). Falls back to a neutral tone for any
// key we don't recognize so a future server-side addition still renders.
const KEY_COLORS = {
  maps: 'var(--color-primary)',
  history: 'var(--color-success)',
  config: 'var(--color-info)',
};

export function colorForKey(key) {
  return KEY_COLORS[key] ?? 'var(--color-text-muted)';
}

export const storageUsageState = (() => {
  let locations = $state([]);
  let totalBytes = $state(0);
  let loaded = $state(false);
  let error = $state(null);

  async function refresh() {
    try {
      const res = await fetch('/api/storage/usage', { credentials: 'same-origin' });
      if (res.status === 401) return;
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      locations = Array.isArray(data?.locations) ? data.locations : [];
      totalBytes = Number(data?.total_bytes) || 0;
      loaded = true;
      error = null;
    } catch (e) {
      // Leave any previously loaded figures in place; surface the error
      // so the UI can show a quiet retry hint rather than blank zeros.
      error = e?.message || 'failed to load storage usage';
    }
  }

  return {
    get locations() { return locations; },
    get totalBytes() { return totalBytes; },
    get loaded() { return loaded; },
    get error() { return error; },
    refresh,
  };
})();
