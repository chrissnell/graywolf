// Reactive theme preference backed by the server (sqlite) with a
// localStorage mirror. The inline boot script in index.html applies
// the stored id before the app mounts; this store keeps the runtime
// in sync after that point and persists changes back to the server.
//
// Shape intentionally mirrors units-store.svelte.js.

import { toasts } from '../stores.js';
import { DEFAULT_THEME_ID, isValidTheme } from '../themes/registry.js';

const LS_KEY = 'theme';

function readStored() {
  try {
    const v = localStorage.getItem(LS_KEY);
    return isValidTheme(v) ? v : DEFAULT_THEME_ID;
  } catch {
    return DEFAULT_THEME_ID;
  }
}

function writeStored(v) {
  try { localStorage.setItem(LS_KEY, v); } catch {}
}

function applyDOM(v) {
  try { document.documentElement.setAttribute('data-theme', v); } catch {}
}

export const themeState = (() => {
  // Read once, then seed the rune. Applying the DOM from `initial`
  // (not from `theme`) sidesteps Svelte 5's state_referenced_locally
  // warning: the rune is there for reactive reads elsewhere in the
  // app, but the one-shot startup apply doesn't need to go through it.
  const initial = readStored();
  let theme = $state(initial);
  // Re-apply so runtime state and the DOM attribute are guaranteed to
  // agree. The boot script already set it, but this is cheap.
  applyDOM(initial);

  async function fetchConfig() {
    try {
      const res = await fetch('/api/preferences/theme', { credentials: 'same-origin' });
      if (res.status === 401) return;
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      // Server returns whatever's in the DB verbatim (regex-validated).
      // If it's not a theme we know how to render (stale row after a
      // theme was removed), fall back to the default.
      const next = isValidTheme(data?.id) ? data.id : DEFAULT_THEME_ID;
      theme = next;
      writeStored(next);
      applyDOM(next);
    } catch {
      // Offline or not yet authenticated — stick with the mirror.
    }
  }

  async function setTheme(next) {
    if (!isValidTheme(next)) return;
    const prev = theme;
    theme = next;
    writeStored(next);
    applyDOM(next);
    try {
      const res = await fetch('/api/preferences/theme', {
        method: 'PUT',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id: next }),
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      const stored = isValidTheme(data?.id) ? data.id : prev;
      theme = stored;
      writeStored(stored);
      applyDOM(stored);
    } catch {
      theme = prev;
      writeStored(prev);
      applyDOM(prev);
      toasts.error("Couldn't save theme preference — try again.");
    }
  }

  return {
    get theme() { return theme; },
    fetchConfig,
    setTheme,
  };
})();
