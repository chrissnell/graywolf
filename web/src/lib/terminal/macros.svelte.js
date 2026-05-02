// Macro store. Fetches the singleton AX25TerminalConfig and exposes a
// reactive macros array. Persistence flows through PUT
// /api/ax25/terminal-config; the store re-loads after every successful
// write so the UI always reflects what the server stored.
//
// Macros are an array of {label, payload} where payload is base64.
// Callers (MacroToolbar) decode the base64 to Uint8Array before
// calling session.sendData.

import { b64ToBytes } from './envelope.js';

const state = $state({
  loaded: false,
  loading: false,
  macros: [],
  error: null,
});

const ENDPOINT = '/api/ax25/terminal-config';

export const macrosStore = {
  get loaded() { return state.loaded; },
  get loading() { return state.loading; },
  get error() { return state.error; },
  get macros() { return state.macros; },

  async load() {
    if (state.loading) return;
    state.loading = true;
    state.error = null;
    try {
      const r = await fetch(ENDPOINT, { credentials: 'same-origin' });
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      const cfg = await r.json();
      state.macros = Array.isArray(cfg?.macros) ? cfg.macros : [];
      state.loaded = true;
    } catch (err) {
      state.error = String(err);
    } finally {
      state.loading = false;
    }
  },

  // saveMacros replaces the persisted macro list. Re-loads on success.
  async saveMacros(macros) {
    state.error = null;
    const r = await fetch(ENDPOINT, {
      method: 'PUT',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ macros }),
    });
    if (!r.ok) {
      const txt = await r.text().catch(() => '');
      throw new Error(`HTTP ${r.status}: ${txt}`);
    }
    const cfg = await r.json();
    state.macros = Array.isArray(cfg?.macros) ? cfg.macros : [];
    state.loaded = true;
    return state.macros;
  },
};

// payloadBytes decodes a stored macro payload (base64) into a Uint8Array.
// Empty / missing payloads return a zero-length buffer rather than null
// so callers can hand the result straight to session.sendData.
export function payloadBytes(macro) {
  return b64ToBytes(macro?.payload ?? '');
}
