// Watches the running server's build identity and flips `updateAvailable`
// when it changes underneath the open tab — i.e. the operator upgraded or
// rebuilt graywolf on the server. App.svelte renders a reload banner off
// this so a stale SPA stops silently running old code against a new server.
//
// Trigger strategy (see the design note on GH): a server upgrade always
// restarts the process, which trips the shared `online` connection store
// false -> true; we re-check /api/version on that reconnect edge. A slow
// fallback poll covers a restart fast enough that no in-flight request
// registered the disconnect. The pure latch/identity logic lives in
// ../server-version-core.js (unit-tested under node --test).

import { online } from './connection.js';
import { serverIdentity, createIdentityWatcher } from '../server-version-core.js';

const DEFAULT_INTERVAL_MS = 60_000;

export const serverVersion = (() => {
  let updated = $state(false);
  const watcher = createIdentityWatcher();
  let started = false;
  let timer = null;
  let unsubOnline = null;

  async function check() {
    try {
      const res = await fetch('/api/version', { credentials: 'same-origin' });
      if (!res.ok) return;
      const data = await res.json();
      if (watcher.observe(serverIdentity(data))) {
        updated = true;
      }
    } catch {
      // A failed fetch during the upgrade restart is expected and means
      // nothing on its own; the reconnect edge will re-trigger a check
      // once the new server answers.
    }
  }

  // start is idempotent. intervalMs is injectable for tests.
  function start(intervalMs = DEFAULT_INTERVAL_MS) {
    if (started || typeof window === 'undefined') return;
    started = true;

    // Establish the boot baseline right away.
    check();

    // Re-check on every disconnect -> reconnect transition. online.subscribe
    // fires synchronously with the current value first; seeding prevOnline
    // to that value means the initial callback is never mistaken for a
    // reconnect.
    let prevOnline = true;
    unsubOnline = online.subscribe((isOnline) => {
      if (isOnline && !prevOnline) check();
      prevOnline = isOnline;
    });

    timer = setInterval(check, intervalMs);
  }

  // stop is here for symmetry/tests; the app never tears the watcher down
  // because it should live for the whole session.
  function stop() {
    if (timer) { clearInterval(timer); timer = null; }
    if (unsubOnline) { unsubOnline(); unsubOnline = null; }
    started = false;
  }

  return {
    get updateAvailable() { return updated; },
    start,
    stop,
  };
})();
