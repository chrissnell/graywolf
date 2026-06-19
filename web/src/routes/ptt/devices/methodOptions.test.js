import { test } from 'node:test';
import assert from 'node:assert/strict';

test('DESKTOP_METHODS includes all seven PTT methods in display order', async () => {
  const { DESKTOP_METHODS } = await import('./methodOptions.desktop.js');
  assert.deepEqual(
    DESKTOP_METHODS.map(m => m.wire.method),
    ['none', 'vox', 'serial_rts', 'serial_dtr', 'gpio', 'cm108', 'rigctld'],
  );
});

test('DESKTOP_METHODS entries carry label + wire', async () => {
  const { DESKTOP_METHODS } = await import('./methodOptions.desktop.js');
  for (const m of DESKTOP_METHODS) {
    assert.ok(m.label, `${m.wire.method} missing label`);
    assert.ok(m.wire, `${m.wire.method} missing wire`);
  }
});

test('ANDROID_METHODS includes off + 3 USB methods + VOX in display order', async () => {
  const { ANDROID_METHODS } = await import('./methodOptions.android.js');
  assert.deepEqual(
    ANDROID_METHODS.map(m => ({ method: m.wire.method, ptt_method: m.wire.ptt_method ?? null })),
    [
      { method: 'none',    ptt_method: null },
      { method: 'android', ptt_method: 1 },   // CP2102N RTS
      { method: 'android', ptt_method: 3 },   // AIOC CDC-ACM DTR
      { method: 'android', ptt_method: 2 },   // CM108 HID
      { method: 'android', ptt_method: 4 },   // VOX
    ],
  );
});

test('ANDROID_METHODS USB entries carry deviceClass; VOX and Off do not', async () => {
  const { ANDROID_METHODS } = await import('./methodOptions.android.js');
  const byKey = (m) => `${m.wire.method}#${m.wire.ptt_method ?? ''}`;
  const map = new Map(ANDROID_METHODS.map(m => [byKey(m), m.deviceClass ?? null]));
  assert.equal(map.get('android#1'), 'cp2102n');
  assert.equal(map.get('android#3'), 'cdc-acm');
  assert.equal(map.get('android#2'), 'hid-cm108');
  assert.equal(map.get('android#4'), null);
  assert.equal(map.get('none#'), null);
});
