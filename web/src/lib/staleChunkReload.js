// Detects a stale-chunk dynamic-import failure -- the browser tab has an
// old Vite bundle loaded (index-*.js) that references a hashed chunk
// (e.g. Login-*.js) which no longer exists because the graywolf server
// was rebuilt/restarted since the tab loaded. The fix is a full page
// reload: index.html is served no-cache (see web/embed.go), so reloading
// fetches the current bundle and its matching chunk hashes.
//
// The match is scoped to the specific browser error phrasing for a
// failed dynamic import() (Chrome/Firefox/Safari all use some variant of
// "dynamically imported module" / "importing a module script failed").
// It deliberately does NOT match generic phrases like "Failed to fetch"
// alone, since that's also how ordinary API fetch() failures surface
// (see lib/api.js ApiError) and would false-trigger a reload loop on any
// unrelated network hiccup.
const STALE_CHUNK_RE = /dynamically imported module|importing a module script failed/i;

export function isStaleChunkError(reason) {
  const msg = (reason && (reason.message || String(reason))) || '';
  return STALE_CHUNK_RE.test(msg);
}

const RELOAD_GUARD_KEY = 'gw_stale_chunk_reload_at';
const RELOAD_GUARD_WINDOW_MS = 10_000;

// shouldAutoReload guards against a reload loop: if the server is
// genuinely down (every request fails, including index.html itself),
// reloading would just hit the same error again immediately. Only
// allow one auto-reload per window.
export function shouldAutoReload(now, storage) {
  const last = Number(storage.getItem(RELOAD_GUARD_KEY) || 0);
  // last === 0 means "no prior reload recorded" -- must not be treated
  // as "just reloaded at epoch 0", which would block the very first
  // legitimate reload.
  if (last > 0 && now - last < RELOAD_GUARD_WINDOW_MS) return false;
  storage.setItem(RELOAD_GUARD_KEY, String(now));
  return true;
}

// installStaleChunkReload wires the detector into the two places a
// dynamic import() failure can surface: an unhandled promise rejection
// (the common case -- svelte-spa-router's lazy route loaders) and a
// window 'error' event (some browsers report module-script load
// failures this way instead).
export function installStaleChunkReload({
  reload = () => window.location.reload(),
  storage = (typeof sessionStorage !== 'undefined' ? sessionStorage : null),
  now = () => Date.now(),
} = {}) {
  function handle(reason) {
    if (!isStaleChunkError(reason)) return;
    if (!storage || !shouldAutoReload(now(), storage)) return;
    reload();
  }
  window.addEventListener('unhandledrejection', (e) => handle(e.reason));
  window.addEventListener('error', (e) => handle(e.error ?? e.message));
}
