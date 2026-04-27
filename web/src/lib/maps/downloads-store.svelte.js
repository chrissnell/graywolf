// Reactive store for the offline downloads UI. Polls the backend
// every 1.5s while any download is active, otherwise stays idle.
// The state-picker component calls start()/remove(); the source-
// picker component reads `completed` to decide if the offline radio
// should be activated.

import { toasts } from '../stores.js';

export const downloadsState = (() => {
  let items = $state(new Map()); // slug -> {state, bytes_total, bytes_downloaded, downloaded_at, error_message}
  let pollHandle = null;

  async function refresh() {
    try {
      const res = await fetch('/api/maps/downloads', { credentials: 'same-origin' });
      if (res.status === 401) return; // not logged in; UI handles redirect elsewhere
      if (!res.ok) return;
      const arr = await res.json();
      const next = new Map();
      for (const r of arr) next.set(r.slug, r);
      items = next;
    } catch {
      // Silent -- toasts on user-initiated actions, not background polls.
    }
  }

  async function start(slug) {
    try {
      const res = await fetch(`/api/maps/downloads/${slug}`, {
        method: 'POST',
        credentials: 'same-origin',
      });
      if (res.status === 409) {
        toasts.error(`${slug}: a download is already in progress`);
        return;
      }
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        toasts.error(`${slug}: ${body.message ?? `download failed (${res.status})`}`);
        return;
      }
      await refresh();
      ensurePolling();
    } catch (e) {
      toasts.error(`${slug}: network error -- ${e.message}`);
    }
  }

  async function remove(slug) {
    try {
      const res = await fetch(`/api/maps/downloads/${slug}`, {
        method: 'DELETE',
        credentials: 'same-origin',
      });
      if (!res.ok) {
        toasts.error(`${slug}: delete failed (${res.status})`);
        return;
      }
      await refresh();
    } catch (e) {
      toasts.error(`${slug}: network error -- ${e.message}`);
    }
  }

  function hasActiveDownload() {
    for (const [, v] of items) {
      if (v.state === 'downloading' || v.state === 'pending') return true;
    }
    return false;
  }

  function ensurePolling() {
    if (pollHandle) return;
    pollHandle = setInterval(async () => {
      await refresh();
      if (!hasActiveDownload()) {
        clearInterval(pollHandle);
        pollHandle = null;
      }
    }, 1500);
  }

  return {
    get items() { return items; },
    refresh,
    start,
    remove,
    ensurePolling,

    // Set<slug> of completed downloads -- driven by the items Map.
    get completed() {
      const out = new Set();
      for (const [slug, v] of items) {
        if (v.state === 'complete') out.add(slug);
      }
      return out;
    },
  };
})();
