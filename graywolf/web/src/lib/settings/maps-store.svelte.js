// graywolf/web/src/lib/settings/maps-store.svelte.js
//
// Reactive Maps preferences store. Mirrors GET /api/preferences/maps;
// PUTs the source change; calls POST /register through the backend
// proxy so the auth.nw5w.com URL is fixed in one place. After
// registration, the response carries a one-time token which the store
// holds in memory only (so the "Show token" / "Back up token" UI can
// surface it without a second round-trip), but the persisted value
// always comes from the GET — no localStorage mirror for the token.

import { toasts } from '../stores.js';
import { normalizeCallsign } from '../maps/callsign.js';

export const ISSUES_URL = 'https://github.com/chrissnell/graywolf/issues';

function emptyConfig() {
  return {
    source: 'osm',
    callsign: '',
    registered: false,
    registeredAt: null,
    token: null, // populated only immediately after registration
  };
}

export const mapsState = (() => {
  let cfg = $state(emptyConfig());
  let loaded = $state(false);
  let registering = $state(false);

  async function fetchConfig() {
    try {
      const res = await fetch('/api/preferences/maps', { credentials: 'same-origin' });
      if (res.status === 401) return;
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      cfg = {
        source: data.source ?? 'osm',
        callsign: data.callsign ?? '',
        registered: !!data.registered,
        registeredAt: data.registered_at ? new Date(data.registered_at) : null,
        token: cfg.token, // preserve in-memory token across refetches
      };
      loaded = true;
    } catch {
      // Silent — leave defaults; toast happens on user-initiated actions.
    }
  }

  async function setSource(next) {
    const prev = cfg.source;
    cfg = { ...cfg, source: next };
    try {
      const res = await fetch('/api/preferences/maps', {
        method: 'PUT',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ source: next }),
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body?.error || `HTTP ${res.status}`);
      }
      const data = await res.json();
      cfg = { ...cfg, source: data.source };
    } catch (e) {
      cfg = { ...cfg, source: prev };
      toasts.error(`Couldn't change map source: ${e.message}`);
    }
  }

  async function register(rawCallsign) {
    const cs = normalizeCallsign(rawCallsign);
    registering = true;
    try {
      const res = await fetch('/api/preferences/maps/register', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ callsign: cs }),
      });
      const body = await res.json().catch(() => ({}));
      if (!res.ok) {
        return {
          ok: false,
          status: res.status,
          code: body.error ?? 'unknown',
          message: body.message ?? 'Registration failed.',
        };
      }
      cfg = {
        source: body.source,
        callsign: body.callsign,
        registered: true,
        registeredAt: body.registered_at ? new Date(body.registered_at) : new Date(),
        token: body.token, // one-time, in-memory until next fetch
      };
      return { ok: true, token: body.token };
    } catch (e) {
      return {
        ok: false,
        status: 0,
        code: 'network',
        message: `Network error: ${e.message}. Please file an issue at ${ISSUES_URL}`,
      };
    } finally {
      registering = false;
    }
  }

  async function revealToken() {
    const res = await fetch('/api/preferences/maps?include_token=1', { credentials: 'same-origin' });
    if (!res.ok) return null;
    const data = await res.json();
    return data.token ?? null;
  }

  return {
    get source() { return cfg.source; },
    get callsign() { return cfg.callsign; },
    get registered() { return cfg.registered; },
    get registeredAt() { return cfg.registeredAt; },
    get tokenOnce() { return cfg.token; },
    get loaded() { return loaded; },
    get registering() { return registering; },

    fetchConfig,
    setSource,
    register,
    revealToken,
  };
})();
