// Tests for the localStorage-backed ignoredInvites store.
//
// Pure-JS; no Svelte runtime. Runnable with either Node's built-in
// `node --test` or Vitest. The project has no test runner wired today,
// so these files exist for:
//   1. Documenting the store's contract.
//   2. Running cleanly once a test runner is added.
//
// To run with Vitest (after adding it):
//   npm run test
// To run with node --test (no deps needed, Node 20+):
//   node --test src/lib/stores/ignoredInvites.test.js
//
// The file uses `import.meta.vitest`-style describe/it if available, else
// falls back to node:test.

import { strict as assert } from 'node:assert';

// Tiny in-memory localStorage shim for tests. The store module reads
// from `localStorage` at import time, so we install the shim BEFORE
// importing.
function installLocalStorageShim() {
  const mem = new Map();
  globalThis.localStorage = {
    getItem: (k) => (mem.has(k) ? mem.get(k) : null),
    setItem: (k, v) => mem.set(k, String(v)),
    removeItem: (k) => mem.delete(k),
    clear: () => mem.clear(),
    get length() { return mem.size; },
    key: (i) => [...mem.keys()][i] ?? null,
    __mem: mem,
  };
  return mem;
}

function resetModule() {
  // Bust the module cache so a fresh import re-hydrates from localStorage.
  // Works with Node's dynamic import cache.
  const key = new URL('./ignoredInvites.js', import.meta.url).href;
  // Node doesn't expose a stable cache-eviction API for ESM, but re-
  // importing with a cache-busting query param achieves the same effect.
  return import(`./ignoredInvites.js?t=${Date.now()}-${Math.random()}`);
}

const hasNodeTest = typeof process !== 'undefined' && typeof process.versions?.node === 'string';

// Pick a describe/it compatible with vitest, jest, or node:test.
let describe, it, beforeEach;
try {
  const nodeTest = await import('node:test');
  describe = nodeTest.describe;
  it = nodeTest.it;
  beforeEach = nodeTest.beforeEach;
} catch {
  // Assume Vitest's globals if node:test isn't available.
  describe = globalThis.describe;
  it = globalThis.it;
  beforeEach = globalThis.beforeEach;
}

describe('ignoredInvites store', () => {
  beforeEach(() => {
    installLocalStorageShim();
  });

  it('starts empty when no localStorage entry exists', async () => {
    const mod = await resetModule();
    let current;
    const unsub = mod.ignoredInviteIds.subscribe((v) => { current = v; });
    unsub();
    assert.ok(current instanceof Set);
    assert.equal(current.size, 0);
  });

  it('hydrates from localStorage on import', async () => {
    const mem = installLocalStorageShim();
    mem.set('graywolf.ignoredInviteIds', JSON.stringify([7, 42, 99]));
    const mod = await resetModule();
    let current;
    const unsub = mod.ignoredInviteIds.subscribe((v) => { current = v; });
    unsub();
    assert.equal(current.size, 3);
    assert.ok(current.has(7));
    assert.ok(current.has(42));
    assert.ok(current.has(99));
  });

  it('ignoreInvite persists to localStorage', async () => {
    const mem = installLocalStorageShim();
    const mod = await resetModule();
    mod.ignoreInvite(123);
    const stored = JSON.parse(mem.get('graywolf.ignoredInviteIds') || '[]');
    assert.deepEqual(stored, [123]);
  });

  it('ignoreInvite is idempotent', async () => {
    const mod = await resetModule();
    mod.ignoreInvite(5);
    mod.ignoreInvite(5);
    mod.ignoreInvite(5);
    let current;
    const unsub = mod.ignoredInviteIds.subscribe((v) => { current = v; });
    unsub();
    assert.equal(current.size, 1);
  });

  it('unignoreInvite removes the id', async () => {
    const mod = await resetModule();
    mod.ignoreInvite(1);
    mod.ignoreInvite(2);
    mod.unignoreInvite(1);
    let current;
    const unsub = mod.ignoredInviteIds.subscribe((v) => { current = v; });
    unsub();
    assert.equal(current.size, 1);
    assert.ok(current.has(2));
    assert.ok(!current.has(1));
  });

  it('isIgnored peeks without subscription leaks', async () => {
    const mod = await resetModule();
    mod.ignoreInvite(50);
    assert.ok(mod.isIgnored(50));
    assert.ok(!mod.isIgnored(51));
  });

  it('rejects non-positive / non-numeric ids', async () => {
    const mod = await resetModule();
    mod.ignoreInvite(0);
    mod.ignoreInvite(-5);
    mod.ignoreInvite('abc');
    mod.ignoreInvite(null);
    let current;
    const unsub = mod.ignoredInviteIds.subscribe((v) => { current = v; });
    unsub();
    assert.equal(current.size, 0);
  });

  it('tolerates malformed localStorage payload', async () => {
    const mem = installLocalStorageShim();
    mem.set('graywolf.ignoredInviteIds', 'not-json-at-all');
    const mod = await resetModule();
    let current;
    const unsub = mod.ignoredInviteIds.subscribe((v) => { current = v; });
    unsub();
    assert.equal(current.size, 0);
  });

  it('markAutoNavDone / hasAutoNavFired track per-session state', async () => {
    const mod = await resetModule();
    assert.ok(!mod.hasAutoNavFired(77));
    mod.markAutoNavDone(77);
    assert.ok(mod.hasAutoNavFired(77));
    assert.ok(!mod.hasAutoNavFired(78));
    // autoNav tracker is module-scoped and does NOT persist to
    // localStorage — reimporting resets it.
    const fresh = await resetModule();
    assert.ok(!fresh.hasAutoNavFired(77));
  });
});
