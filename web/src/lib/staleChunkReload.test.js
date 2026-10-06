import { test } from 'node:test';
import assert from 'node:assert/strict';
import { isStaleChunkError, shouldAutoReload } from './staleChunkReload.js';

test('isStaleChunkError matches Chrome/Firefox/Safari dynamic-import phrasing', () => {
  assert.equal(isStaleChunkError(new TypeError('Failed to fetch dynamically imported module: http://x/y.js')), true);
  assert.equal(isStaleChunkError(new TypeError('error loading dynamically imported module: http://x/y.js')), true);
  assert.equal(isStaleChunkError(new Error('Importing a module script failed')), true);
});

test('isStaleChunkError ignores unrelated errors, including generic fetch failures', () => {
  assert.equal(isStaleChunkError(new TypeError('Failed to fetch')), false);
  assert.equal(isStaleChunkError(new Error('HTTP 401')), false);
  assert.equal(isStaleChunkError(null), false);
  assert.equal(isStaleChunkError(undefined), false);
});

function fakeStorage(initial = {}) {
  const data = { ...initial };
  return {
    getItem: (k) => (k in data ? data[k] : null),
    setItem: (k, v) => { data[k] = v; },
  };
}

test('shouldAutoReload allows the first reload and then guards a burst', () => {
  const storage = fakeStorage();
  assert.equal(shouldAutoReload(1000, storage), true);
  assert.equal(shouldAutoReload(1500, storage), false, 'within the guard window');
  assert.equal(shouldAutoReload(11001, storage), true, 'guard window elapsed');
});
