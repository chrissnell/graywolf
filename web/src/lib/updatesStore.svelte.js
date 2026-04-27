// Reactive store for the GitHub update-check feature. Mirrors the IIFE
// + getter pattern of releaseNotesStore.svelte.js — state lives in
// $state runes, exposed via getters so components get fine-grained
// reactivity when they read updates.status etc.
//
// The server is the source of truth for status / enabled. Dismissal
// lives in localStorage (single key, stores the dismissed version
// string) because it's a per-browser display preference and not
// worth a server-side per-user column. The `dismissedVersion` $state
// mirror is what lets hasUnseenUpdate react to dismiss() instantly —
// localStorage itself isn't reactive.
//
// No pure-JS core / test split: this store is four thin fetches +
// a localStorage pass-through. Backend /pkg/updatescheck has the
// logic coverage.

import { toasts } from './stores.js';

const DISMISS_KEY = 'dismissed-update';

export const updates = (() => {
  let enabled = $state(true);        // from GET /api/updates/config
  let status = $state('pending');    // from GET /api/updates/status; default matches the backend's "no checker yet" fallback
  let current = $state('');
  let latest = $state('');
  let url = $state('');
  let checkedAt = $state('');
  let loading = $state(false);
  let error = $state(null);
  // Initialize from localStorage. Any exception on access (Safari
  // private mode, SSR-like environment) falls back to empty string.
  let dismissedVersion = $state((() => {
    try { return localStorage.getItem(DISMISS_KEY) ?? ''; }
    catch { return ''; }
  })());

  async function fetchJSON(path) {
    const res = await fetch(path, { credentials: 'same-origin' });
    if (res.status === 401) throw new Error('unauthorized');
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return res.json();
  }

  async function fetchStatus() {
    loading = true;
    error = null;
    try {
      const data = await fetchJSON('/api/updates/status');
      status = String(data?.status ?? 'pending');
      current = String(data?.current ?? '');
      latest = String(data?.latest ?? '');
      url = String(data?.url ?? '');
      checkedAt = String(data?.checked_at ?? '');
    } catch (e) {
      error = e?.message || String(e);
      // Don't clobber prior-good values on transient failure — mirror
      // the backend checker's retain-last-success policy.
    } finally {
      loading = false;
    }
  }

  async function fetchConfig() {
    try {
      const data = await fetchJSON('/api/updates/config');
      enabled = Boolean(data?.enabled);
    } catch (e) {
      error = e?.message || String(e);
    }
  }

  // Optimistic: flip local enabled, PUT, rollback on failure. Toast
  // on both success and failure — matches the voice used elsewhere
  // (brief, non-modal).
  async function setEnabled(next) {
    const prev = enabled;
    enabled = Boolean(next);
    try {
      const res = await fetch('/api/updates/config', {
        method: 'PUT',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled: Boolean(next) }),
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      enabled = Boolean(data?.enabled);
      toasts.success('Saved');
    } catch (e) {
      enabled = prev;
      error = e?.message || String(e);
      toasts.error("Couldn't save — try again.");
    }
  }

  // Writes the dismissed version to localStorage AND the reactive
  // mirror. hasUnseenUpdate flips to false, which causes the About
  // banner and the sidebar badge to un-render in the same tick.
  function dismiss() {
    if (!latest) return;
    try { localStorage.setItem(DISMISS_KEY, latest); } catch {}
    dismissedVersion = latest;
  }

  return {
    get enabled()           { return enabled; },
    get status()            { return status; },
    get current()           { return current; },
    get latest()            { return latest; },
    get url()               { return url; },
    get checkedAt()         { return checkedAt; },
    get loading()           { return loading; },
    get error()             { return error; },
    get dismissedVersion()  { return dismissedVersion; },
    // Single source of truth for "does anything about updates
    // deserve the operator's attention right now." Banner + sidebar
    // badge both read this.
    get hasUnseenUpdate() {
      return status === 'available' && latest !== '' && latest !== dismissedVersion;
    },
    fetchStatus,
    fetchConfig,
    setEnabled,
    dismiss,
  };
})();
