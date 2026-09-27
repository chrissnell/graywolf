// MUST be the first import: installs Android bearer-token wrappers
// around globalThis.fetch and globalThis.WebSocket before any other
// module evaluates. Reordering or removing this line breaks Android
// auth silently.
import './bootstrap.js';

import { installStaleChunkReload } from './lib/staleChunkReload.js';

// A lazy route (e.g. Login.svelte) can fail to load if the server was
// rebuilt/restarted since this tab's bundle was fetched -- the chunk
// hash it's asking for no longer exists. Auto-reload so the operator
// isn't stuck on a broken screen after a session expiry redirects them
// to #/login. Installed before mounting App so it catches a failure on
// the very first route load.
installStaleChunkReload();

// Auto-import every theme stylesheet in graywolf/web/themes/. Each
// file scopes its declarations under [data-theme="<id>"]; Vite
// inlines them all so the final bundle contains every shipped theme.
// Dropping a new .css file in that directory is enough to register it.
import.meta.glob('../themes/*.css', { eager: true });

// Polyfill crypto.randomUUID for non-secure contexts (plain HTTP on LAN).
if (typeof crypto !== 'undefined' && !crypto.randomUUID) {
  crypto.randomUUID = function () {
    const bytes = crypto.getRandomValues(new Uint8Array(16));
    bytes[6] = (bytes[6] & 0x0f) | 0x40; // version 4
    bytes[8] = (bytes[8] & 0x3f) | 0x80; // variant 1
    const h = Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('');
    return `${h.slice(0,8)}-${h.slice(8,12)}-${h.slice(12,16)}-${h.slice(16,20)}-${h.slice(20)}`;
  };
}

import App from './App.svelte';
import { mount } from 'svelte';

const app = mount(App, { target: document.getElementById('app') });
export default app;
