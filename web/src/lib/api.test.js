import { test, beforeEach, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import { get } from 'svelte/store';
import { _resetForTests as resetBridge } from './androidBridge.js';
import { api } from './api.js';
import { online, markConnected } from './stores/connection.js';

// api.js reads globalThis.fetch and window.location.hash. Provide a
// minimal window+location shim before importing it; tests reset the
// bridge cache between cases so the bridge presence is re-evaluated.

let originalFetch;
let originalWindow;

beforeEach(() => {
  originalFetch = globalThis.fetch;
  originalWindow = globalThis.window;
  globalThis.window = { location: { hash: '' } };
  delete globalThis.GraywolfWebInterface;
  resetBridge();
});
afterEach(() => {
  globalThis.fetch = originalFetch;
  globalThis.window = originalWindow;
  delete globalThis.GraywolfWebInterface;
  resetBridge();
});

test('redirects to #/login on 401 when bridge is absent (desktop)', async () => {
  globalThis.fetch = () => Promise.resolve(new Response('{}', { status: 401 }));
  await assert.rejects(() => api.get('/version'));
  assert.equal(globalThis.window.location.hash, '#/login');
});

test('does NOT redirect on 401 when Android bridge is present', async () => {
  globalThis.GraywolfWebInterface = { getBearerToken: () => 'tok-abc' };
  globalThis.fetch = () => Promise.resolve(new Response('{}', { status: 401 }));
  await assert.rejects(() => api.get('/version'));
  assert.notEqual(globalThis.window.location.hash, '#/login');
});

test('network failure marks disconnected and throws ApiError (no fabricated data)', async () => {
  // A thrown fetch is a genuine lost connection. import.meta.env is undefined
  // under node --test, so import.meta.env?.DEV is falsy here — exactly the
  // production path: no mock fallback, surface the error, flip the store.
  markConnected();
  assert.equal(get(online), true);
  globalThis.fetch = () => Promise.reject(new TypeError('Failed to fetch'));
  await assert.rejects(() => api.get('/status'), (err) => {
    assert.equal(err.name, 'ApiError');
    assert.equal(err.status, 0);
    return true;
  });
  assert.equal(get(online), false);
  markConnected();
});
