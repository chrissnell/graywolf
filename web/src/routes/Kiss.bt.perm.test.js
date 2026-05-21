// Unit tests for the Bluetooth-permission-grant helper used by Kiss.svelte
// (Phase 6 of the Android Bluetooth KISS TNC plan).
//
// The component itself has no headless harness, so the helper is reproduced
// verbatim against a mocked JS bridge and verified end-to-end.

import { test, beforeEach, afterEach } from 'node:test';
import assert from 'node:assert/strict';

// Verbatim copy of requestBtPerm() from Kiss.svelte so we exercise the
// exact dispatcher-swap + try/catch behaviour without spinning up Svelte.
// If the production helper changes, this copy must too — the diff between
// the two is intentionally small and easy to spot in review.
function requestBtPerm({ isAndroid, loadBondedDevices }) {
  if (!isAndroid) return;
  if (!globalThis.GraywolfWebInterface?.requestBluetoothPermission) return;
  const callbackId = 'bt-' + Math.random().toString(36).slice(2);
  const prev = globalThis.__btResult;
  globalThis.__btResult = (id, granted) => {
    if (id !== callbackId) return;
    if (prev) globalThis.__btResult = prev;
    else delete globalThis.__btResult;
    if (granted) loadBondedDevices();
  };
  try {
    globalThis.GraywolfWebInterface.requestBluetoothPermission(callbackId);
  } catch (err) {
    if (prev) globalThis.__btResult = prev;
    else delete globalThis.__btResult;
  }
}

beforeEach(() => {
  delete globalThis.GraywolfWebInterface;
  delete globalThis.__btResult;
});

afterEach(() => {
  delete globalThis.GraywolfWebInterface;
  delete globalThis.__btResult;
});

test('no-ops on desktop (isAndroid=false)', () => {
  let loaded = 0;
  // Even with a bridge present, the isAndroid gate must short-circuit.
  globalThis.GraywolfWebInterface = {
    requestBluetoothPermission: () => assert.fail('should not be called'),
  };
  requestBtPerm({ isAndroid: false, loadBondedDevices: () => loaded++ });
  assert.equal(loaded, 0);
  assert.equal(globalThis.__btResult, undefined);
});

test('no-ops on Android when the JS bridge is missing the method', () => {
  let loaded = 0;
  globalThis.GraywolfWebInterface = {}; // no requestBluetoothPermission
  requestBtPerm({ isAndroid: true, loadBondedDevices: () => loaded++ });
  assert.equal(loaded, 0);
  assert.equal(globalThis.__btResult, undefined);
});

test('granted=true triggers loadBondedDevices and clears the dispatcher', () => {
  let loaded = 0;
  let capturedId = null;
  globalThis.GraywolfWebInterface = {
    requestBluetoothPermission: (id) => { capturedId = id; },
  };

  requestBtPerm({ isAndroid: true, loadBondedDevices: () => loaded++ });

  assert.ok(capturedId && capturedId.startsWith('bt-'));
  assert.equal(typeof globalThis.__btResult, 'function');

  // Simulate the Kotlin side firing window.__btResult(id, true).
  globalThis.__btResult(capturedId, true);

  assert.equal(loaded, 1);
  // Dispatcher is one-shot: should be torn down so a stray late
  // callback can't re-trigger the bonded-device fetch.
  assert.equal(globalThis.__btResult, undefined);
});

test('granted=false does not trigger loadBondedDevices', () => {
  let loaded = 0;
  let capturedId = null;
  globalThis.GraywolfWebInterface = {
    requestBluetoothPermission: (id) => { capturedId = id; },
  };

  requestBtPerm({ isAndroid: true, loadBondedDevices: () => loaded++ });
  globalThis.__btResult(capturedId, false);

  assert.equal(loaded, 0);
  assert.equal(globalThis.__btResult, undefined);
});

test('mismatched callbackId is ignored and dispatcher is preserved', () => {
  let loaded = 0;
  globalThis.GraywolfWebInterface = {
    requestBluetoothPermission: () => {},
  };

  requestBtPerm({ isAndroid: true, loadBondedDevices: () => loaded++ });
  assert.equal(typeof globalThis.__btResult, 'function');

  // A different in-flight id arrives — must be silently ignored.
  globalThis.__btResult('other-id', true);
  assert.equal(loaded, 0);
  // Dispatcher still in place for the real callback.
  assert.equal(typeof globalThis.__btResult, 'function');
});

test('thrown bridge error is swallowed and dispatcher is rolled back', () => {
  let loaded = 0;
  globalThis.GraywolfWebInterface = {
    requestBluetoothPermission: () => { throw new Error('boom'); },
  };

  requestBtPerm({ isAndroid: true, loadBondedDevices: () => loaded++ });
  // No throw escaping requestBtPerm, and the dispatcher is cleared
  // (no previous handler to restore) so we don't leave a dangling
  // closure on the global.
  assert.equal(loaded, 0);
  assert.equal(globalThis.__btResult, undefined);
});

test('previous __btResult dispatcher is restored after the callback fires', () => {
  let loaded = 0;
  let priorCalls = 0;
  const priorDispatcher = () => { priorCalls++; };
  globalThis.__btResult = priorDispatcher;

  let capturedId = null;
  globalThis.GraywolfWebInterface = {
    requestBluetoothPermission: (id) => { capturedId = id; },
  };

  requestBtPerm({ isAndroid: true, loadBondedDevices: () => loaded++ });
  globalThis.__btResult(capturedId, true);

  assert.equal(loaded, 1);
  // Restored, not deleted, because there was a prior handler.
  assert.equal(globalThis.__btResult, priorDispatcher);
});
