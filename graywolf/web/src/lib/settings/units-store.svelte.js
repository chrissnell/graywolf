// Reactive unit system preference backed by the server (sqlite) with
// a localStorage mirror so the initial render before fetchConfig()
// resolves still respects the last known value — avoids a flicker
// from imperial to metric on page load for metric operators.

import { toasts } from '../stores.js';

const LS_KEY = 'units-system';

function normalize(v) {
  return v === 'metric' ? 'metric' : 'imperial';
}

function readStored() {
  try { return normalize(localStorage.getItem(LS_KEY)); }
  catch { return 'imperial'; }
}

function writeStored(v) {
  try { localStorage.setItem(LS_KEY, v); } catch {}
}

export const unitsState = (() => {
  let system = $state(readStored());

  async function fetchConfig() {
    try {
      const res = await fetch('/api/preferences/units', { credentials: 'same-origin' });
      if (res.status === 401) return;
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      const next = normalize(data?.system);
      system = next;
      writeStored(next);
    } catch {
      // Offline or not yet authenticated — stick with the localStorage
      // mirror we initialized from.
    }
  }

  async function setSystem(next) {
    const v = normalize(next);
    const prev = system;
    system = v;
    writeStored(v);
    try {
      const res = await fetch('/api/preferences/units', {
        method: 'PUT',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ system: v }),
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      const stored = normalize(data?.system);
      system = stored;
      writeStored(stored);
    } catch {
      system = prev;
      writeStored(prev);
      toasts.error("Couldn't save units preference — try again.");
    }
  }

  return {
    get system() { return system; },
    set system(v) { setSystem(v); },

    get isMetric() { return system === 'metric'; },

    fetchConfig,
    setSystem,
  };
})();
