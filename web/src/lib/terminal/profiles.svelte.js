// Saved profile + recent connection store. Backs the pre-connect form's
// "Recents" list and the (future) saved-profiles picker. The server
// returns pinned rows first, then recents ordered by last_used desc;
// this store preserves that order.

const state = $state({
  loaded: false,
  loading: false,
  profiles: [],
  error: null,
});

const ENDPOINT = '/api/ax25/profiles';

export const profilesStore = {
  get loaded() { return state.loaded; },
  get loading() { return state.loading; },
  get profiles() { return state.profiles; },
  get pinned() { return state.profiles.filter((p) => p.pinned); },
  get recents() { return state.profiles.filter((p) => !p.pinned); },
  get error() { return state.error; },

  async load() {
    if (state.loading) return;
    state.loading = true;
    state.error = null;
    try {
      const r = await fetch(ENDPOINT, { credentials: 'same-origin' });
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      const rows = await r.json();
      state.profiles = Array.isArray(rows) ? rows : [];
      state.loaded = true;
    } catch (err) {
      state.error = String(err);
    } finally {
      state.loading = false;
    }
  },

  // setPinned flips the pinned flag on a single profile via the
  // dedicated /pin endpoint, then re-loads so order is consistent.
  async setPinned(id, pinned) {
    const r = await fetch(`${ENDPOINT}/${id}/pin`, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pinned }),
    });
    if (!r.ok) {
      const txt = await r.text().catch(() => '');
      throw new Error(`HTTP ${r.status}: ${txt}`);
    }
    await this.load();
  },

  async remove(id) {
    const r = await fetch(`${ENDPOINT}/${id}`, {
      method: 'DELETE',
      credentials: 'same-origin',
    });
    if (!r.ok && r.status !== 204) {
      const txt = await r.text().catch(() => '');
      throw new Error(`HTTP ${r.status}: ${txt}`);
    }
    await this.load();
  },
};

// profileLabel renders a short, sortable label for a profile.
export function profileLabel(p) {
  if (!p) return '';
  const name = (p.name ?? '').trim();
  const peer = addr(p.dest_call, p.dest_ssid);
  if (name) return `${name} (${peer})`;
  return peer;
}

function addr(call, ssid) {
  const c = (call ?? '').toUpperCase();
  if (!c) return '';
  if (!ssid || ssid === 0) return c;
  return `${c}-${ssid}`;
}
