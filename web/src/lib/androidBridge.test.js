import { test, beforeEach, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import { getBearerToken, _resetForTests } from './androidBridge.js';

// node:test doesn't ship a window globals shim. Tests run on
// node so we provide a globalThis-keyed bridge instead -- the
// implementation reads via globalThis.GraywolfWebInterface so this
// works without a DOM. Production runs under WebView where window
// === globalThis.
beforeEach(() => {
  _resetForTests();
  delete globalThis.GraywolfWebInterface;
});
afterEach(() => {
  _resetForTests();
  delete globalThis.GraywolfWebInterface;
});

test('returns null when bridge absent (desktop)', () => {
  assert.equal(getBearerToken(), null);
});

test('returns the token when bridge present', () => {
  globalThis.GraywolfWebInterface = { getBearerToken: () => 'tok-xyz' };
  assert.equal(getBearerToken(), 'tok-xyz');
});

test('caches the token across calls', () => {
  let calls = 0;
  globalThis.GraywolfWebInterface = {
    getBearerToken: () => { calls += 1; return 'tok-cached'; },
  };
  assert.equal(getBearerToken(), 'tok-cached');
  assert.equal(getBearerToken(), 'tok-cached');
  assert.equal(calls, 1, 'JNI bridge must be called only once');
});

test('returns null if bridge throws', () => {
  globalThis.GraywolfWebInterface = {
    getBearerToken: () => { throw new Error('JNI dead'); },
  };
  assert.equal(getBearerToken(), null);
});

test('returns null when bridge returns empty string', () => {
  globalThis.GraywolfWebInterface = { getBearerToken: () => '' };
  assert.equal(getBearerToken(), null);
});
