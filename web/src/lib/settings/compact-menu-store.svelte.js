// Device-local "force compact menu" preference. localStorage-only like
// ui-scale: this is a per-device display choice that shouldn't propagate
// to other screens.
//
// When active, applies the class `force-compact-menu` to <html> so CSS
// in Sidebar.svelte and App.svelte can respond without needing to import
// this store.

const LS_KEY = 'force-compact-menu';

function readStored() {
  try { return localStorage.getItem(LS_KEY) === 'true'; }
  catch { return false; }
}

function writeStored(v) {
  try { localStorage.setItem(LS_KEY, String(v)); } catch {}
}

function applyDOM(v) {
  try { document.documentElement.classList.toggle('force-compact-menu', v); } catch {}
}

export const compactMenuState = (() => {
  const initial = readStored();
  let forceCompact = $state(initial);
  // Apply before first paint so the correct layout is present immediately.
  applyDOM(initial);

  function setForceCompact(next) {
    const v = Boolean(next);
    forceCompact = v;
    writeStored(v);
    applyDOM(v);
  }

  return {
    get forceCompact() { return forceCompact; },
    setForceCompact,
  };
})();
