// web/src/routes/ptt/devicePayload.test.js
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { resolveDevice } from './devicePayload.js';

const enumerated = [
  { path: '/dev/ttyUSB0', type: 'serial', name: 'ttyUSB0', recommended: true },
  { path: '/dev/hidraw0', type: 'cm108', name: 'hidraw0' },
];

test('returns the enumerated row verbatim when the path matches', () => {
  const d = resolveDevice(enumerated, '/dev/ttyUSB0', 'serial');
  assert.equal(d, enumerated[0]);
  assert.equal(d.recommended, true);
});

test('synthesises a manual device for a path not in the list', () => {
  const d = resolveDevice(enumerated, '/dev/aioc-aprs-ptt', 'serial');
  assert.deepEqual(d, { path: '/dev/aioc-aprs-ptt', manual: true, type: 'serial' });
});

test('trims surrounding whitespace before matching and emitting', () => {
  assert.equal(resolveDevice(enumerated, '  /dev/ttyUSB0  ', 'serial'), enumerated[0]);
  assert.deepEqual(resolveDevice(enumerated, '  /dev/custom  ', 'cm108'), {
    path: '/dev/custom',
    manual: true,
    type: 'cm108',
  });
});

test('returns null for an empty or whitespace-only path', () => {
  assert.equal(resolveDevice(enumerated, '', 'serial'), null);
  assert.equal(resolveDevice(enumerated, '   ', 'serial'), null);
  assert.equal(resolveDevice(enumerated, null, 'serial'), null);
});

test('omits type when no fallback type is supplied', () => {
  assert.deepEqual(resolveDevice([], '/dev/x'), { path: '/dev/x', manual: true });
});

test('tolerates a non-array device list', () => {
  assert.deepEqual(resolveDevice(undefined, '/dev/x', 'gpio'), {
    path: '/dev/x',
    manual: true,
    type: 'gpio',
  });
});

test('selecting a persistent alias saves that alias rather than its kernel target', () => {
  const path = '/dev/serial/by-id/usb-Adapter_UNIQUE-if00-port0';
  const d = { path, type: 'serial', description: 'Adapter — persistent ID (currently /dev/ttyUSB0)' };
  assert.equal(resolveDevice([d], path, 'serial'), d);
  assert.equal(resolveDevice([d], path, 'serial').path, path);
});
