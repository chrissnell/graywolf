// web/src/lib/remote_actions/store.svelte.js
//
// Singleton store for outbound-Actions feature state. Mirrors
// lib/actions/store.svelte.js. Three slices:
//   - macrosByTarget: per-peer macro list, lazily loaded by openDrawer().
//   - creds:          credential list (one cache for the whole app).
//   - lastUsedCredByTarget: peer -> cred id used most recently for free-form
//                            fires; primes the picker default.
import { remoteCredsApi, remoteMacrosApi } from './api.js';

function describe(error, fallback) {
  if (!error) return fallback;
  if (typeof error === 'string') return error;
  return error.error ?? error.message ?? fallback;
}

class RemoteActionsStore {
  creds = $state([]);
  macrosByTarget = $state({}); // { 'KK7XYZ-9': [macro, ...] }
  lastUsedCredByTarget = $state({}); // { 'KK7XYZ-9': 4 }
  loading = $state(false);
  error = $state(null);

  async loadCreds() {
    const { data, error } = await remoteCredsApi.list();
    if (error) {
      this.error = describe(error, 'Failed to load credentials');
      return;
    }
    this.creds = data ?? [];
    this.error = null;
  }

  async loadMacros(target) {
    const { data, error } = await remoteMacrosApi.list(target);
    if (error) {
      this.error = describe(error, 'Failed to load macros');
      return;
    }
    this.macrosByTarget = { ...this.macrosByTarget, [target]: data ?? [] };
    this.error = null;
  }

  async refreshTarget(target) {
    this.loading = true;
    try {
      await Promise.all([this.loadCreds(), this.loadMacros(target)]);
    } finally {
      this.loading = false;
    }
  }

  rememberCredForTarget(target, credId) {
    if (!target || !credId) return;
    this.lastUsedCredByTarget = {
      ...this.lastUsedCredByTarget,
      [target]: credId,
    };
  }

  // Resolve the picker default for a target. Order:
  //   1. Most recently used credential against this peer.
  //   2. The credential whose Name is alphabetically first.
  //   3. null (manual OTP mode).
  defaultCredFor(target) {
    const remembered = this.lastUsedCredByTarget[target];
    if (remembered && this.creds.find((c) => c.id === remembered)) return remembered;
    if (this.creds.length === 0) return null;
    const sorted = [...this.creds].sort((a, b) =>
      (a.name ?? '').localeCompare(b.name ?? ''),
    );
    return sorted[0]?.id ?? null;
  }
}

export const remoteActionsStore = new RemoteActionsStore();
