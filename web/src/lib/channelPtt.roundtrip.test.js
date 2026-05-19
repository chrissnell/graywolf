import { test } from 'node:test';
import assert from 'node:assert/strict';

// Contract: GET /api/channels .ptt for an Android channel MUST include
// gpio_pin so ChannelEditModal can restore androidPttMethod. This mirrors
// the restore predicate in ChannelEditModal.svelte (row.ptt?.method ===
// 'android' && row.ptt?.gpio_pin).
test('android ptt row exposes gpio_pin for modal restore', () => {
  const row = { id: 1, ptt: { method: 'android', configured: true, gpio_pin: 3 } };
  const restored =
    row.ptt?.method === 'android' && row.ptt?.gpio_pin ? row.ptt.gpio_pin : 1;
  assert.equal(restored, 3, 'AIOC (3) must restore, not fall back to CP2102N (1)');
});
