// Reactive per-browser preference for which app the map popup's Navigate
// action opens. Persisted to localStorage only (NOT server-synced), since
// the OS-based default must be able to differ per device -- mirrors
// mapState.radarRegion in map-store.svelte.js.

import { Platform } from '../platform.js';
import { NAV_PROVIDERS, normalizeProvider, detectDefaultProvider } from '../map/nav-provider-core.js';

export { NAV_PROVIDERS };

const NAV_PROVIDER_KEY = 'gw_nav_provider';

function readStored() {
  try { return normalizeProvider(localStorage.getItem(NAV_PROVIDER_KEY)); }
  catch { return null; }
}

function writeStored(v) {
  try { localStorage.setItem(NAV_PROVIDER_KEY, v); } catch {}
}

function osDefault() {
  return detectDefaultProvider({
    isAndroid: Platform.isAndroid,
    userAgent: typeof navigator !== 'undefined' ? navigator.userAgent : '',
    platform: typeof navigator !== 'undefined' ? navigator.platform : '',
  });
}

export const navProviderState = (() => {
  let provider = $state(readStored() ?? osDefault());

  return {
    get provider() { return provider; },
    set provider(v) {
      const next = normalizeProvider(v);
      if (!next) return;
      provider = next;
      writeStored(next);
    },
  };
})();
