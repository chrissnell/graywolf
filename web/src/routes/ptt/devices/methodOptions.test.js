import { test } from 'node:test';
import assert from 'node:assert/strict';

test('DESKTOP_METHODS includes all six PTT methods in display order', async () => {
  const { DESKTOP_METHODS } = await import('./methodOptions.desktop.js');
  assert.deepEqual(
    DESKTOP_METHODS.map(m => m.wire.method),
    ['none', 'serial_rts', 'serial_dtr', 'gpio', 'cm108', 'rigctld'],
  );
});

test('DESKTOP_METHODS entries carry label + wire', async () => {
  const { DESKTOP_METHODS } = await import('./methodOptions.desktop.js');
  for (const m of DESKTOP_METHODS) {
    assert.ok(m.label, `${m.wire.method} missing label`);
    assert.ok(m.wire, `${m.wire.method} missing wire`);
  }
});
